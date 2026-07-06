package style

import (
	"image/color"
	"path"
	"strings"

	"github.com/charmbracelet/lipgloss/v2"
)

// nerdFont gates icon rendering. Icons are opt-in ([ui] nerd_font = true)
// so the default rendering stays byte-identical to the icon-less layout.
var nerdFont bool

// SetNerdFont enables/disables Nerd Font icon rendering. Called once at
// startup from the loaded config, before any component is constructed.
func SetNerdFont(v bool) { nerdFont = v }

// NerdFontEnabled reports whether icon rendering is on.
func NerdFontEnabled() bool { return nerdFont }

// iconDef pairs a Nerd Font glyph with an optional hex color ("" = no
// color preference).
type iconDef struct {
	glyph string
	color string
}

// Fallback glyphs (Nerd Font v3 codepoints).
var (
	iconFile    = iconDef{"", "#8b949e"}          //  generic file
	iconDir     = iconDef{"\U000f024b", "#42a5f5"} // 󰉋 directory
	iconSymlink = iconDef{"", "#26c6da"}          //  symlink
	iconBucket  = iconDef{"\U000f0ccf", "#e39f00"} // 󰳏 bucket
)

// iconByName maps exact (lowercased) file names to icons.
var iconByName = map[string]iconDef{
	"makefile":           {"", "#6d8086"},          //
	"dockerfile":         {"\U000f0868", "#0db7ed"}, // 󰡨
	"docker-compose.yml": {"\U000f0868", "#0db7ed"},
	"go.mod":             {"", "#00add8"}, //
	"go.sum":             {"", "#00add8"},
	"license":            {"", "#d0bf41"}, //
	"readme":             {"", "#42a5f5"}, //
	"readme.md":          {"", "#42a5f5"},
	".gitignore":         {"", "#f4511e"}, //
	".gitattributes":     {"", "#f4511e"},
	"cmakelists.txt":     {"", "#6d8086"}, //
	"taskfile.yaml":      {"", "#6d8086"},
}

// iconByExt maps lowercased extensions (no dot) to icons.
var iconByExt = map[string]iconDef{
	// code
	"go":    {"", "#00add8"}, //
	"py":    {"", "#ffbc03"}, //
	"js":    {"", "#f1e05a"}, //
	"ts":    {"", "#3178c6"}, //
	"jsx":   {"", "#61dafb"}, //
	"tsx":   {"", "#61dafb"},
	"rs":    {"", "#dea584"}, //
	"c":     {"", "#599eff"}, //
	"cpp":   {"", "#599eff"}, //
	"cc":    {"", "#599eff"},
	"h":     {"", "#a074c4"}, //
	"hpp":   {"", "#a074c4"},
	"java":  {"", "#cc3e44"}, //
	"kt":    {"", "#7f52ff"}, //
	"rb":    {"", "#701516"}, //
	"php":   {"", "#a074c4"}, //
	"lua":   {"", "#51a0cf"}, //
	"swift": {"", "#e37933"}, //
	"sh":    {"", "#4d5a5e"}, //
	"bash":  {"", "#4d5a5e"},
	"zsh":   {"", "#4d5a5e"},
	"sql":   {"", "#dad8d8"}, //
	"html":  {"", "#e34c26"}, //
	"css":   {"", "#563d7c"}, //
	// text / config
	"md":   {"", "#42a5f5"}, //
	"txt":  {"", "#89e051"}, //
	"json": {"", "#cbcb41"}, //
	"yaml": {"", "#6d8086"}, //
	"yml":  {"", "#6d8086"},
	"toml": {"", "#6d8086"},
	"ini":  {"", "#6d8086"},
	"xml":  {"\U000f05c0", "#e37933"}, // 󰗀
	"csv":  {"\U000f021b", "#89e051"}, // 󰈛
	"log":  {"", "#7f8c8d"},          //
	"env":  {"", "#eed645"},          //
	"lock": {"", "#f1c40f"},          //
	// media
	"png":  {"\U000f021f", "#a074c4"}, // 󰈟
	"jpg":  {"\U000f021f", "#a074c4"},
	"jpeg": {"\U000f021f", "#a074c4"},
	"gif":  {"\U000f021f", "#a074c4"},
	"svg":  {"\U000f0721", "#ffb13b"}, // 󰜡
	"webp": {"\U000f021f", "#a074c4"},
	"mp4":  {"\U000f022b", "#fd971f"}, // 󰈫
	"mkv":  {"\U000f022b", "#fd971f"},
	"mov":  {"\U000f022b", "#fd971f"},
	"mp3":  {"\U000f0223", "#66d9ef"}, // 󰈣
	"wav":  {"\U000f0223", "#66d9ef"},
	"flac": {"\U000f0223", "#66d9ef"},
	// archives / binaries
	"zip": {"", "#eca517"}, //
	"tar": {"", "#eca517"},
	"gz":  {"", "#eca517"},
	"tgz": {"", "#eca517"},
	"bz2": {"", "#eca517"},
	"xz":  {"", "#eca517"},
	"7z":  {"", "#eca517"},
	"rar": {"", "#eca517"},
	"iso": {"\U000f02ca", "#f1c40f"}, // 󰋊
	"deb": {"", "#a80030"},          //
	"rpm": {"", "#cc0000"},          //
	"bin": {"", "#9f0500"},          //
	"exe": {"", "#9f0500"},
	// documents
	"pdf":  {"", "#b30b00"}, //
	"doc":  {"", "#295394"}, //
	"docx": {"", "#295394"},
	"xls":  {"", "#207245"}, //
	"xlsx": {"", "#207245"},
	"ppt":  {"", "#cb4a32"}, //
	"pptx": {"", "#cb4a32"},
}

// IconFor returns the Nerd Font glyph and color for the given entry. name
// may be a base name or a key path (with or without a trailing slash for
// directories); the lookup uses the base name's exact match first, then
// its extension, then the generic fallbacks. The glyph is empty when
// nerd_font is off, so callers can gate the icon column on it.
func IconFor(name string, isDir, isSymlink bool) (string, color.Color) {
	if !nerdFont {
		return "", nil
	}
	var d iconDef
	switch {
	case isSymlink:
		d = iconSymlink
	case isDir:
		d = iconDir
	default:
		base := strings.ToLower(path.Base(strings.TrimSuffix(name, "/")))
		if def, ok := iconByName[base]; ok {
			d = def
		} else if def, ok := iconByExt[strings.TrimPrefix(path.Ext(base), ".")]; ok && path.Ext(base) != "" {
			d = def
		} else {
			d = iconFile
		}
	}
	return d.glyph, lipgloss.Color(d.color)
}

// BucketIcon returns the bucket glyph for list titles. Empty when
// nerd_font is off.
func BucketIcon() (string, color.Color) {
	if !nerdFont {
		return "", nil
	}
	return iconBucket.glyph, lipgloss.Color(iconBucket.color)
}
