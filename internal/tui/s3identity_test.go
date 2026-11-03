// Tests that every path which builds an S3 client carries the FULL
// connection identity (profile, region, endpoint, path-style, shared file
// paths). A single missing field silently changes which account, region or
// endpoint the request goes to — the sync path shipped without a region
// for exactly this reason, and nothing failed loudly.
package tui

import (
	"testing"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/profilelist"
	"github.com/LinPr/lazys3/internal/tui/state"
)

// identityModel returns a model parked in the object list on a profile
// that has every connection field set to a distinguishable value, so a
// dropped field shows up as an empty string rather than a plausible
// default.
func identityModel(t *testing.T) Model {
	t.Helper()
	m := NewLazyS3Model()
	m.awsFiles.ConfigFile = "/custom/config"
	m.awsFiles.CredentialsFile = "/custom/credentials"
	m = updateModel(t, m, profilelist.ReadAwsConfigResult{Profiles: []profilelist.Profile{
		profilelist.NewProfileWithRegion("demo", "https://oss.example.com", "cn-hangzhou"),
	}})
	m.selectedProfile = "demo"
	m.selectedRegion = "cn-hangzhou"
	m.selectedBucket = "bkt"
	m.selectedObject = "dir/"
	m.state = state.ActiveObjectList
	return m
}

// wantIdentity is the connection identity every client builder must
// reproduce.
type wantIdentity struct {
	profile, region, endpoint, configFile, credentialFile string
	pathStyle                                             bool
}

func identityOf(t *testing.T, got s3store.S3Option) wantIdentity {
	t.Helper()
	return wantIdentity{
		profile:        got.Profile,
		region:         got.Region,
		endpoint:       got.Endpoint,
		configFile:     got.ConfigFile,
		credentialFile: got.CredentialFile,
		pathStyle:      got.UsePathStyle,
	}
}

// TestEveryS3OptionCarriesFullIdentity walks each Option-building entry
// point and asserts the identity survives. It is deliberately table-driven
// so a newly added builder is one line away from being covered.
func TestEveryS3OptionCarriesFullIdentity(t *testing.T) {
	want := wantIdentity{
		profile:        "demo",
		region:         "cn-hangzhou",
		endpoint:       "https://oss.example.com",
		configFile:     "/custom/config",
		credentialFile: "/custom/credentials",
		pathStyle:      true, // example.com is not AWS/OSS → path-style
	}

	m := identityModel(t)
	cases := map[string]func() s3store.S3Option{
		"object list (default)": func() s3store.S3Option {
			return toS3Option(m.objectListOptionFromState())
		},
		"object list (explicit uri)": func() s3store.S3Option {
			return toS3Option(m.objectListOption("s3://other/pre/"))
		},
		"sync connection params": func() s3store.S3Option {
			return syncS3Option(m.syncConnParams())
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			if got := identityOf(t, build()); got != want {
				t.Fatalf("identity = %+v\nwant %+v", got, want)
			}
		})
	}
}

// toS3Option mirrors the conversion objectlist.ListObjects performs
// internally (the package keeps its own copy of the identity fields).
func toS3Option(o objectlist.Option) s3store.S3Option {
	return s3store.S3Option{
		UsePathStyle:   o.PathStyle,
		Region:         o.Region,
		Profile:        o.Profile,
		Endpoint:       o.EndpointURL,
		ConfigFile:     o.ConfigFile,
		CredentialFile: o.CredentialFile,
	}
}

// TestSyncConnParamsCarriesRegion is the direct regression for the shipped
// bug: connParams had no region field, so every sync signed with
// us-east-1 while listings signed with the profile's region.
func TestSyncConnParamsCarriesRegion(t *testing.T) {
	m := identityModel(t)
	conn := m.syncConnParams()
	if conn.region != "cn-hangzhou" {
		t.Fatalf("connParams.region = %q, want cn-hangzhou", conn.region)
	}
	if got := syncS3Option(conn); got.Region != "cn-hangzhou" {
		t.Fatalf("sync S3Option.Region = %q, want cn-hangzhou", got.Region)
	}
}
