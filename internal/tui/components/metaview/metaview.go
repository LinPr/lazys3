// Package metaview renders the floating metadata overlay ('m'): a centered
// box composited over the live layout (style.PlaceOverlay in tui.go) showing
// every populated metadata field of the highlighted item as aligned
// "Key: value" rows — HeadObject for remote objects (plus a ListObjectsV2
// probe for prefixes), HeadBucket/GetBucketLocation/GetBucketVersioning for
// buckets, lstat + owner lookup for local entries, and the shared-config
// facts for profiles. Empty fields are omitted rather than printed blank.
// The overlay follows the historyview pattern: the TUI opens it on 'm',
// swallows every other key while it is visible (except ctrl+c), scrolls it
// with j/k/pgup/pgdown/g/G, and closes it on esc/'m'.
package metaview

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/strutil"
	"github.com/LinPr/lazys3/internal/tui/components/preview"
)

// fetchTimeout bounds a metadata fetch so a hung endpoint cannot leave the
// overlay loading forever.
const fetchTimeout = 30 * time.Second

// timeFormat renders timestamps in local time.
const timeFormat = "2006-01-02 15:04:05 MST"

// Row is one display line: a Key/Value pair rendered as an aligned
// "Key:  value" row, a section heading (Key empty, Value set), or a blank
// separator (both empty).
type Row struct {
	Key   string
	Value string
}

// LoadedMsg carries the fetched rows. Seq echoes the fetch generation
// stamped by the Show calls; the Model drops messages from superseded
// fetches (and everything that lands after Hide bumped the seq).
type LoadedMsg struct {
	Seq  int
	Rows []Row
	Err  error
}

// Model is the metadata overlay state.
type Model struct {
	visible bool
	loading bool
	err     error
	title   string
	rows    []Row
	// seq identifies the newest fetch request (bumped by every Show and by
	// Hide, so closing the overlay drops in-flight results).
	seq int

	offset int
	width  int
	height int
}

// NewModel returns a hidden metadata overlay.
func NewModel() Model { return Model{} }

// Init is a no-op; fetching is kicked off by the Show calls.
func (m Model) Init() tea.Cmd { return nil }

// IsVisible reports whether the overlay is shown.
func (m Model) IsVisible() bool { return m.visible }

// SetSize sets the full-canvas dimensions the overlay sizes its box from.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Hide closes the overlay and invalidates any in-flight fetch.
func (m *Model) Hide() {
	m.visible = false
	m.seq++
}

// open resets the overlay into its loading state for a new target.
func (m *Model) open(title string) {
	m.visible = true
	m.loading = true
	m.err = nil
	m.title = title
	m.rows = nil
	m.offset = 0
	m.seq++
}

// Update consumes LoadedMsg; everything else passes through unchanged.
// Messages whose Seq does not match the newest fetch are stale results for
// a previously shown item (or a closed overlay) and are dropped.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	lm, ok := msg.(LoadedMsg)
	if !ok || lm.Seq != m.seq || !m.visible {
		return m, nil
	}
	m.loading = false
	m.err = lm.Err
	m.rows = lm.Rows
	return m, nil
}

// ShowObject opens the overlay for a remote object (HeadObject) or prefix
// (an empty/non-empty ListObjectsV2 probe) and returns the fetch Cmd.
func (m *Model) ShowObject(req *preview.PreviewRequest, isDir bool) tea.Cmd {
	m.open(fmt.Sprintf("s3://%s/%s", req.Bucket, req.Key))
	seq := m.seq
	r := *req
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		cli, err := newClient(ctx, r)
		if err != nil {
			return LoadedMsg{Seq: seq, Err: err}
		}
		if isDir {
			return LoadedMsg{Seq: seq, Rows: dirRows(ctx, cli, r.Bucket, r.Key)}
		}
		head, err := cli.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(r.Bucket),
			Key:    aws.String(r.Key),
		})
		if err != nil {
			// HeadObject on a directory-marker key may 404 on some services;
			// fall back to the prefix probe rather than surfacing an error.
			var nf *s3types.NotFound
			var nk *s3types.NoSuchKey
			if errors.As(err, &nf) || errors.As(err, &nk) {
				return LoadedMsg{Seq: seq, Rows: dirRows(ctx, cli, r.Bucket, r.Key)}
			}
			return LoadedMsg{Seq: seq, Err: err}
		}
		return LoadedMsg{Seq: seq, Rows: objectRows(r.Key, head)}
	}
}

// ShowBucket opens the overlay for a bucket
// (HeadBucket/GetBucketLocation/GetBucketVersioning) and returns the fetch
// Cmd. Sub-call failures degrade to omitted fields rather than aborting.
func (m *Model) ShowBucket(req *preview.PreviewRequest) tea.Cmd {
	m.open("s3://" + req.Bucket)
	seq := m.seq
	r := *req
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		cli, err := newClient(ctx, r)
		if err != nil {
			return LoadedMsg{Seq: seq, Err: err}
		}
		return LoadedMsg{Seq: seq, Rows: bucketRows(ctx, cli, r)}
	}
}

// ShowLocal opens the overlay for a local filesystem entry and returns the
// Cmd that lstats it (plus readlink/owner/entry-count lookups) off the
// Update goroutine.
func (m *Model) ShowLocal(path string) tea.Cmd {
	m.open(path)
	seq := m.seq
	return func() tea.Msg {
		rows, err := localRows(path)
		return LoadedMsg{Seq: seq, Rows: rows, Err: err}
	}
}

// ShowProfile opens the overlay for an AWS shared-config profile. All the
// facts are already in hand, so there is no fetch Cmd and no loading state.
func (m *Model) ShowProfile(name, endpointURL, region string, configFiles []string) {
	m.open(name)
	m.loading = false
	m.rows = profileRows(name, endpointURL, region, configFiles)
}

// newClient builds a fresh S3 client from the request's connection hints.
func newClient(ctx context.Context, r preview.PreviewRequest) (*s3.Client, error) {
	cli, err := s3store.NewS3Client(ctx, s3store.S3Option{
		Profile:      r.Profile,
		Endpoint:     r.EndpointURL,
		UsePathStyle: r.PathStyle,
		Region:       r.Region,
	})
	if err != nil {
		return nil, err
	}
	return cli.Client(), nil
}

// addRow appends a Key/Value row, omitting empty values.
func addRow(rows []Row, key, value string) []Row {
	if value == "" {
		return rows
	}
	return append(rows, Row{Key: key, Value: value})
}

// humanSize renders "1.2K (1234 bytes)".
func humanSize(n int64) string {
	return fmt.Sprintf("%s (%d bytes)", strutil.HumanizeBytes(n), n)
}

// objectRows renders every populated HeadObject field, with the user
// metadata (x-amz-meta-*, prefix already stripped by the SDK) under its own
// heading.
func objectRows(key string, head *s3.HeadObjectOutput) []Row {
	rows := []Row{{Key: "Key", Value: key}}
	if head.ContentLength != nil {
		rows = addRow(rows, "Size", humanSize(aws.ToInt64(head.ContentLength)))
	}
	rows = addRow(rows, "Content-Type", aws.ToString(head.ContentType))
	rows = addRow(rows, "ETag", aws.ToString(head.ETag))
	if head.PartsCount != nil {
		rows = addRow(rows, "Parts-Count", fmt.Sprintf("%d", aws.ToInt32(head.PartsCount)))
	}
	if head.LastModified != nil {
		rows = addRow(rows, "Last-Modified", head.LastModified.Local().Format(timeFormat))
	}
	rows = addRow(rows, "Storage-Class", string(head.StorageClass))
	rows = addRow(rows, "Archive-Status", string(head.ArchiveStatus))
	rows = addRow(rows, "Version-ID", aws.ToString(head.VersionId))
	rows = addRow(rows, "Cache-Control", aws.ToString(head.CacheControl))
	rows = addRow(rows, "Content-Encoding", aws.ToString(head.ContentEncoding))
	rows = addRow(rows, "Content-Disposition", aws.ToString(head.ContentDisposition))
	rows = addRow(rows, "Content-Language", aws.ToString(head.ContentLanguage))
	rows = addRow(rows, "Expires", aws.ToString(head.ExpiresString))
	rows = addRow(rows, "Expiration", aws.ToString(head.Expiration))
	rows = addRow(rows, "Website-Redirect", aws.ToString(head.WebsiteRedirectLocation))
	rows = addRow(rows, "SSE", string(head.ServerSideEncryption))
	rows = addRow(rows, "SSE-KMS-Key", aws.ToString(head.SSEKMSKeyId))
	rows = addRow(rows, "SSE-C-Algorithm", aws.ToString(head.SSECustomerAlgorithm))
	if head.BucketKeyEnabled != nil {
		rows = addRow(rows, "SSE-Bucket-Key", strconv.FormatBool(aws.ToBool(head.BucketKeyEnabled)))
	}
	rows = addRow(rows, "Checksum-CRC32", aws.ToString(head.ChecksumCRC32))
	rows = addRow(rows, "Checksum-CRC32C", aws.ToString(head.ChecksumCRC32C))
	rows = addRow(rows, "Checksum-CRC64NVME", aws.ToString(head.ChecksumCRC64NVME))
	rows = addRow(rows, "Checksum-SHA1", aws.ToString(head.ChecksumSHA1))
	rows = addRow(rows, "Checksum-SHA256", aws.ToString(head.ChecksumSHA256))
	rows = addRow(rows, "Checksum-Type", string(head.ChecksumType))
	rows = addRow(rows, "Replication", string(head.ReplicationStatus))
	rows = addRow(rows, "Restore", aws.ToString(head.Restore))
	rows = addRow(rows, "Object-Lock-Mode", string(head.ObjectLockMode))
	if head.ObjectLockRetainUntilDate != nil {
		rows = addRow(rows, "Object-Lock-Retain", head.ObjectLockRetainUntilDate.Local().Format(timeFormat))
	}
	rows = addRow(rows, "Object-Lock-Hold", string(head.ObjectLockLegalHoldStatus))
	if head.MissingMeta != nil {
		rows = addRow(rows, "Missing-Meta", fmt.Sprintf("%d", aws.ToInt32(head.MissingMeta)))
	}
	if len(head.Metadata) > 0 {
		rows = append(rows, Row{}, Row{Value: "User metadata"})
		keys := make([]string, 0, len(head.Metadata))
		for k := range head.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			rows = addRow(rows, k, head.Metadata[k])
		}
	}
	return rows
}

// dirRows renders a prefix summary: an empty/non-empty probe via
// ListObjectsV2 with MaxKeys=1 (which caps KeyCount at 1, so it can only
// tell whether any key matches the prefix, not how many).
func dirRows(ctx context.Context, cli *s3.Client, bucket, key string) []Row {
	rows := []Row{
		{Key: "Key", Value: key},
		{Key: "Type", Value: "directory (prefix)"},
	}
	out, err := cli.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		Prefix:  aws.String(key),
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return rows
	}
	children := "empty"
	if aws.ToInt32(out.KeyCount) > 0 {
		children = "non-empty"
	}
	return addRow(rows, "Children", children)
}

// bucketRows renders the bucket facts: name, endpoint/profile in use, then
// the live region/location/versioning probes (each omitted on failure).
func bucketRows(ctx context.Context, cli *s3.Client, r preview.PreviewRequest) []Row {
	rows := []Row{{Key: "Name", Value: r.Bucket}}
	rows = addRow(rows, "Endpoint", r.EndpointURL)
	rows = addRow(rows, "Profile", r.Profile)

	region := ""
	if out, err := cli.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(r.Bucket),
	}); err == nil && out != nil {
		region = aws.ToString(out.BucketRegion)
	}
	if region == "" {
		region = r.Region
	}
	rows = addRow(rows, "Region", region)

	if out, err := cli.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(r.Bucket),
	}); err == nil && out != nil && out.LocationConstraint != "" {
		rows = addRow(rows, "Location", string(out.LocationConstraint))
	}

	if out, err := cli.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(r.Bucket),
	}); err == nil && out != nil {
		status := string(out.Status)
		if status == "" {
			status = "not enabled"
		}
		rows = addRow(rows, "Versioning", status)
	}
	return rows
}

// localRows renders a local entry: identity, type (with the symlink target
// and whether it exists), size or directory entry count, permissions,
// owner/group and the three timestamps (access/change only where the
// platform stat carries them; see statExtra).
func localRows(path string) ([]Row, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	rows := []Row{
		{Key: "Name", Value: info.Name()},
		{Key: "Path", Value: path},
	}

	// eff describes the entry the size/permission/time rows talk about: the
	// symlink's target when it resolves, the entry itself otherwise.
	eff := info
	isLink := info.Mode()&fs.ModeSymlink != 0
	switch {
	case isLink:
		target, _ := os.Readlink(path)
		rows = addRow(rows, "Type", "symlink → "+target)
		if tinfo, err := os.Stat(path); err == nil {
			rows = addRow(rows, "Target exists", "yes")
			eff = tinfo
		} else {
			rows = addRow(rows, "Target exists", "no (broken link)")
		}
	case info.IsDir():
		rows = addRow(rows, "Type", "directory")
	default:
		rows = addRow(rows, "Type", "file")
	}

	if eff.IsDir() {
		if ents, err := os.ReadDir(path); err == nil {
			rows = addRow(rows, "Size", fmt.Sprintf("%d entries", len(ents)))
		}
	} else {
		rows = addRow(rows, "Size", humanSize(eff.Size()))
	}
	rows = addRow(rows, "Permissions", fmt.Sprintf("%s (%04o)", eff.Mode(), eff.Mode().Perm()))
	if uid, gid, atime, ctime, ok := statExtra(eff); ok {
		rows = addRow(rows, "Owner", lookupOwner(uid))
		rows = addRow(rows, "Group", lookupGroup(gid))
		rows = addRow(rows, "Modified", eff.ModTime().Local().Format(timeFormat))
		rows = addRow(rows, "Accessed", atime.Local().Format(timeFormat))
		rows = addRow(rows, "Changed", ctime.Local().Format(timeFormat))
	} else {
		rows = addRow(rows, "Modified", eff.ModTime().Local().Format(timeFormat))
	}
	return rows, nil
}

// lookupOwner resolves a uid, falling back to the bare number when the id
// has no passwd entry.
func lookupOwner(id uint32) string {
	s := strconv.FormatUint(uint64(id), 10)
	if u, err := user.LookupId(s); err == nil && u.Username != "" {
		return fmt.Sprintf("%s (%s)", u.Username, s)
	}
	return s
}

// lookupGroup resolves a gid, falling back to the bare number.
func lookupGroup(id uint32) string {
	s := strconv.FormatUint(uint64(id), 10)
	if g, err := user.LookupGroupId(s); err == nil && g.Name != "" {
		return fmt.Sprintf("%s (%s)", g.Name, s)
	}
	return s
}

// profileRows renders the shared-config profile facts (what the old preview
// panel showed): name, config file paths, endpoint_url and region.
func profileRows(name, endpointURL, region string, configFiles []string) []Row {
	rows := []Row{{Key: "Profile", Value: name}}
	for _, f := range configFiles {
		rows = addRow(rows, "Config file", f)
	}
	rows = addRow(rows, "Endpoint", endpointURL)
	rows = addRow(rows, "Region", region)
	return rows
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
	headingStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#3b82f6"))
	keyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#dddddd"))
	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#aaaaaa"))
)

// boxWidth is the overlay's outer width: min(80, W-6) with a floor.
func (m Model) boxWidth() int {
	w := min(80, m.width-6)
	if w < 20 {
		w = 20
	}
	return w
}

// innerWidth is the content width inside the box frame.
func (m Model) innerWidth() int {
	inner := m.boxWidth() - boxStyle.GetHorizontalFrameSize()
	if inner < 10 {
		inner = 10
	}
	return inner
}

// maxPageSize is the tallest content window the terminal allows: the H-4
// box budget minus the border frame, title and footer lines.
func (m Model) maxPageSize() int {
	page := m.height - 4 - boxStyle.GetVerticalFrameSize() - 2
	if page < 1 {
		page = 1
	}
	return page
}

// contentLines renders the rows fitted to the inner width: aligned
// "Key:  value" pairs, bold section headings, blank separators.
func (m Model) contentLines() []string {
	inner := m.innerWidth()
	keyW := 0
	for _, r := range m.rows {
		if r.Key != "" {
			if w := ansi.StringWidth(r.Key) + 1; w > keyW {
				keyW = w
			}
		}
	}
	keyW += 2 // gutter
	lines := make([]string, 0, len(m.rows))
	for _, r := range m.rows {
		switch {
		case r.Key == "" && r.Value == "":
			lines = append(lines, "")
		case r.Key == "":
			lines = append(lines, headingStyle.Render(ansi.Truncate(r.Value, inner, "…")))
		default:
			line := keyStyle.Render(pad(r.Key+":", keyW)) + r.Value
			lines = append(lines, ansi.Truncate(line, inner, "…"))
		}
	}
	return lines
}

// pad pads s to exactly w display cells.
func pad(s string, w int) string {
	if sw := ansi.StringWidth(s); sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

// HandleKey scrolls the rows (j/k, arrows, pgup/pgdown, g/G). Unrecognised
// keys are swallowed by design — the TUI forwards nothing else while the
// overlay is visible.
func (m *Model) HandleKey(key string) {
	total := len(m.contentLines())
	page := m.maxPageSize()
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

// View renders the floating box (title, row window, footer); tui.go
// composites it centered over the live layout via style.PlaceOverlay. The
// box height fits the content, capped at the H-4 budget (scrolling past
// that).
func (m Model) View() string {
	if !m.visible {
		return ""
	}
	inner := m.innerWidth()
	page := m.maxPageSize()

	var content []string
	switch {
	case m.loading:
		content = []string{dimStyle.Render("loading metadata…")}
	case m.err != nil:
		content = strings.Split(ansi.Hardwrap("metadata failed: "+m.err.Error(), inner, true), "\n")
	default:
		content = m.contentLines()
	}

	offset := m.offset
	if max := len(content) - page; offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}
	end := min(offset+page, len(content))

	footer := "m/esc close"
	if len(content) > page {
		footer = fmt.Sprintf("%d-%d of %d · j/k scroll · m/esc close", offset+1, end, len(content))
	}

	lines := []string{titleStyle.Render(ansi.Truncate(m.title, inner-2, "…"))}
	lines = append(lines, content[offset:end]...)
	lines = append(lines, dimStyle.Render(ansi.Truncate(footer, inner, "…")))
	return boxStyle.Width(m.boxWidth()).Render(strings.Join(lines, "\n"))
}
