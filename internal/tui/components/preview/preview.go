// Package preview renders the floating content-preview overlay ('p'): a
// large centered box composited over the live layout (style.PlaceOverlay in
// tui.go) showing the FIRST 256 KiB of the highlighted file — a ranged
// GetObject for remote objects, a bounded read for local files. Valid UTF-8
// renders as scrollable hard-wrapped text; binary samples show a size note
// instead. The overlay follows the historyview pattern: the TUI opens it on
// 'p', swallows every other key while it is visible (except ctrl+c), scrolls
// it with j/k/pgup/pgdown/g/G, and closes it on esc/'p'.
package preview

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/strutil"
)

// MaxBytes is the preview sample size: only the first 256 KiB of a file is
// ever fetched, never the whole object.
const MaxBytes = 256 * 1024

// fetchTimeout bounds a preview fetch so a hung endpoint cannot leave the
// overlay loading forever.
const fetchTimeout = 30 * time.Second

// PreviewRequest identifies a remote target plus the connection hints needed
// to build a fresh S3 client for it. Objects and buckets are stamped with
// these hints at list-fetch time (see objectlist.listObjects /
// bucketlist.listBuckets), so the preview and metadata overlays never reach
// back into TUI state. Size carries the listed object size so a known-empty
// object skips the ranged fetch (a ranged GET on a zero-byte object is a
// 416, not an empty body).
type PreviewRequest struct {
	Profile     string
	EndpointURL string
	PathStyle   bool
	Region      string
	// ConfigFile/CredentialFile are the resolved AWS shared file paths
	// (--aws-config/--aws-credentials > env > ~/.aws default); empty keeps
	// the SDK defaults.
	ConfigFile     string
	CredentialFile string

	Bucket string
	Key    string
	Size   int64
}

// ContentMsg carries a fetched content sample. Seq echoes the fetch
// generation stamped by ShowRemote/ShowLocal; the Model drops messages from
// superseded fetches (and everything that lands after Hide bumped the seq).
// Total is the file's full size (-1 when unknown).
type ContentMsg struct {
	Seq   int
	Data  []byte
	Total int64
	Err   error
}

// Model is the content overlay state.
type Model struct {
	visible bool
	loading bool
	err     error
	title   string
	// seq identifies the newest fetch request (bumped by every Show and by
	// Hide, so closing the overlay drops in-flight results).
	seq int

	text   string // cleaned sample, valid-UTF-8 text only
	binary bool
	shown  int   // sample bytes fetched
	total  int64 // full size, -1 unknown

	lines []string // text hard-wrapped to wrapW, nil until content lands
	wrapW int      // inner width lines was wrapped at

	offset int
	width  int
	height int
}

// NewModel returns a hidden content overlay.
func NewModel() Model { return Model{total: -1} }

// Init is a no-op; fetching is kicked off by the Show calls.
func (m Model) Init() tea.Cmd { return nil }

// IsVisible reports whether the overlay is shown.
func (m Model) IsVisible() bool { return m.visible }

// SetSize sets the full-canvas dimensions the overlay sizes its box from and
// re-wraps the cached lines when the inner width changed.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	if m.lines != nil && m.wrapW != m.innerWidth() {
		m.rewrap()
	}
}

// Hide closes the overlay, invalidates any in-flight fetch and releases the
// sample so a closed preview does not pin up to 256 KiB until the next open.
func (m *Model) Hide() {
	m.visible = false
	m.seq++
	m.title = ""
	m.text = ""
	m.binary = false
	m.shown = 0
	m.total = -1
	m.lines = nil
	m.wrapW = 0
}

// open resets the overlay into its loading state for a new target.
func (m *Model) open(title string) {
	m.visible = true
	m.loading = true
	m.err = nil
	m.title = title
	m.text = ""
	m.binary = false
	m.shown = 0
	m.total = -1
	m.lines = nil
	m.wrapW = 0
	m.offset = 0
	m.seq++
}

// ShowRemote opens the overlay for an object and returns the Cmd that
// fetches its first MaxBytes via a ranged GetObject.
func (m *Model) ShowRemote(req *PreviewRequest) tea.Cmd {
	m.open(fmt.Sprintf("s3://%s/%s", req.Bucket, req.Key))
	seq := m.seq
	if req.Size == 0 {
		// A ranged GET on a zero-byte object fails with 416 InvalidRange;
		// render the empty state directly.
		return func() tea.Msg { return ContentMsg{Seq: seq, Total: 0} }
	}
	r := *req
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		cli, err := s3store.NewS3Client(ctx, s3store.S3Option{
			Profile:        r.Profile,
			Endpoint:       r.EndpointURL,
			UsePathStyle:   r.PathStyle,
			Region:         r.Region,
			ConfigFile:     r.ConfigFile,
			CredentialFile: r.CredentialFile,
		})
		if err != nil {
			return ContentMsg{Seq: seq, Err: err}
		}
		out, err := cli.Client().GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(r.Bucket),
			Key:    aws.String(r.Key),
			Range:  aws.String(fmt.Sprintf("bytes=0-%d", MaxBytes-1)),
		})
		if err != nil {
			return ContentMsg{Seq: seq, Err: err}
		}
		defer out.Body.Close() //nolint:errcheck // best-effort cleanup of the ranged body
		// The Range header is advisory: a server that ignores it responds 200
		// with the full body, so cap the read rather than trusting the status.
		data, err := io.ReadAll(io.LimitReader(out.Body, MaxBytes))
		if err != nil {
			return ContentMsg{Seq: seq, Err: err}
		}
		total := totalFromContentRange(aws.ToString(out.ContentRange))
		if total < 0 {
			total = r.Size
		}
		if total < int64(len(data)) {
			total = int64(len(data))
		}
		return ContentMsg{Seq: seq, Data: data, Total: total}
	}
}

// ShowLocal opens the overlay for a local file and returns the Cmd that
// reads its first MaxBytes off the Update goroutine.
func (m *Model) ShowLocal(path string) tea.Cmd {
	m.open(path)
	seq := m.seq
	return func() tea.Msg {
		// Stat (following symlinks, as navigation does) before opening: an
		// open(2) on a FIFO blocks until a writer appears, leaking the
		// goroutine, and device files are not previewable either.
		st, err := os.Stat(path)
		if err != nil {
			return ContentMsg{Seq: seq, Err: err}
		}
		if !st.Mode().IsRegular() {
			return ContentMsg{Seq: seq, Err: fmt.Errorf("not a regular file (%s)", st.Mode().Type())}
		}
		f, err := os.Open(path)
		if err != nil {
			return ContentMsg{Seq: seq, Err: err}
		}
		defer f.Close() //nolint:errcheck // read-only handle
		total := st.Size()
		data, err := io.ReadAll(io.LimitReader(f, MaxBytes))
		if err != nil {
			return ContentMsg{Seq: seq, Err: err}
		}
		if total < int64(len(data)) {
			total = int64(len(data))
		}
		return ContentMsg{Seq: seq, Data: data, Total: total}
	}
}

// totalFromContentRange parses the total size off a Content-Range header
// ("bytes 0-65535/123456"); -1 when absent or unparseable (".../*").
func totalFromContentRange(cr string) int64 {
	_, totalStr, ok := strings.Cut(cr, "/")
	if !ok {
		return -1
	}
	n, err := strconv.ParseInt(strings.TrimSpace(totalStr), 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// Update consumes ContentMsg; everything else passes through unchanged.
// Messages whose Seq does not match the newest fetch are stale results for
// a previously previewed item (or a closed overlay) and are dropped.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	cm, ok := msg.(ContentMsg)
	if !ok || cm.Seq != m.seq || !m.visible {
		return m, nil
	}
	m.loading = false
	m.err = cm.Err
	if cm.Err != nil {
		return m, nil
	}
	m.shown = len(cm.Data)
	m.total = cm.Total
	data := cm.Data
	if m.total > int64(len(data)) {
		// The sample was cut at a byte boundary: a multi-byte rune split at
		// MaxBytes is a truncation artifact, not binary evidence.
		data = trimPartialRune(data)
	}
	m.binary = isBinary(data)
	if !m.binary {
		m.text = cleanText(data)
		m.rewrap()
	}
	return m, nil
}

// isBinary reports whether the sample looks non-textual: any NUL byte, or
// more than 10% invalid UTF-8.
func isBinary(b []byte) bool {
	if bytes.IndexByte(b, 0) >= 0 {
		return true
	}
	invalid := 0
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			invalid++
		}
		i += size
	}
	return invalid*10 > len(b)
}

// trimPartialRune drops an incomplete multi-byte sequence off the tail of a
// truncated sample.
func trimPartialRune(b []byte) []byte {
	for i := 1; i < utf8.UTFMax && i <= len(b); i++ {
		c := b[len(b)-i]
		if utf8.RuneStart(c) {
			if !utf8.Valid(b[len(b)-i:]) {
				return b[:len(b)-i]
			}
			break
		}
	}
	return b
}

// cleanText normalizes the sample for the viewport: CRLF/CR fold to LF,
// tabs expand to four spaces, and remaining control characters (which would
// corrupt the terminal) are dropped.
func cleanText(b []byte) string {
	s := string(b)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\t", "    ")
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if (r < 0x20 && r != '\n') || r == 0x7f {
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

var (
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3b82f6")).
			Padding(0, 1)
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#e39f00ff")).
			Background(lipgloss.Color("#444745ff")).
			Padding(0, 1)
	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#aaaaaa"))
)

// boxSize is the overlay's outer box: width min(100, W-6), height H-4,
// with floors so a tiny terminal still renders something sane.
func (m Model) boxSize() (w, h int) {
	w = min(100, m.width-6)
	if w < 20 {
		w = 20
	}
	h = m.height - 4
	if h < 6 {
		h = 6
	}
	return w, h
}

// innerWidth is the content width inside the box frame.
func (m Model) innerWidth() int {
	w, _ := m.boxSize()
	inner := w - boxStyle.GetHorizontalFrameSize()
	if inner < 10 {
		inner = 10
	}
	return inner
}

// pageSize is how many content lines fit in the box: height minus the
// border frame, title and footer lines.
func (m Model) pageSize() int {
	_, h := m.boxSize()
	page := h - boxStyle.GetVerticalFrameSize() - 2
	if page < 1 {
		page = 1
	}
	return page
}

// rewrap rebuilds the cached display lines: the sample hard-wrapped
// (ANSI/CJK-safe) to the box's inner width, with the truncation marker
// appended when only part of the file is shown. Wrapping the full 256 KiB is
// too expensive to redo per keypress, so it happens once per content/resize.
func (m *Model) rewrap() {
	m.wrapW = m.innerWidth()
	m.lines = strings.Split(ansi.Hardwrap(m.text, m.wrapW, true), "\n")
	if m.truncated() {
		m.lines = append(m.lines, dimStyle.Render("── truncated at 256 KiB ──"))
	}
}

// contentLines returns the display lines wrapped by the last rewrap.
func (m Model) contentLines() []string {
	return m.lines
}

func (m Model) truncated() bool {
	return m.total > int64(m.shown)
}

// HandleKey scrolls the text (j/k, arrows, pgup/pgdown, g/G). Unrecognised
// keys are swallowed by design — the TUI forwards nothing else while the
// overlay is visible.
func (m *Model) HandleKey(key string) {
	if m.loading || m.err != nil || m.binary {
		return
	}
	total := len(m.contentLines())
	page := m.pageSize()
	switch key {
	case "j", "down":
		m.offset++
	case "k", "up":
		m.offset--
	case "pgdown":
		m.offset += page
	case "pgup":
		m.offset -= page
	case "g", "home":
		m.offset = 0
	case "G", "end":
		m.offset = total // clamped below
	}
	if max := total - page; m.offset > max {
		m.offset = max
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// centered places a one-line note in the middle of the content area.
func (m Model) centered(note string) string {
	return lipgloss.Place(m.innerWidth(), m.pageSize(),
		lipgloss.Center, lipgloss.Center, dimStyle.Render(note))
}

// View renders the floating box (title, content window, footer); tui.go
// composites it centered over the live layout via style.PlaceOverlay.
func (m Model) View() string {
	if !m.visible {
		return ""
	}
	w, h := m.boxSize()
	inner := m.innerWidth()
	page := m.pageSize()

	lines := []string{titleStyle.Render(ansi.Truncate(m.title, inner-2, "…"))}
	footer := "p/esc close"

	switch {
	case m.loading:
		lines = append(lines, m.centered("loading preview…"))
	case m.err != nil:
		lines = append(lines, strings.Split(
			ansi.Hardwrap("preview failed: "+m.err.Error(), inner, true), "\n")...)
	case m.binary:
		size := m.total
		if size < 0 {
			size = int64(m.shown)
		}
		lines = append(lines, m.centered(fmt.Sprintf("(binary file — %d bytes, no preview)", size)))
	case m.shown == 0:
		lines = append(lines, m.centered("(empty file)"))
	default:
		content := m.contentLines()
		offset := m.offset
		if max := len(content) - page; offset > max {
			offset = max
		}
		if offset < 0 {
			offset = 0
		}
		end := min(offset+page, len(content))
		lines = append(lines, content[offset:end]...)
		total := m.total
		if total < 0 {
			total = int64(m.shown)
		}
		footer = fmt.Sprintf("%d lines · %s shown of %s · j/k scroll · p/esc close",
			len(content), strutil.HumanizeBytes(int64(m.shown)), strutil.HumanizeBytes(total))
	}

	lines = append(lines, dimStyle.Render(ansi.Truncate(footer, inner, "…")))
	return boxStyle.Width(w).Height(h).Render(strings.Join(lines, "\n"))
}
