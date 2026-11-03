package s3store

import (
	"context"
	"net/url"
	"os"
	"testing"
)

// TestParseEndpoint pins endpoint parsing: an empty endpoint must yield the
// sentinel (meaning "SDK default"), and a malformed one must be reported
// with the original input rather than silently defaulting.
func TestParseEndpoint(t *testing.T) {
	got, err := parseEndpoint("")
	if err != nil {
		t.Fatalf("parseEndpoint(\"\"): %v", err)
	}
	if got != sentinelURL {
		t.Errorf("empty endpoint = %v, want sentinel", got)
	}

	got, err = parseEndpoint("https://oss-cn-hangzhou.aliyuncs.com")
	if err != nil {
		t.Fatalf("parseEndpoint: %v", err)
	}
	if got.Hostname() != "oss-cn-hangzhou.aliyuncs.com" || got.Scheme != "https" {
		t.Errorf("parsed = %v, want the aliases host over https", got)
	}

	if _, err := parseEndpoint("http://exa mple.com/%zz"); err == nil {
		t.Error("a malformed endpoint should error")
	}
}

// TestSupportsTransferAcceleration pins the accelerate detection that
// decides whether the SDK owns the endpoint.
func TestSupportsTransferAcceleration(t *testing.T) {
	cases := []struct {
		endpoint string
		want     bool
	}{
		{"https://s3-accelerate.amazonaws.com", true},
		{"https://s3.amazonaws.com", false},
		{"https://oss-cn-hangzhou.aliyuncs.com", false},
		{"", false},
	}
	for _, tc := range cases {
		u, err := url.Parse(tc.endpoint)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", tc.endpoint, err)
		}
		if got := supportsTransferAcceleration(*u); got != tc.want {
			t.Errorf("supportsTransferAcceleration(%q) = %v, want %v", tc.endpoint, got, tc.want)
		}
	}
}

// TestNewS3ClientCarriesClientOptions pins that the compatibility knobs
// actually land on the built store. NewS3Client resolves credentials through
// the shared config, so a demo profile is supplied explicitly; no network
// call happens at construction.
func TestNewS3ClientCarriesClientOptions(t *testing.T) {
	dir := t.TempDir()
	cfg := writeSharedConfig(t, dir)

	store, err := NewS3Client(context.Background(), S3Option{
		Profile:    "demo",
		Region:     "cn-hangzhou",
		Endpoint:   "https://s3.example.com",
		ConfigFile: cfg,
		Client: ClientOptions{
			MaxRetries:       5,
			UseListObjectsV1: true,
			NoVerifySSL:      true,
			NoSignRequest:    false,
		},
	})
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}
	if !store.useListObjectsV1 {
		t.Error("useListObjectsV1 not carried into the store")
	}
	if store.client == nil || store.tm == nil || store.presigner == nil {
		t.Fatal("client/transfermanager/presigner must all be initialised")
	}
	if got := store.Client().Options().Region; got != "cn-hangzhou" {
		t.Errorf("client region = %q, want cn-hangzhou", got)
	}
	if store.DryRun() {
		t.Error("DryRun should default to false")
	}
}

// TestNewS3ClientRegionResolution pins region precedence: the profile's
// shared-config region wins when no explicit region is passed, and an
// explicit option overrides it. Falling back to us-east-1 for AWS is what
// makes an unset region work at all.
func TestNewS3ClientRegionResolution(t *testing.T) {
	dir := t.TempDir()
	cfg := writeSharedConfig(t, dir)

	store, err := NewS3Client(context.Background(), S3Option{Profile: "demo", ConfigFile: cfg})
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}
	if got := store.Client().Options().Region; got != "cn-hangzhou" {
		t.Errorf("region from shared config = %q, want cn-hangzhou", got)
	}

	store, err = NewS3Client(context.Background(), S3Option{
		Profile: "demo", ConfigFile: cfg, Region: "eu-west-1",
	})
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}
	if got := store.Client().Options().Region; got != "eu-west-1" {
		t.Errorf("explicit region = %q, want eu-west-1", got)
	}
}

// TestNewS3ClientDryRun pins that DryRun is carried through.
func TestNewS3ClientDryRun(t *testing.T) {
	dir := t.TempDir()
	cfg := writeSharedConfig(t, dir)
	store, err := NewS3Client(context.Background(), S3Option{
		Profile: "demo", ConfigFile: cfg, DryRun: true,
	})
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}
	if !store.DryRun() {
		t.Error("DryRun not carried into the store")
	}
}

// writeSharedConfig writes a minimal shared config + credentials pair for a
// "demo" profile and returns the config path (the SDK also resolves the
// credentials file next to it via the profile's source).
func writeSharedConfig(t *testing.T, dir string) string {
	t.Helper()
	cfg := dir + "/config"
	cred := dir + "/credentials"
	if err := writeFile(cfg, "[profile demo]\nregion = cn-hangzhou\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(cred, "[demo]\naws_access_key_id = demo\naws_secret_access_key = demo\n"); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}
