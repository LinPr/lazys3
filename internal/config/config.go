// Package config loads the user-facing lazys3 configuration from
// $XDG_CONFIG_HOME/lazys3/config.yaml (falling back to
// ~/.config/lazys3/config.yaml). Every key is optional; the zero value of
// Config keeps today's built-in defaults, and invalid values fall back to
// the defaults with a log line — a broken config file never crashes the
// TUI. On first run a commented default file is written so the knobs are
// discoverable.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// Theme overrides the style package's package-level colors. Values are hex
// strings like "#20e71c" (3-, 6- or 8-digit); empty keeps the default.
type Theme struct {
	FocusedBorder   string `yaml:"focused_border"`
	UnfocusedBorder string `yaml:"unfocused_border"`
	TitleFg         string `yaml:"title_fg"`
	TitleBg         string `yaml:"title_bg"`
	StatusErrorFg   string `yaml:"status_error_fg"`
	SelectedFg      string `yaml:"selected_fg"`
}

// UI carries rendering and behavior knobs. NerdFont defaults to false so
// the default rendering stays byte-identical to the icon-less layout.
type UI struct {
	NerdFont    bool   `yaml:"nerd_font"`
	DefaultSort string `yaml:"default_sort"` // "name" | "size" | "time"
	SortDesc    bool   `yaml:"sort_desc"`
	// TransferPanelHeight is deprecated: the bottom transfer panel was
	// replaced by the full-screen transfers overlay ('t'). The key is
	// still parsed so old config files load without error, but its value
	// is ignored (sanitize logs and zeroes it).
	TransferPanelHeight int `yaml:"transfer_panel_height"`
}

// Local configures the dual-pane local filesystem browser.
type Local struct {
	StartDir string `yaml:"start_dir"` // overrides the process cwd when set
}

// Config is the root of the config file schema.
type Config struct {
	Theme Theme `yaml:"theme"`
	UI    UI    `yaml:"ui"`
	Local Local `yaml:"local"`
}

// Path returns the default config file location:
// $XDG_CONFIG_HOME/lazys3/config.yaml, falling back to
// ~/.config/lazys3/config.yaml. Empty when no home dir can be resolved.
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "lazys3", "config.yaml")
}

// Load reads the config. An empty path means the default location
// (Path()): a missing file there writes the commented template — unless a
// legacy config.toml sits next to it, in which case a hint is returned as
// warn and no template is written (the user's file must not be clobbered
// by a default one). A non-empty path is an explicit --config value: a
// missing, unreadable or malformed file is a hard error (the user asked
// for that file, silently ignoring it would be worse than stopping). The
// warn string, when non-empty, must be shown to the user by the caller —
// the standard logger is discarded in non-debug runs.
func Load(path string) (cfg Config, warn string, err error) {
	if path != "" {
		cfg, err = loadStrict(path)
		return cfg, "", err
	}
	def := Path()
	if def == "" {
		return Config{}, "", nil
	}
	if _, statErr := os.Stat(def); errors.Is(statErr, fs.ErrNotExist) {
		if legacy := strings.TrimSuffix(def, ".yaml") + ".toml"; fileExists(legacy) {
			return Config{}, fmt.Sprintf("config: %s: config.toml is no longer read — rename to config.yaml (YAML syntax)", legacy), nil
		}
		writeDefault(def)
		return Config{}, "", nil
	}
	return LoadFrom(def), "", nil
}

// loadStrict reads an explicit --config file. Unlike LoadFrom it does not
// forgive: a directory, an unreadable file or a YAML parse error all stop
// startup. Invalid individual values still fall back via sanitize.
func loadStrict(path string) (Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Config{}, fmt.Errorf("--config %s: %w", path, err)
	}
	if info.IsDir() {
		return Config{}, fmt.Errorf("--config %s: is a directory, want a YAML file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("--config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("--config %s: %v", path, err)
	}
	cfg.sanitize()
	return cfg, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// LoadFrom reads and validates the config at path. A missing or unreadable
// file logs and returns defaults; a malformed file logs the parse error
// and returns defaults.
func LoadFrom(path string) Config {
	var cfg Config
	if path == "" {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Println("config: read:", err)
		return cfg
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Printf("config: parse %s: %v (using defaults)", path, err)
		return Config{}
	}
	cfg.sanitize()
	return cfg
}

// hexColorRe accepts the hex forms lipgloss parses: #rgb, #rrggbb, #rrggbbaa.
var hexColorRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

func validColor(key, v string) string {
	if v == "" || hexColorRe.MatchString(v) {
		return v
	}
	log.Printf("config: [theme] %s: invalid color %q, using default", key, v)
	return ""
}

// sanitize validates every field in place, resetting invalid values to
// their zero (default) with a log line.
func (c *Config) sanitize() {
	t := &c.Theme
	t.FocusedBorder = validColor("focused_border", t.FocusedBorder)
	t.UnfocusedBorder = validColor("unfocused_border", t.UnfocusedBorder)
	t.TitleFg = validColor("title_fg", t.TitleFg)
	t.TitleBg = validColor("title_bg", t.TitleBg)
	t.StatusErrorFg = validColor("status_error_fg", t.StatusErrorFg)
	t.SelectedFg = validColor("selected_fg", t.SelectedFg)

	switch c.UI.DefaultSort {
	case "", "name", "size", "time":
	default:
		log.Printf("config: [ui] default_sort: invalid value %q (want name|size|time), using default", c.UI.DefaultSort)
		c.UI.DefaultSort = ""
	}
	if c.UI.TransferPanelHeight != 0 {
		log.Printf("config: [ui] transfer_panel_height is deprecated and ignored (transfers moved to the full-screen 't' overlay)")
		c.UI.TransferPanelHeight = 0
	}
	if d := c.Local.StartDir; d != "" {
		// Expand a leading "~" and resolve relative paths against the launch
		// cwd: locallist's parent navigation (filepath.Dir) needs an absolute
		// path, and a stored relative one would silently depend on wherever
		// lazys3 happens to be started from.
		if d == "~" || strings.HasPrefix(d, "~/") {
			if home, err := os.UserHomeDir(); err == nil && home != "" {
				d = filepath.Join(home, strings.TrimPrefix(d, "~"))
			}
		}
		if abs, err := filepath.Abs(d); err == nil {
			d = abs
		}
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			log.Printf("config: [local] start_dir: %q is not an existing directory, ignored", c.Local.StartDir)
			c.Local.StartDir = ""
		} else {
			c.Local.StartDir = d
		}
	}
}

// defaultFile is the commented template written on first run. Every key is
// commented out so it parses to the zero Config.
const defaultFile = `# lazys3 configuration.
# All keys are optional; the commented values show the built-in defaults.

theme:
  # Colors are hex strings: "#rgb", "#rrggbb" or "#rrggbbaa".
  # focused_border: "#20e71c"    # border of the focused pane
  # unfocused_border: "#555555"  # border of the unfocused pane (dual-pane mode)
  # title_fg: "#e39f00"          # status-bar profile chip foreground
  # title_bg: "#444745"          # status-bar profile chip background
  # status_error_fg: "#ffffff"   # status-bar error text
  # selected_fg: ""              # highlighted list row foreground

ui:
  # nerd_font: false             # render Nerd Font file icons (needs a patched font)
  # default_sort: "name"         # initial sort field: name | size | time
  # sort_desc: false             # sort descending by default

local:
  # start_dir: ""                # local pane start directory, "~" ok (default: process cwd)
`

// writeDefault writes the commented template, creating parent directories.
// Errors are deliberately silent (beyond the log): a read-only home must
// not break startup.
func writeDefault(path string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Println("config: write default:", err)
		return
	}
	if err := os.WriteFile(path, []byte(defaultFile), 0o644); err != nil {
		log.Println("config: write default:", err)
	}
}
