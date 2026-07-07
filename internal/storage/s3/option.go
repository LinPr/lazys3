package s3store

// S3Option stores configuration for the S3 storage backend. Field status is
// annotated so callers know what is wired up vs. what is still a TODO.
type S3Option struct {
	// Region is the AWS region to use. When empty, NewS3Client defaults to
	// us-east-1.
	//
	// Status: implemented.
	Region string

	// UsePathStyle selects the S3 addressing style.
	//   - true:  force path-style addressing (https://endpoint/bucket/key).
	//           Required for MinIO, Alibaba OSS, Tencent COS, GCS and most
	//           S3-compatible services.
	//   - false (default): virtual-host addressing
	//           (https://bucket.endpoint/key), which is the AWS S3 default.
	//
	// Status: implemented.
	UsePathStyle bool

	// Profile selects a named profile from the shared credentials file.
	//
	// Status: implemented.
	Profile string

	// Endpoint overrides the default service endpoint URL. Used for
	// MinIO/OSS/COS/GCS and other S3-compatible services.
	//
	// Status: implemented.
	Endpoint string

	// NoVerifySSL disables TLS certificate verification.
	//
	// Status: implemented.
	NoVerifySSL bool

	// DryRun makes mutating operations (Put/Copy/Delete/MakeBucket/...)
	// no-ops on the client side.
	//
	// Status: implemented.
	DryRun bool

	// NoSignRequest sends anonymous (unsigned) requests. Used for public
	// buckets.
	//
	// Status: implemented.
	NoSignRequest bool

	// UseListObjectsV1 selects the legacy ListObjects API instead of
	// ListObjectsV2. Some S3-compatible services do not implement V2.
	//
	// Status: implemented.
	UseListObjectsV1 bool

	// RequestPayer, when set, is sent as RequestPayer on every supporting
	// request to acknowledge requester-pays buckets.
	//
	// Status: implemented.
	RequestPayer string

	// MaxRetries is the maximum number of attempts the SDK retryer will
	// make for a retriable request. A non-positive value leaves the SDK
	// default (3) in place. The retryer is the v2 standard retryer
	// (retry.NewStandard) extended with extra retryable error codes
	// (InternalError, RequestTimeTooSkewed, SlowDown, plus
	// connection-timed-out string matches) and with the token errors
	// (ExpiredToken/ExpiredTokenException/InvalidToken) explicitly
	// excluded.
	//
	// Status: implemented.
	MaxRetries int

	// NoSuchUploadRetryCount caps the number of times Put retries an upload
	// that failed with NoSuchUpload. A non-positive value disables the
	// retry path. Currently stored on the struct for downstream use by
	// Track B/C; not yet wired into Put.
	//
	// Status: implemented.
	NoSuchUploadRetryCount int

	// CredentialFile overrides the shared credentials file path the SDK
	// loads (config.WithSharedCredentialsFiles), replacing the default
	// ~/.aws/credentials source. Profile (when set) is still honoured via
	// config.WithSharedConfigProfile.
	//
	// Status: implemented.
	CredentialFile string

	// ConfigFile overrides the shared config file path the SDK loads
	// (config.WithSharedConfigFiles), replacing the default ~/.aws/config
	// source. Resolved at startup with the precedence
	// --aws-config > AWS_CONFIG_FILE > ~/.aws/config.
	//
	// Status: implemented.
	ConfigFile string
}
