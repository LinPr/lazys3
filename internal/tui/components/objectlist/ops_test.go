package objectlist

import "testing"

// TestS3OptionFromListOption pins the Option → s3store.S3Option mapping:
// the resolved AWS shared file paths must ride along with the connection
// hints, or file ops would silently fall back to ~/.aws while the listing
// used --aws-config/--aws-credentials.
func TestS3OptionFromListOption(t *testing.T) {
	o := Option{
		S3Uri:          "s3://bucket/prefix/",
		Profile:        "alpha",
		Region:         "us-west-2",
		PathStyle:      true,
		EndpointURL:    "https://oss.example.com",
		ConfigFile:     "/custom/aws-config",
		CredentialFile: "/custom/aws-credentials",
	}
	got := s3OptionFromListOption(o)
	if got.Profile != o.Profile || got.Region != o.Region ||
		got.Endpoint != o.EndpointURL || !got.UsePathStyle {
		t.Errorf("connection hints not mapped: %+v", got)
	}
	if got.ConfigFile != o.ConfigFile {
		t.Errorf("ConfigFile = %q, want %q", got.ConfigFile, o.ConfigFile)
	}
	if got.CredentialFile != o.CredentialFile {
		t.Errorf("CredentialFile = %q, want %q", got.CredentialFile, o.CredentialFile)
	}
}
