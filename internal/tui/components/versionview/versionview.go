// Package versionview renders a full-screen overlay listing the version
// history of a single object (versions and delete markers, newest first).
// It follows the historyview overlay pattern: the TUI opens it on 'v' over
// a highlighted file, swallows every other key while it is visible (except
// ctrl+c and 'x', which keeps cancelling the latest running transfer), and
// closes it on esc/'v'.
//
// Unlike historyview the overlay keeps a cursor: d/R/D act on the
// highlighted row by returning a tea.Cmd that emits an ActionMsg, which the
// TUI routes to the matching modal flow (see handler.go). While a modal
// opened from the overlay is active, the overlay stays open but the modal's
// canvas takes render precedence (tui.go View), so the overlay is hidden
// behind the modal and restored when it resolves.
package versionview

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/strutil"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
)

// ActionKind discriminates the per-row actions the overlay can request.
type ActionKind int

const (
	// ActionDownload downloads the highlighted version to a local path.
	ActionDownload ActionKind = iota
	// ActionRestore server-side copies the highlighted version on top of
	// the key, making it the latest version.
	ActionRestore
	// ActionDelete permanently removes the highlighted version (or delete
	// marker — removing the current marker undeletes the object).
	ActionDelete
)

// ActionMsg is emitted by HandleKey when the user triggers d/R/D on the
// highlighted row. It echoes the overlay's fetch parameters so the TUI's
// modal flows can build the op Cmd without reaching back into overlay state.
// Status/StatusKnown echo the bucket's versioning status shown in the
// overlay so the restore confirm can warn when it is not "Enabled".
type ActionMsg struct {
	Kind        ActionKind
	Opt         objectlist.Option
	Bucket      string
	Key         string
	Version     s3store.ObjectVersion
	Status      string
	StatusKnown bool
}

// Model is the versions overlay state.
type Model struct {
	visible bool
	loading bool
	loadErr error

	opt    objectlist.Option
	bucket string
	key    string
	// seq identifies the newest fetch request (bumped by Show/Refresh).
	// LoadedMsg echoes it so a stale in-flight listing — e.g. for the key
	// the overlay showed previously — is dropped instead of applied.
	seq int

	versions []s3store.ObjectVersion
	// status is the bucket's versioning status ("Enabled", "Suspended",
	// "" for never versioned); statusKnown is false when the status fetch
	// itself failed, so no hint line is drawn from a guess.
	status      string
	statusKnown bool

	cursor int
	offset int
	width  int
	height int
}

// NewModel returns a hidden versions overlay.
func NewModel() Model { return Model{} }

// Init is a no-op; loading is kicked off by the TUI when the overlay opens.
func (m Model) Init() tea.Cmd { return nil }

// Update consumes LoadedMsg; everything else passes through unchanged. The
// cursor is clamped (not reset) so a refresh after restore/delete keeps the
// highlight near the row the user was on.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if lm, ok := msg.(LoadedMsg); ok {
		// A superseded fetch (the overlay was re-shown for another key,
		// or refreshed, while this one was in flight) must not overwrite
		// the current listing: d/R/D would pair m.key with rows from the
		// stale key's history.
		if lm.Seq != m.seq {
			return m, nil
		}
		m.loading = false
		m.loadErr = lm.Err
		m.versions = lm.Versions
		m.status = lm.Status
		m.statusKnown = lm.StatusKnown
		m.clamp()
	}
	return m, nil
}

// Show opens the overlay in its loading state for the given object and
// returns the Cmd that fetches its listing. Hide closes it.
func (m *Model) Show(opt objectlist.Option, bucket, key string) tea.Cmd {
	m.visible = true
	m.loadErr = nil
	m.versions = nil
	m.status = ""
	m.statusKnown = false
	m.cursor = 0
	m.offset = 0
	m.opt = opt
	m.bucket = bucket
	m.key = key
	return m.fetch()
}

// fetch bumps the request sequence and returns the listing fetch Cmd for
// the overlay's current object; Update drops LoadedMsgs with an older seq.
func (m *Model) fetch() tea.Cmd {
	m.loading = true
	m.seq++
	return fetchCmd(m.opt, m.bucket, m.key, m.seq)
}

func (m *Model) Hide() { m.visible = false }

// IsVisible reports whether the overlay is shown.
func (m Model) IsVisible() bool { return m.visible }

// SetSize sets the overlay's full-canvas dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Refresh re-fetches the listing the overlay currently shows (after a
// restore/delete completed). Returns nil when the overlay is hidden.
func (m *Model) Refresh() tea.Cmd {
	if !m.visible {
		return nil
	}
	return m.fetch()
}

// SelectedVersion returns a copy of the highlighted row, or nil while
// loading / on error / on an empty history.
func (m Model) SelectedVersion() *s3store.ObjectVersion {
	if m.loading || m.loadErr != nil || m.cursor < 0 || m.cursor >= len(m.versions) {
		return nil
	}
	v := m.versions[m.cursor]
	return &v
}

// HandleKey moves the cursor (j/k/pgup/pgdown) or requests a row action
// (d/R/D, returned as an ActionMsg Cmd). Unrecognised keys are swallowed by
// design — the TUI forwards nothing else while the overlay is visible.
func (m *Model) HandleKey(key string) tea.Cmd {
	page := m.pageSize()
	switch key {
	case "j", "down":
		m.cursor++
	case "k", "up":
		m.cursor--
	case "pgdown":
		m.cursor += page
	case "pgup":
		m.cursor -= page
	case "d":
		return m.actionCmd(ActionDownload)
	case "R":
		return m.actionCmd(ActionRestore)
	case "D":
		return m.actionCmd(ActionDelete)
	}
	m.clamp()
	return nil
}

// actionCmd builds the ActionMsg Cmd for the highlighted row, or nil when
// no row is selectable (loading/error/empty).
func (m *Model) actionCmd(kind ActionKind) tea.Cmd {
	v := m.SelectedVersion()
	if v == nil {
		return nil
	}
	msg := ActionMsg{Kind: kind, Opt: m.opt, Bucket: m.bucket, Key: m.key, Version: *v,
		Status: m.status, StatusKnown: m.statusKnown}
	return func() tea.Msg { return msg }
}

// clamp keeps the cursor inside the listing and the scroll window around
// the cursor.
func (m *Model) clamp() {
	if m.cursor >= len(m.versions) {
		m.cursor = len(m.versions) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	page := m.pageSize()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+page {
		m.offset = m.cursor - page + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

var boxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("#3b82f6")).
	Padding(0, 1)

// pageSize is how many version rows fit in the box: total height minus the
// border frame, title, header, footer, and the hint line when shown.
func (m Model) pageSize() int {
	page := m.height - boxStyle.GetVerticalFrameSize() - 3 - m.hintLines()
	if page < 1 {
		page = 1
	}
	return page
}

func (m Model) hintLines() int {
	if m.hint() != "" {
		return 1
	}
	return 0
}

// hint returns the versioning-disabled hint line, or "" when versioning is
// enabled, still loading, errored, or the status fetch itself failed.
func (m Model) hint() string {
	if m.loading || m.loadErr != nil || !m.statusKnown || m.status == "Enabled" {
		return ""
	}
	return fmt.Sprintf("bucket versioning is %s — new writes do not create versions (V in the bucket list toggles it)",
		StatusLabel(m.status))
}

func (m Model) innerWidth() int {
	inner := m.width - boxStyle.GetHorizontalFrameSize()
	if inner < 20 {
		inner = 20
	}
	return inner
}

// ShortID abbreviates a version ID to its first 8 characters. Unversioned
// objects carry the literal "null" version; an empty ID renders the same.
func ShortID(id string) string {
	if id == "" {
		return "null"
	}
	r := []rune(id)
	if len(r) <= 8 {
		return id
	}
	return string(r[:8]) + "…"
}

// StatusLabel renders a GetBucketVersioning status for humans: the empty
// status means the bucket was never versioned.
func StatusLabel(status string) string {
	if status == "" {
		return "not enabled (never versioned)"
	}
	return status
}

// pad pads or truncates s to exactly w display cells.
func pad(s string, w int) string {
	if sw := ansi.StringWidth(s); sw > w {
		return ansi.Truncate(s, w, "…")
	} else if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

// renderRow renders one version fitted to inner cells:
//
//	▸ a1b2c3d4…    1.2M  2026-07-06 12:34  LATEST
func renderRow(v s3store.ObjectVersion, selected bool, inner int) string {
	const (
		idW   = 12
		sizeW = 8
		timeW = 16
	)
	marker := "  "
	if selected {
		marker = "▸ "
	}
	size := "-"
	if !v.IsDeleteMarker {
		size = strutil.HumanizeBytes(v.Size)
	}
	var flags []string
	if v.IsLatest {
		flags = append(flags, "LATEST")
	}
	if v.IsDeleteMarker {
		flags = append(flags, "DELETE-MARKER")
	}
	row := fmt.Sprintf("%s%s  %s  %s  %s",
		marker,
		pad(ShortID(v.VersionID), idW),
		pad(size, sizeW),
		pad(v.LastModified.Format("2006-01-02 15:04"), timeW),
		strings.Join(flags, " "),
	)
	return ansi.Truncate(row, inner, "")
}

// View renders the overlay: a full-canvas bordered box with a title,
// column header, the visible page of versions, an optional
// versioning-disabled hint, and a footer with the key legend.
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#e39f00ff")).
		Background(lipgloss.Color("#444745ff")).
		Padding(0, 1)
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#3b82f6"))
	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#aaaaaa"))

	inner := m.innerWidth()
	page := m.pageSize()

	lines := []string{
		titleStyle.Render(ansi.Truncate(fmt.Sprintf("lazys3 — versions of s3://%s/%s", m.bucket, m.key), inner-2, "…")),
		headerStyle.Render(ansi.Truncate(fmt.Sprintf("  %s  %s  %s  %s",
			pad("version", 12), pad("size", 8), pad("modified", 16), "flags"), inner, "")),
	}

	switch {
	case m.loading:
		lines = append(lines, dimStyle.Render("loading versions…"))
	case m.loadErr != nil:
		// Surface the storage error (e.g. NotImplemented on endpoints
		// without a versioning API) in the body rather than crashing.
		wrapped := ansi.Hardwrap("versions unavailable: "+m.loadErr.Error(), inner, true)
		for i, l := range strings.Split(wrapped, "\n") {
			if i >= page {
				break
			}
			lines = append(lines, dimStyle.Render(l))
		}
	case len(m.versions) == 0:
		lines = append(lines, dimStyle.Render("no versions found for this key"))
	default:
		end := m.offset + page
		if end > len(m.versions) {
			end = len(m.versions)
		}
		for i, v := range m.versions[m.offset:end] {
			lines = append(lines, renderRow(v, m.offset+i == m.cursor, inner))
		}
	}

	if h := m.hint(); h != "" {
		lines = append(lines, dimStyle.Render(ansi.Truncate(h, inner, "…")))
	}

	footer := fmt.Sprintf("%d versions", len(m.versions))
	if len(m.versions) > page {
		footer = fmt.Sprintf("%d-%d of %d", m.offset+1, min(m.offset+page, len(m.versions)), len(m.versions))
	}
	footer += " · j/k · d download · R restore · D delete · x cancel · esc close"
	lines = append(lines, dimStyle.Render(ansi.Truncate(footer, inner, "…")))

	box := boxStyle.Width(m.width)
	if m.height > 0 {
		box = box.Height(m.height)
	}
	return box.Render(strings.Join(lines, "\n"))
}
