// Tests for the startup-location feature (start.go): the s3:// positional
// grammar, per-profile [profiles.<name>] config, the --profile auto-select,
// the ListBuckets skip, the lazy bucket-list fetch, and the region plumbing.
package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/config"
	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/profilelist"
	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
)

func TestParseStartLocation(t *testing.T) {
	cases := []struct {
		in     string
		bucket string
		prefix string
		want   bool
	}{
		{in: "s3://bkt", bucket: "bkt"},
		{in: "s3://bkt/", bucket: "bkt"},
		{in: "s3://bkt/logs/2026", bucket: "bkt", prefix: "logs/2026/"},
		{in: "  s3://bkt//a//b/  ", bucket: "bkt", prefix: "a/b/"},
		{in: "bkt/logs", want: true},
		{in: "s3://", want: true},
		{in: "s3://bad bucket/x", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseStartLocation(tc.in)
			if tc.want {
				if err == nil {
					t.Fatalf("ParseStartLocation(%q) = %#v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseStartLocation(%q): %v", tc.in, err)
			}
			if got.Bucket != tc.bucket || got.Prefix != tc.prefix {
				t.Fatalf("ParseStartLocation(%q) = %#v, want %q/%q", tc.in, got, tc.bucket, tc.prefix)
			}
		})
	}
}

// startModel builds a model with the given config and staged profiles, then
// returns it still parked in the profile list.
func startModel(t *testing.T, cfg config.Config, profiles ...profilelist.Profile) Model {
	t.Helper()
	m := NewLazyS3ModelWithConfig(cfg, config.AWSFiles{})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateModel(t, m, profilelist.ReadAwsConfigResult{Profiles: profiles})
	if m.state != state.ActiveProfileList {
		t.Fatalf("setup: state = %v, want ActiveProfileList", m.state)
	}
	return m
}

// TestProfileStartLocationSkipsListBuckets pins the core of the feature:
// selecting a profile with a configured start location opens the object
// list directly and never starts a ListBuckets fetch.
func TestProfileStartLocationSkipsListBuckets(t *testing.T) {
	cfg := config.Config{Profiles: map[string]config.Profile{
		"oss": {Bucket: "my-bucket", Prefix: "logs/2026/"},
	}}
	m := startModel(t, cfg,
		profilelist.NewProfileWithRegion("oss", "https://oss.example.com", "cn-hangzhou"),
		profilelist.NewProfile("aws", ""),
	)
	nm, _ := m.Update(enterPress())
	m = nm.(Model)

	if m.state != state.ActiveObjectList {
		t.Fatalf("state = %v, want ActiveObjectList", m.state)
	}
	if m.selectedProfile != "oss" || m.selectedBucket != "my-bucket" || m.selectedObject != "logs/2026/" {
		t.Fatalf("location = %q/%q/%q, want oss/my-bucket/logs/2026/",
			m.selectedProfile, m.selectedBucket, m.selectedObject)
	}
	if m.bucketList.Loading() || m.bucketList.Fetched() {
		t.Fatal("configured start location must not call ListBuckets")
	}
	if !m.objectlist.Loading() {
		t.Fatal("object list fetch was not armed")
	}
	// The object-list Option must carry the profile endpoint and region.
	opt := m.objectListOptionFromState()
	if opt.S3Uri != "s3://my-bucket/logs/2026/" || opt.Region != "cn-hangzhou" ||
		opt.EndpointURL != "https://oss.example.com" {
		t.Fatalf("object option = %#v, want endpoint/region/URI from the profile", opt)
	}
}

// TestProfileWithoutStartStillListsBuckets pins the unchanged behavior for
// profiles that have no configured start location.
func TestProfileWithoutStartStillListsBuckets(t *testing.T) {
	m := startModel(t, config.Config{}, profilelist.NewProfile("oss", ""))
	nm, _ := m.Update(enterPress())
	m = nm.(Model)
	if m.state != state.ActiveBucketList {
		t.Fatalf("state = %v, want ActiveBucketList", m.state)
	}
	if !m.bucketList.Loading() || m.bucketList.Fetched() {
		t.Fatal("normal profile selection should start a bucket-list fetch")
	}
}

// TestBackFromStartRootLazilyListsBuckets pins the fallback: a skipped
// bucket list is fetched the first time the user backs out to it.
func TestBackFromStartRootLazilyListsBuckets(t *testing.T) {
	cfg := config.Config{Profiles: map[string]config.Profile{
		"oss": {Bucket: "my-bucket"}, // bucket root: backspace leaves the list
	}}
	m := startModel(t, cfg, profilelist.NewProfile("oss", ""))
	nm, _ := m.Update(enterPress())
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	m = nm.(Model)

	if m.state != state.ActiveBucketList {
		t.Fatalf("state = %v, want ActiveBucketList", m.state)
	}
	if !m.bucketList.Loading() {
		t.Fatal("backing out to an unfetched bucket list should start its lazy fetch")
	}
	if m.bucketList.Fetched() {
		t.Fatal("lazy fetch should still be in flight")
	}
}

// TestCLIStartOverridesProfileConfig pins the precedence: the positional
// s3:// argument wins over [profiles.<name>] for the first selected
// profile, then is consumed so later profile switches use their own config.
func TestCLIStartOverridesProfileConfig(t *testing.T) {
	cli := StartLocation{Bucket: "cli-bucket", Prefix: "cli/prefix/"}
	cfg := config.Config{Profiles: map[string]config.Profile{
		"oss": {Bucket: "config-bucket", Prefix: "config/prefix/"},
	}}
	m := NewLazyS3ModelWithOptions(cfg, config.AWSFiles{}, Options{Start: &cli})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateModel(t, m, profilelist.ReadAwsConfigResult{Profiles: []profilelist.Profile{
		profilelist.NewProfile("oss", ""),
	}})

	nm, _ := m.Update(enterPress())
	m = nm.(Model)
	if m.selectedBucket != "cli-bucket" || m.selectedObject != "cli/prefix/" {
		t.Fatalf("started at %q/%q, want CLI location cli-bucket/cli/prefix/",
			m.selectedBucket, m.selectedObject)
	}
	if m.cliStart != nil {
		t.Fatal("CLI start location should be consumed after the first profile")
	}
}

// TestAutoSelectProfileFromFlag pins --profile: the picker auto-selects the
// named profile once the shared config is read, and its configured start
// location applies immediately.
func TestAutoSelectProfileFromFlag(t *testing.T) {
	cfg := config.Config{Profiles: map[string]config.Profile{
		"oss": {Bucket: "my-bucket", Prefix: "logs/"},
	}}
	m := NewLazyS3ModelWithOptions(cfg, config.AWSFiles{}, Options{Profile: "oss"})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateModel(t, m, profilelist.ReadAwsConfigResult{Profiles: []profilelist.Profile{
		profilelist.NewProfile("aws", ""),
		profilelist.NewProfile("oss", ""),
	}})

	if m.selectedProfile != "oss" {
		t.Fatalf("selectedProfile = %q, want oss", m.selectedProfile)
	}
	if m.state != state.ActiveObjectList || m.selectedBucket != "my-bucket" || m.selectedObject != "logs/" {
		t.Fatalf("state/location = %v/%q/%q, want ActiveObjectList/my-bucket/logs/",
			m.state, m.selectedBucket, m.selectedObject)
	}
	if m.startProfile != "" {
		t.Fatal("--profile should be consumed after the first profile listing")
	}
}

// TestAutoSelectProfileNotFoundSurfacesError pins the failure mode: an
// unknown --profile leaves the picker visible and reports on the status bar.
func TestAutoSelectProfileNotFoundSurfacesError(t *testing.T) {
	m := NewLazyS3ModelWithOptions(config.Config{}, config.AWSFiles{}, Options{Profile: "missing"})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	nm, cmd := m.Update(profilelist.ReadAwsConfigResult{Profiles: []profilelist.Profile{
		profilelist.NewProfile("oss", ""),
	}})
	m = nm.(Model)
	if m.state != state.ActiveProfileList {
		t.Fatalf("state = %v, want the picker to stay active", m.state)
	}
	var found bool
	for _, msg := range collectMsgs(cmd) {
		if em, ok := msg.(types.ErrMsg); ok && strings.Contains(em.Err.Error(), "missing") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing --profile did not surface a status-bar error")
	}
}

// TestBucketListSetOptionResetsFetched pins that switching profiles drops
// the previous account's listing and its fetched flag, so the lazy fetch
// runs for the new profile.
func TestBucketListOptionChangeResetsFetched(t *testing.T) {
	m := NewLazyS3Model()
	m.bucketList.SetBuckets([]bucketlist.Bucket{bucketlist.NewBucket("stale")})
	nm, _ := m.bucketList.Update(bucketlist.FetchBucketListResultMsg{Buckets: []bucketlist.Bucket{bucketlist.NewBucket("stale")}})
	m.bucketList = nm
	if !m.bucketList.Fetched() {
		t.Fatal("setup: expected a completed fetch")
	}
	m.bucketList.SetOption(&bucketlist.Option{Profile: "other"})
	if m.bucketList.Fetched() {
		t.Fatal("changing profile must reset the fetched flag")
	}
	if m.bucketList.GetSelectedBucket() != nil {
		t.Fatal("changing profile must clear the previous account's buckets")
	}
}
