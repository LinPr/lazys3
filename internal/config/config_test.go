package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// zeroConfig reports whether cfg equals the zero Config. Config carries a
// map (profiles), so it is not comparable with ==.
func zeroConfig(cfg Config) bool {
	return reflect.DeepEqual(cfg, Config{})
}

func TestLoadValid(t *testing.T) {
	dir := t.TempDir()
	startDir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
theme:
  focused_border: "#ff0000"
  unfocused_border: "#333"
  title_fg: "#aabbcc"
  selected_fg: "#00ff00"

ui:
  nerd_font: true
  default_sort: "size"
  sort_desc: true
  transfer_panel_height: 8

local:
  start_dir: "` + startDir + `"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadFrom(path)
	if cfg.Theme.FocusedBorder != "#ff0000" {
		t.Errorf("FocusedBorder = %q", cfg.Theme.FocusedBorder)
	}
	if cfg.Theme.UnfocusedBorder != "#333" {
		t.Errorf("UnfocusedBorder = %q", cfg.Theme.UnfocusedBorder)
	}
	if cfg.Theme.TitleFg != "#aabbcc" {
		t.Errorf("TitleFg = %q", cfg.Theme.TitleFg)
	}
	if cfg.Theme.SelectedFg != "#00ff00" {
		t.Errorf("SelectedFg = %q", cfg.Theme.SelectedFg)
	}
	if !cfg.UI.NerdFont || !cfg.UI.SortDesc {
		t.Errorf("NerdFont/SortDesc = %v/%v, want true/true", cfg.UI.NerdFont, cfg.UI.SortDesc)
	}
	if cfg.UI.DefaultSort != "size" {
		t.Errorf("DefaultSort = %q", cfg.UI.DefaultSort)
	}
	// transfer_panel_height is deprecated: still parsed without error, but
	// ignored (zeroed) — the transfers view is a full-screen overlay now.
	if cfg.UI.TransferPanelHeight != 0 {
		t.Errorf("deprecated TransferPanelHeight should be ignored, got %d", cfg.UI.TransferPanelHeight)
	}
	if cfg.Local.StartDir != startDir {
		t.Errorf("StartDir = %q, want %q", cfg.Local.StartDir, startDir)
	}
}

func TestMissingDefaultWritesTemplateAndReturnsZero(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	cfg, warn, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if warn != "" {
		t.Errorf("no warning expected, got %q", warn)
	}
	if !zeroConfig(cfg) {
		t.Errorf("missing file should return zero config, got %+v", cfg)
	}
	path := filepath.Join(base, "lazys3", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("default file was not written: %v", err)
	}
	for _, want := range []string{"theme:", "ui:", "local:", "nerd_font", "start_dir"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("default file should mention %q", want)
		}
	}
	// The deprecated key must not be advertised to new users.
	if strings.Contains(string(data), "transfer_panel_height") {
		t.Error("default file should not mention the deprecated transfer_panel_height")
	}
	// The written template must itself parse to the zero config (all keys
	// commented out).
	if cfg2 := LoadFrom(path); !zeroConfig(cfg2) {
		t.Errorf("written default file should load as zero config, got %+v", cfg2)
	}
}

func TestLegacyTomlSkipsTemplate(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	dir := filepath.Join(base, "lazys3")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[ui]\nnerd_font = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, warn, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The TOML file is no longer read (defaults win)…
	if !zeroConfig(cfg) {
		t.Errorf("legacy toml must not be parsed, got %+v", cfg)
	}
	// …the caller gets a user-visible hint (the standard logger is
	// discarded in non-debug runs, so returning it is the only way the
	// user ever sees it)…
	if !strings.Contains(warn, "config.toml") || !strings.Contains(warn, "config.yaml") {
		t.Errorf("legacy toml should return a rename hint, got %q", warn)
	}
	// …and no YAML template may clobber the user's intent.
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); !os.IsNotExist(err) {
		t.Errorf("config.yaml template must not be written next to a legacy config.toml (stat err = %v)", err)
	}
}

func TestExplicitConfigMissingIsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	if _, _, err := Load(missing); err == nil {
		t.Fatal("Load with a missing explicit --config path should error")
	}
	// No template is written for an explicit path either.
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("no template should be written for an explicit path (stat err = %v)", err)
	}
}

func TestExplicitConfigLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.yaml")
	if err := os.WriteFile(path, []byte("ui:\n  nerd_font: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.UI.NerdFont {
		t.Error("explicit config file was not read")
	}
}

func TestExplicitConfigStrictErrors(t *testing.T) {
	// A directory stats fine but is not a config file.
	if _, _, err := Load(t.TempDir()); err == nil {
		t.Error("Load with --config pointing at a directory should error")
	}
	// A YAML parse error is a hard error for an explicit path (the
	// default location stays forgiving via LoadFrom).
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("ui: [this is not yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(bad); err == nil {
		t.Error("Load with a malformed explicit --config file should error")
	}
}

func TestBadValuesFallBackToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `
theme:
  focused_border: "not-a-color"
  title_fg: "#12"

ui:
  default_sort: "alphabetical"
  transfer_panel_height: 42
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadFrom(path)
	if cfg.Theme.FocusedBorder != "" || cfg.Theme.TitleFg != "" {
		t.Errorf("bad colors should reset: %+v", cfg.Theme)
	}
	if cfg.UI.DefaultSort != "" {
		t.Errorf("bad sort should reset, got %q", cfg.UI.DefaultSort)
	}
	if cfg.UI.TransferPanelHeight != 0 {
		t.Errorf("deprecated panel height should reset, got %d", cfg.UI.TransferPanelHeight)
	}
}

func TestMalformedFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("ui: [this is not yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg := LoadFrom(path); !zeroConfig(cfg) {
		t.Errorf("malformed file should return zero config, got %+v", cfg)
	}
}

func TestStartDirHonoredOnlyWhenDirExists(t *testing.T) {
	write := func(t *testing.T, startDir string) Config {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")
		body := "local:\n  start_dir: \"" + startDir + "\"\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return LoadFrom(path)
	}

	exists := t.TempDir()
	if cfg := write(t, exists); cfg.Local.StartDir != exists {
		t.Errorf("existing start_dir dropped: %q", cfg.Local.StartDir)
	}
	if cfg := write(t, "/no/such/dir/for/lazys3"); cfg.Local.StartDir != "" {
		t.Errorf("missing start_dir should be ignored, got %q", cfg.Local.StartDir)
	}
	// A file (not a directory) is rejected too.
	f := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg := write(t, f); cfg.Local.StartDir != "" {
		t.Errorf("non-directory start_dir should be ignored, got %q", cfg.Local.StartDir)
	}
}

func TestStartDirExpandsTildeAndRelative(t *testing.T) {
	write := func(t *testing.T, startDir string) Config {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")
		body := "local:\n  start_dir: \"" + startDir + "\"\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return LoadFrom(path)
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	if cfg := write(t, "~"); cfg.Local.StartDir != home {
		t.Errorf("~ should expand to %q, got %q", home, cfg.Local.StartDir)
	}
	// A relative path resolves against the process cwd, so the stored
	// value is always absolute (parent navigation depends on it).
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if cfg := write(t, "."); cfg.Local.StartDir != wd {
		t.Errorf(". should resolve to %q, got %q", wd, cfg.Local.StartDir)
	}
}

// TestProfileStartLocations pins the per-profile startup schema: the bucket
// is required, the prefix is normalized to a listing prefix, and malformed
// entries are dropped instead of making profile selection jump somewhere
// unintended.
func TestProfileStartLocations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `
profiles:
  oss:
    bucket: "my-bucket"
    prefix: "logs/2026"
  "  padded  ":
    bucket: "  padded-bucket  "
    prefix: "//a//b//"
  root:
    bucket: "root-bucket"
  no-bucket:
    prefix: "orphan/"
  slash:
    bucket: "bad/bucket"
  spaces:
    bucket: "bad bucket"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadFrom(path)
	if len(cfg.Profiles) != 3 {
		t.Fatalf("Profiles = %#v, want 3 valid entries", cfg.Profiles)
	}
	if got := cfg.Profiles["oss"]; got.Bucket != "my-bucket" || got.Prefix != "logs/2026/" {
		t.Errorf("oss = %#v, want bucket my-bucket prefix logs/2026/", got)
	}
	if got := cfg.Profiles["  padded  "]; got.Bucket != "padded-bucket" || got.Prefix != "a/b/" {
		t.Errorf("padded = %#v, want trimmed bucket and collapsed prefix", got)
	}
	if got := cfg.Profiles["root"]; got.Bucket != "root-bucket" || got.Prefix != "" {
		t.Errorf("root = %#v, want bucket root with an empty prefix", got)
	}
	for _, name := range []string{"no-bucket", "slash", "spaces"} {
		if _, ok := cfg.Profiles[name]; ok {
			t.Errorf("invalid profiles entry %q should be dropped", name)
		}
	}
}

// TestDefaultTemplateDocumentsProfiles keeps the generated template and the
// parser in sync: a new user must be able to discover the per-profile start
// location, and the template must still load as the zero Config.
func TestDefaultTemplateDocumentsProfiles(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if _, _, err := Load(""); err != nil {
		t.Fatalf("Load: %v", err)
	}
	path := filepath.Join(base, "lazys3", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"profiles:", "bucket:", "prefix:", "ListBuckets"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("default file should mention %q", want)
		}
	}
	if cfg := LoadFrom(path); cfg.Profiles != nil {
		t.Errorf("written default file should load with no profiles, got %#v", cfg.Profiles)
	}
}

// TestProfileBucketAcceptsURIAndSlashSpellings pins the forgiving bucket
// normalization: the same bucket written as a bare name, an s3:// URI, or
// with surrounding slashes must all be accepted. Regression: a "/" was
// treated as "invalid", so "/my-bucket/" silently dropped the whole entry
// and profile selection fell back to ListBuckets — the exact call this
// feature exists to avoid for bucket-scoped credentials.
func TestProfileBucketAcceptsURIAndSlashSpellings(t *testing.T) {
	cases := []struct{ in, want string }{
		{"my-bucket", "my-bucket"},
		{"/my-bucket/", "my-bucket"},
		{"s3://my-bucket", "my-bucket"},
		{"s3://my-bucket/", "my-bucket"},
		{"  s3://my-bucket/  ", "my-bucket"},
		{"/my-bucket", "my-bucket"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			body := "profiles:\n  oss:\n    bucket: \"" + tc.in + "\"\n    prefix: \"logs\"\n"
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := LoadFrom(path)
			got, ok := cfg.Profiles["oss"]
			if !ok {
				t.Fatalf("bucket %q was dropped; want it normalized to %q", tc.in, tc.want)
			}
			if got.Bucket != tc.want {
				t.Errorf("bucket = %q, want %q", got.Bucket, tc.want)
			}
			if got.Prefix != "logs/" {
				t.Errorf("prefix = %q, want logs/", got.Prefix)
			}
		})
	}
}

// TestProfileBucketRejectsUnsalvageable pins that only genuinely unusable
// buckets are dropped: an interior "/" (a whole object URI) is refused
// rather than guessed at, and whitespace stays invalid.
func TestProfileBucketRejectsUnsalvageable(t *testing.T) {
	for _, bad := range []string{"s3://bucket/key", "my/bucket", "my bucket", "", "   "} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		body := "profiles:\n  oss:\n    bucket: \"" + bad + "\"\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := LoadFrom(path)
		if _, ok := cfg.Profiles["oss"]; ok {
			t.Errorf("bucket %q should be rejected, got %+v", bad, cfg.Profiles["oss"])
		}
	}
}
