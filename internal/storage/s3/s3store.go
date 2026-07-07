// Package s3store implements the S3 Storage backend and client factory.
package s3store

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/logging"
)

// defaultRegion is used when no region can be inferred from the user, the
// environment or the bucket itself. AWS treats us-east-1 as the canonical
// default region.
const defaultRegion = "us-east-1"

// transferAccelEndpoint is the Amazon S3 Transfer Acceleration endpoint.
// When the user supplies it we let the SDK own the endpoint (set the parsed
// URL back to the sentinel) and enable UseAccelerate.
const transferAccelEndpoint = "s3-accelerate.amazonaws.com"

// sentinelURL is the zero value of url.URL. parseEndpoint returns it for an
// empty input so downstream code can distinguish "no endpoint supplied"
// (AWS default) from a custom endpoint.
var sentinelURL = url.URL{}

// parseEndpoint parses the given endpoint URL. An empty endpoint yields the
// sentinel, signalling "use the AWS SDK default endpoint". A parse error is
// reported with the original input so the caller can surface a useful
// message.
func parseEndpoint(endpoint string) (url.URL, error) {
	if endpoint == "" {
		return sentinelURL, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return sentinelURL, fmt.Errorf("parse endpoint %q: %v", endpoint, err)
	}
	return *u, nil
}

// supportsTransferAcceleration reports whether endpoint points at the S3
// Transfer Acceleration hostname. When true the caller should set
// s3.Options.UseAccelerate = true and stop overriding the endpoint (the SDK
// derives the correct accelerate URL from the bucket name).
func supportsTransferAcceleration(endpoint url.URL) bool {
	return endpoint.Hostname() == transferAccelEndpoint
}

// S3Store is the storage.Storage implementation for S3.
type S3Store struct {
	client     *s3.Client
	uploader   *manager.Uploader
	downloader *manager.Downloader
	presigner  *s3.PresignClient

	// dryRun, when true, makes mutating operations no-ops.
	dryRun bool
	// useListObjectsV1 selects the legacy ListObjects API instead of
	// ListObjectsV2 (useful for services that do not implement V2).
	useListObjectsV1 bool
	// requestPayerFlag, when non-empty, is sent as RequestPayer on every
	// request that supports it.
	requestPayerFlag string
	// noSuchUploadRetryCount caps the number of times Put retries an upload
	// that failed with NoSuchUpload. Currently surfaced for Track B/C.
	noSuchUploadRetryCount int
}

// newRetryer builds the SDK v2 retryer. It starts from retry.NewStandard
// (which already covers throttling, 5xx, RequestTimeout, connection errors,
// etc.) and layers on extra retryable error codes
// (InternalError, RequestTimeTooSkewed, SlowDown) plus a deny-list for the
// token errors (ExpiredToken, ExpiredTokenException, InvalidToken) which
// must NOT be retried even if the standard rules would allow it.
//
// MaxAttempts is set via retry.AddWithMaxAttempts. A non-positive max
// leaves the SDK default in place.
func newRetryer(max int) aws.Retryer {
	std := retry.NewStandard(func(o *retry.StandardOptions) {
		if max > 0 {
			o.MaxAttempts = max
		}
		// Append the extra retryable codes. StandardOptions already
		// seeds Retryables with DefaultRetryables, so we append rather
		// than replace.
		o.Retryables = append(o.Retryables, retry.IsErrorRetryableFunc(func(err error) aws.Ternary {
			if err == nil {
				return aws.UnknownTernary
			}
			var apiErr smithy.APIError
			if !errors.As(err, &apiErr) {
				return aws.UnknownTernary
			}
			switch apiErr.ErrorCode() {
			case "InternalError", "RequestTimeTooSkewed", "SlowDown":
				return aws.TrueTernary
			case "ExpiredToken", "ExpiredTokenException", "InvalidToken":
				return aws.FalseTernary
			}
			// "connection reset"/"connection timed out" are not separate
			// error codes; they appear inside the error message. The v2
			// standard retryer already handles "connection reset" via
			// RetryableConnectionError, so we only add the timed-out
			// variant here.
			if strings.Contains(apiErr.ErrorMessage(), "connection timed out") {
				return aws.TrueTernary
			}
			return aws.UnknownTernary
		}))
	})
	return std
}

// NewS3Client builds an S3Store from the given options.
//
// Addressing: option.UsePathStyle is forwarded to the SDK verbatim.
//   - true  → path-style (https://endpoint/bucket/key)
//   - false → virtual-host (https://bucket.endpoint/key), the AWS default
//
// A transfer-acceleration endpoint enables s3.Options.UseAccelerate and the
// SDK owns the endpoint (the parsed URL is reset to the sentinel). A custom
// endpoint (non-accelerate) is wired through s3.Options.BaseEndpoint, which
// the SDK honours verbatim for both virtual-host and path-style addressing —
// most S3-compatible services (OSS/OBS/COS/MinIO) work with path-style +
// BaseEndpoint.
func NewS3Client(ctx context.Context, option S3Option) (*S3Store, error) {
	endpointURL, err := parseEndpoint(option.Endpoint)
	if err != nil {
		return nil, err
	}

	// Transfer acceleration: let the SDK own the endpoint. Keeping the
	// parsed accelerate URL would cause bucket operations to fail because
	// the SDK derives the accelerate hostname itself from the bucket name.
	useAccelerate := supportsTransferAcceleration(endpointURL)
	if useAccelerate {
		endpointURL = sentinelURL
	}

	var optFns []func(*config.LoadOptions) error
	// Route SDK warnings (e.g. "Response has no supported checksum") through
	// the standard logger instead of stderr: stderr writes corrupt the TUI's
	// rendering, while the standard logger already goes to debug.log or
	// /dev/null depending on --debug.
	optFns = append(optFns, config.WithLogger(logging.LoggerFunc(
		func(classification logging.Classification, format string, v ...any) {
			log.Printf("SDK %s "+format, append([]any{string(classification)}, v...)...)
		})))
	if option.Region != "" {
		optFns = append(optFns, config.WithRegion(option.Region))
	}
	if option.Profile != "" {
		optFns = append(optFns, config.WithSharedConfigProfile(option.Profile))
	}
	if option.CredentialFile != "" {
		optFns = append(optFns, config.WithSharedCredentialsFiles([]string{option.CredentialFile}))
	}
	if option.ConfigFile != "" {
		optFns = append(optFns, config.WithSharedConfigFiles([]string{option.ConfigFile}))
	}
	if option.MaxRetries > 0 {
		retryer := newRetryer(option.MaxRetries)
		optFns = append(optFns, config.WithRetryer(func() aws.Retryer { return retryer }))
	}
	if option.NoVerifySSL {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		} else {
			transport.TLSClientConfig.InsecureSkipVerify = true
		}
		httpClient := &http.Client{Transport: transport}
		optFns = append(optFns, config.WithHTTPClient(httpClient))
	}

	conf, err := config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, err
	}
	if conf.Region == "" {
		conf.Region = defaultRegion
	}

	client := s3.NewFromConfig(conf, func(o *s3.Options) {
		o.UsePathStyle = option.UsePathStyle
		o.UseAccelerate = useAccelerate
		// Custom endpoint (non-accelerate). BaseEndpoint is the modern v2
		// replacement for the deprecated EndpointResolverWithOptionsFunc
		// path. The SDK honours the URL verbatim for both virtual-host and
		// path-style addressing.
		if endpointURL != sentinelURL {
			o.BaseEndpoint = aws.String(endpointURL.String())
		}
		if option.NoSignRequest {
			o.Credentials = nil // anonymous credentials
		}
	})

	uploader := manager.NewUploader(client)
	downloader := manager.NewDownloader(client)
	presigner := s3.NewPresignClient(client)

	return &S3Store{
		client:                 client,
		uploader:               uploader,
		downloader:             downloader,
		presigner:              presigner,
		dryRun:                 option.DryRun,
		useListObjectsV1:       option.UseListObjectsV1,
		requestPayerFlag:       option.RequestPayer,
		noSuchUploadRetryCount: option.NoSuchUploadRetryCount,
	}, nil
}

// requestPayer returns the RequestPayer value to send on supporting
// requests, or the zero value (omitted by the SDK) when unset.
func (s3store *S3Store) requestPayer() types.RequestPayer {
	if s3store.requestPayerFlag == "" {
		return ""
	}
	return types.RequestPayer(s3store.requestPayerFlag)
}

// DryRun reports whether the store is in dry-run mode. Mutating operations
// (Track B/C) consult this before issuing writes.
func (s3store *S3Store) DryRun() bool { return s3store.dryRun }

// Client returns the underlying S3 client. It is exposed so that components
// needing direct access to operations not wrapped on S3Store (e.g. preview
// fetching via HeadObject/GetObject range) can do so without re-implementing
// the client configuration. Callers must not mutate the client.
func (s3store *S3Store) Client() *s3.Client {
	return s3store.client
}

// Credentials is a small static-credentials helper used by callers that
// build their own config (kept for backwards compatibility).
type Credentials struct {
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
}

func (c *Credentials) GetAccessKeyID() string     { return c.AccessKeyID }
func (c *Credentials) GetAccessKeySecret() string { return c.AccessKeySecret }
func (c *Credentials) GetSecurityToken() string   { return c.SecurityToken }

// NewAwsS3Provider wraps the given credentials as a static credentials
// provider.
func NewAwsS3Provider(credential *Credentials) credentials.StaticCredentialsProvider {
	return credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     credential.AccessKeyID,
			SecretAccessKey: credential.AccessKeySecret,
			SessionToken:    credential.SecurityToken,
		},
	}
}

// NewEnvironmentVariableCredentials reads OSS_* env vars and returns a
// Credentials value. It is kept for legacy callers.
func NewEnvironmentVariableCredentials() (*Credentials, error) {
	accessID := os.Getenv("OSS_ACCESS_KEY_ID")
	if accessID == "" {
		return nil, errors.New("access key id is empty")
	}
	accessKey := os.Getenv("OSS_ACCESS_KEY_SECRET")
	if accessKey == "" {
		return nil, errors.New("access key secret is empty")
	}
	token := os.Getenv("OSS_SESSION_TOKEN")
	return &Credentials{
		AccessKeyID:     accessID,
		AccessKeySecret: accessKey,
		SecurityToken:   token,
	}, nil
}
