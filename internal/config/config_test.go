package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadValid(t *testing.T) {
	dir := t.TempDir()
	startDir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[theme]
focused_border = "#ff0000"
unfocused_border = "#333"
title_fg = "#aabbccdd"
selected_fg = "#00ff00"

[ui]
nerd_font = true
default_sort = "size"
sort_desc = true
transfer_panel_height = 8

[local]
start_dir = "` + startDir + `"
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
	if cfg.Theme.TitleFg != "#aabbccdd" {
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

func TestMissingFileWritesDefaultsAndReturnsZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lazys3", "config.toml")
	cfg := LoadFrom(path)
	if cfg != (Config{}) {
		t.Errorf("missing file should return zero config, got %+v", cfg)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("default file was not written: %v", err)
	}
	for _, want := range []string{"[theme]", "[ui]", "[local]", "nerd_font", "start_dir"} {
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
	if cfg2 := LoadFrom(path); cfg2 != (Config{}) {
		t.Errorf("written default file should load as zero config, got %+v", cfg2)
	}
}

func TestBadValuesFallBackToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
[theme]
focused_border = "not-a-color"
title_fg = "#12"

[ui]
default_sort = "alphabetical"
transfer_panel_height = 42
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
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("this is not toml ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg := LoadFrom(path); cfg != (Config{}) {
		t.Errorf("malformed file should return zero config, got %+v", cfg)
	}
}

func TestStartDirHonoredOnlyWhenDirExists(t *testing.T) {
	write := func(t *testing.T, startDir string) Config {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.toml")
		body := "[local]\nstart_dir = \"" + startDir + "\"\n"
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
		path := filepath.Join(t.TempDir(), "config.toml")
		body := "[local]\nstart_dir = \"" + startDir + "\"\n"
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
