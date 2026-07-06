//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LinPr/lazys3/internal/storage"
	fsstore "github.com/LinPr/lazys3/internal/storage/fs"
	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/aws/aws-sdk-go-v2/config"
)

// realProfile holds the value of LAZYS3_E2E_REAL. When non-empty, the
// e2e suite runs against the named shared-config profile (typically
// "oss") instead of the in-process gofakes3 server.
var realProfile string

// useReal reports whether the suite is running against a real
// S3-compatible service.
var useReal bool

// TestMain parses the test flags and reads LAZYS3_E2E_REAL. If it is set
// to a non-empty value (e.g. "oss"), the suite switches to real-OSS mode
// and reads the profile named by that value from ~/.aws/config.
func TestMain(m *testing.M) {
	realProfile = os.Getenv("LAZYS3_E2E_REAL")
	useReal = realProfile != ""

	code := m.Run()
	os.Exit(code)
}

// endpointFor returns the S3 endpoint URL the test should target.
//
// In gofakes3 mode this starts an in-process server and returns its URL.
// In real-OSS mode this returns "" so the AWS SDK resolves the endpoint
// from the shared config profile.
func endpointFor(t *testing.T) string {
	t.Helper()
	if useReal {
		return ""
	}
	return s3ServerEndpoint(t)
}

// clientFor builds a storage.Storage against the appropriate backend.
//
// In gofakes3 mode it points S3Option.Endpoint at the in-process server
// the test already created (passed in as `endpoint`), forces path-style,
// and sets AWS_* env vars so the SDK's default config loader picks up
// static test credentials. The endpoint must be the same one the test's
// raw s3.Client talks to, otherwise bucket/object setup will not be
// visible to the storage client.
//
// In real-OSS mode it sets S3Option.Profile to realProfile and lets the
// SDK load ~/.aws/config (region, endpoint, credentials) from there.
func clientFor(t *testing.T, endpoint string) *storage.Storage {
	t.Helper()
	ctx := context.Background()

	s3opt := s3store.S3Option{}
	localopt := fsstore.LocalOption{}

	if useReal {
		s3opt.Profile = realProfile
		// Region/endpoint come from the shared config profile. OSS requires
		// virtual-host-style addressing, so leave UsePathStyle false.
	} else {
		s3opt.Endpoint = endpoint
		s3opt.UsePathStyle = true
		s3opt.Region = defaultRegion
		// gofakes3 does not validate credentials, but the SDK requires
		// them to be non-empty. Set them via env so storage.NewStorage
		// (which calls config.LoadDefaultConfig) picks them up.
		t.Setenv("AWS_ACCESS_KEY_ID", defaultAccessKeyID)
		t.Setenv("AWS_SECRET_ACCESS_KEY", defaultSecretAccessKey)
		t.Setenv("AWS_REGION", defaultRegion)
	}

	opt := storage.NewStorageOption(s3opt, localopt)
	st, err := storage.NewStorage(ctx, *opt)
	if err != nil {
		t.Fatalf("storage.NewStorage: %v", err)
	}
	return st
}

// s3StoreOptionFor builds an S3Option suitable for s3store.NewS3Client in
// either mode. It mirrors clientFor's S3Option construction so tests that
// build an S3Store directly (e.g. to call ListBuckets / ListObjects /
// PutObject / DeleteObjects) configure the client the same way as the
// storage.Storage wrapper under test.
func s3StoreOptionFor(t *testing.T, endpoint string) s3store.S3Option {
	t.Helper()
	if useReal {
		return s3store.S3Option{Profile: realProfile}
	}
	t.Setenv("AWS_ACCESS_KEY_ID", defaultAccessKeyID)
	t.Setenv("AWS_SECRET_ACCESS_KEY", defaultSecretAccessKey)
	t.Setenv("AWS_REGION", defaultRegion)
	return s3store.S3Option{
		Region:       defaultRegion,
		Endpoint:     endpoint,
		UsePathStyle: true,
	}
}

// createBucketRegion returns the region to pass to S3Store.CreateBucket.
// In gofakes3 mode this is defaultRegion (the fake ignores it). In real
// mode we pass "" so S3Store.CreateBucket does NOT set
// CreateBucketConfiguration.LocationConstraint — Aliyun OSS rejects a
// LocationConstraint that doesn't match the endpoint's region.
func createBucketRegion(t *testing.T) string {
	t.Helper()
	if useReal {
		return ""
	}
	return defaultRegion
}

// sharedConfigPath returns the path to ~/.aws/config. It is used when
// real-OSS mode needs to inspect the profile directly.
func sharedConfigPath(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	return filepath.Join(home, ".aws", "config")
}

// loadSharedConfigProfile is a thin wrapper around
// config.LoadSharedConfigProfile for tests that need to inspect profile
// fields (e.g. endpoint_url) directly.
func loadSharedConfigProfile(ctx context.Context, name string) (config.SharedConfig, error) {
	return config.LoadSharedConfigProfile(ctx, name)
}
