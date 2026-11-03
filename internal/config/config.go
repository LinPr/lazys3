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
// strings like "#20e71c" (3- or 6-digit); empty keeps the default.
type Theme struct {
	// Mode picks a built-in palette ("dark" or "light"); the per-colour
	// keys below override individual entries on top of it.
	Mode            string `yaml:"mode"`
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

// S3 carries client-level knobs that some S3-compatible services need. The
// zero value is exactly the previous behaviour: signed requests, TLS
// verification on, ListObjectsV2, SDK default retries.
type S3 struct {
	// MaxRetries caps SDK retry attempts. Zero keeps the SDK default (3).
	MaxRetries int `yaml:"max_retries"`
	// UseListObjectsV1 selects the legacy ListObjects API. Some appliances
	// (and a few private clouds) never implemented ListObjectsV2, and
	// without this knob their buckets look empty.
	UseListObjectsV1 bool `yaml:"use_list_objects_v1"`
	// InsecureSkipVerify disables TLS certificate verification, for
	// endpoints with self-signed certificates (internal MinIO). It is a
	// real security downgrade, so it logs a warning at startup.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
	// NoSignRequest sends anonymous requests, for browsing public buckets.
	NoSignRequest bool `yaml:"no_sign_request"`
}

// Local configures the dual-pane local filesystem browser.
type Local struct {
	StartDir string `yaml:"start_dir"` // overrides the process cwd when set
}

// Profile configures the startup location for one AWS shared-config
// profile. When that profile is selected, lazys3 opens Bucket/Prefix
// directly instead of calling ListBuckets: bucket-scoped credentials
// (common in OSS/OBS/COS RAM policies and least-privilege S3 policies)
// often deny s3:ListAllMyBuckets, and without this the user would have to
// jump with 'g' on every launch. An empty Bucket disables the entry.
type Profile struct {
	Bucket string `yaml:"bucket"`
	Prefix string `yaml:"prefix"`
	// Endpoint and Region override the shared config's endpoint_url and
	// region for this profile. They exist so a profile can be pointed at
	// an internal/self-hosted endpoint, or at a different region, without
	// editing ~/.aws/config (which other tools share). Empty keeps the
	// shared-config value.
	Endpoint string `yaml:"endpoint"`
	Region   string `yaml:"region"`
}

// Config is the root of the config file schema.
type Config struct {
	Theme    Theme              `yaml:"theme"`
	UI       UI                 `yaml:"ui"`
	Local    Local              `yaml:"local"`
	S3       S3                 `yaml:"s3"`
	Profiles map[string]Profile `yaml:"profiles"`
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

// hexColorRe accepts the hex forms lipgloss parses: #rgb and #rrggbb.
// 8-digit #rrggbbaa is rejected here because lipgloss.Color silently
// turns it into NoColor.
var hexColorRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

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
	switch t.Mode {
	case "", "dark", "light":
	default:
		log.Printf("config: [theme] mode: invalid value %q (want dark|light), using dark", t.Mode)
		t.Mode = ""
	}
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

	if c.S3.MaxRetries < 0 {
		log.Printf("config: [s3] max_retries: %d is negative, using the SDK default", c.S3.MaxRetries)
		c.S3.MaxRetries = 0
	}
	if c.S3.InsecureSkipVerify {
		log.Printf("config: [s3] insecure_skip_verify is ON: TLS certificates are not verified")
	}
	if c.S3.NoSignRequest {
		log.Printf("config: [s3] no_sign_request is ON: requests are sent anonymously (read-only public buckets)")
	}

	// Profile start locations: a bucket is required; a prefix is optional
	// and always normalized to a "/"-terminated listing prefix. The bucket
	// is normalised as forgivingly as the prefix: users write the same
	// bucket as "my-bucket", "/my-bucket/" or "s3://my-bucket", and
	// silently dropping one of those spellings would send them back to the
	// ListBuckets call this feature exists to avoid. Only a bucket that
	// cannot be salvaged (empty, or still containing whitespace or an
	// interior "/") is dropped, with a log line.
	for name, p := range c.Profiles {
		p.Bucket = normalizeBucket(p.Bucket)
		p.Prefix = normalizeKeyPrefix(p.Prefix)
		p.Endpoint = strings.TrimSpace(p.Endpoint)
		p.Region = strings.TrimSpace(p.Region)
		if p.Endpoint != "" && !strings.Contains(p.Endpoint, "://") {
			// Accept a bare host ("oss-cn-hangzhou.aliyuncs.com"); the SDK
			// needs a scheme to tell it apart from a path.
			p.Endpoint = "https://" + p.Endpoint
		}
		if p.Bucket == "" {
			// No start location, but the entry may still carry an
			// endpoint/region override, which is legitimate on its own.
			if p.Endpoint == "" && p.Region == "" {
				log.Printf("config: [profiles.%s] has neither bucket nor endpoint/region, entry ignored", name)
				delete(c.Profiles, name)
				continue
			}
		} else if strings.ContainsAny(p.Bucket, "/ \t") {
			log.Printf("config: [profiles.%s] invalid bucket %q, entry ignored", name, p.Bucket)
			delete(c.Profiles, name)
			continue
		}
		c.Profiles[name] = p
	}
}

// normalizeBucket strips the accepted s3://-URI decorations from a bucket
// so "/my-bucket/", "s3://my-bucket" and "s3://my-bucket/" all mean the
// same bucket. An interior "/" (a whole object URI) is left alone for the
// caller's validation to reject: guessing which part is the bucket would
// be worse than refusing.
func normalizeBucket(b string) string {
	b = strings.TrimSpace(b)
	if rest, ok := strings.CutPrefix(b, "s3://"); ok {
		b = rest
	}
	b = strings.Trim(b, "/")
	return strings.TrimSpace(b)
}

// normalizeKeyPrefix trims surrounding whitespace and slashes and guarantees
// a single trailing "/" on a non-empty prefix. A prefix is a literal key
// prefix (not a path to resolve), so "." and ".." segments are left alone.
func normalizeKeyPrefix(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p + "/"
}

// defaultFile is the commented template written on first run. Every key is
// commented out so it parses to the zero Config.
const defaultFile = `# lazys3 configuration.
# All keys are optional; the commented values show the built-in defaults.

theme:
  # mode: "dark"                   # built-in palette: dark | light
  # Colors are hex strings: "#rgb" or "#rrggbb"; the keys below override
  # individual entries of the chosen palette.
  # focused_border: "#D9A441"    # border of the focused pane
  # unfocused_border: "#4A4238"  # border of the unfocused pane (dual-pane mode)
  # title_fg: "#1C1917"          # list/status chip foreground
  # title_bg: "#D9A441"          # list/status chip background
  # status_error_fg: "#ffffff"   # status-bar error text
  # selected_fg: ""              # highlighted list row foreground

ui:
  # nerd_font: false             # render Nerd Font file icons (needs a patched font)
  # default_sort: "name"         # initial sort field: name | size | time
  # sort_desc: false             # sort descending by default

local:
  # start_dir: ""                # local pane start directory, "~" ok (default: process cwd)

s3:
  # Client-level knobs, mostly for self-hosted / older S3-compatible services.
  # max_retries: 3               # SDK retry attempts (default: SDK's 3)
  # use_list_objects_v1: false   # legacy ListObjects, for services without V2
  # insecure_skip_verify: false  # skip TLS verification (self-signed endpoint)
  # no_sign_request: false       # anonymous requests (public buckets, read-only)

profiles:
  # Per-profile startup location. When the profile is selected, lazys3 opens
  # this bucket/prefix directly instead of calling ListBuckets, which
  # bucket-scoped credentials often deny. The prefix is optional and always
  # treated as a directory prefix ("logs" lists "logs/").
  # oss:
  #   bucket: "my-bucket"
  #   prefix: "some/prefix/"
  #   endpoint: "https://s3.internal.example.com"  # optional: override endpoint_url
  #   region: "cn-hangzhou"                        # optional: override region
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
