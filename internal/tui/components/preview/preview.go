// Package preview renders the metadata/content preview panel for the
// currently selected profile, bucket, or object.
package preview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/charmbracelet/bubbles/v2/list"
	"github.com/charmbracelet/bubbles/v2/viewport"
	tea "github.com/charmbracelet/bubbletea/v2"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
)

// PreviewItem is the contract every previewable list item must satisfy.
// GetPreviewContent returns a synchronous fallback (used when no live fetch
// is possible, e.g. for a Profile, or as immediate content while the async
// fetch is in flight); GetPreviewRequest returns the parameters needed to
// fetch live metadata/content asynchronously. A nil return from
// GetPreviewRequest means "use the synchronous content path" — the preview
// component will call GetPreviewContent and skip the tea.Cmd.
type PreviewItem interface {
	list.Item
	GetPreviewContent() string
	GetPreviewRequest() *PreviewRequest
}

// PreviewRequest is the union of fields the preview component needs to
// fetch live data for any of the three item kinds (profile/bucket/object).
// Bucket-scoped fields are populated for bucket & object items; key-scoped
// fields are populated only for object items; Profile items return nil.
//
// Kind discriminates the source: "profile" | "bucket" | "object". For
// "profile" the preview component falls back to GetPreviewContent (the
// request itself is unused).
type PreviewRequest struct {
	Kind string

	// Connection hints shared by all kinds.
	Profile     string
	EndpointURL string
	PathStyle   bool
	Region      string

	// Bucket-scoped.
	Bucket string

	// Object-scoped.
	Key     string
	IsDir   bool
	Size    int64
	ModTime time.Time
	Storage string
	ETag    string
}

// fetchTimeout bounds a live preview fetch so a hung endpoint cannot leave
// the panel loading forever.
const fetchTimeout = 30 * time.Second

// PreviewContentMsg is emitted by fetchContentCmd when the live fetch
// completes. Content holds the rendered string (metadata block, optionally
// followed by a ranged body for textual objects). Err is non-nil on fetch
// failure; the component surfaces it in place of the content. Token
// identifies the request the message belongs to so stale fetches from a
// previously highlighted item can be dropped.
type PreviewContentMsg struct {
	Token   string
	Content string
	Err     error
}

// requestToken derives a stable identity for a fetch request so late
// completions of superseded fetches can be discarded.
func requestToken(req *PreviewRequest) string {
	return strings.Join([]string{req.Kind, req.Profile, req.EndpointURL, req.Bucket, req.Key}, "\x00")
}

// Model is the preview panel model. It owns a viewport, the current
// pending/loaded content, and a loading flag so the caller can show a
// "loading..." state while the fetch tea.Cmd is in flight.
type Model struct {
	title       string
	content     string
	pendingItem PreviewItem
	// lastKey identifies the item whose preview was last dispatched.
	// SetContent runs on every Update pass of the root model, so it must
	// be a no-op when the highlighted item hasn't changed — otherwise each
	// PreviewContentMsg would trigger another live fetch, forever.
	lastKey      string
	pendingToken string
	loading      bool
	viewport     viewport.Model
	width        int
	height       int
	visible      bool
}

func NewPreviewModel() Model {
	vp := viewport.New()
	return Model{
		title:    "Preview",
		content:  "No item selected",
		visible:  false,
		viewport: vp,
	}
}

// SetContent sets the item to preview and returns a tea.Cmd that performs
// the live fetch. Callers in tui.go must execute the returned cmd so the
// PreviewContentMsg arrives. Items that return a nil PreviewRequest (e.g.
// a Profile) fall back to the synchronous GetPreviewContent path and return
// nil — no async fetch is needed.
func (pm *Model) SetContent(item PreviewItem) tea.Cmd {
	if item == nil {
		pm.title = "Preview"
		pm.content = "No item selected"
		pm.pendingItem = nil
		pm.pendingToken = ""
		pm.lastKey = ""
		pm.loading = false
		pm.viewport.SetContent(pm.content)
		return nil
	}

	req := item.GetPreviewRequest()
	key := previewKey(item, req)
	if key == pm.lastKey {
		// Same item as the current/pending preview: nothing to do.
		return nil
	}

	pm.pendingItem = item
	// Render the synchronous fallback immediately so the panel shows
	// metadata while the live fetch is in flight.
	pm.content = item.GetPreviewContent()
	pm.viewport.SetContent(pm.content)

	if !pm.visible {
		// Don't spend a live fetch on a hidden panel. lastKey stays empty
		// so the first SetContent after the panel opens fetches.
		pm.pendingToken = ""
		pm.lastKey = ""
		pm.loading = false
		return nil
	}
	pm.lastKey = key
	if req == nil {
		// Synchronous-only item (Profile). No live fetch needed.
		pm.pendingToken = ""
		pm.loading = false
		return nil
	}
	pm.pendingToken = requestToken(req)
	pm.loading = true
	return pm.fetchContentCmd(req)
}

// Invalidate clears the last-item memo so the next SetContent re-fetches
// even for an unchanged selection. Used after ops that change what the
// preview shows without changing the highlighted item (e.g. a bucket
// versioning toggle updates the bucket preview's Versioning line).
func (pm *Model) Invalidate() { pm.lastKey = "" }

// Keyer is an optional interface synchronous-only items implement when
// their FilterValue is not a unique identity (local entries filter on the
// base name, which collides across directories).
type Keyer interface {
	PreviewKey() string
}

// previewKey builds the identity key SetContent uses to detect whether the
// highlighted item changed. Requests reuse the fetch token; synchronous-only
// items use their PreviewKey when provided, falling back to FilterValue.
func previewKey(item PreviewItem, req *PreviewRequest) string {
	if req == nil {
		if k, ok := item.(Keyer); ok {
			return "sync\x00" + k.PreviewKey()
		}
		return "sync\x00" + item.FilterValue()
	}
	return requestToken(req)
}

// fetchContentCmd builds a fresh S3 client from the request's connection
// hints and fetches the live preview. Bucket requests run HeadBucket +
// GetBucketLocation + GetBucketVersioning; object directories run a
// ListObjectsV2 with MaxKeys=1 to surface an empty/non-empty probe; object
// files run HeadObject and, for textual content-types, a ranged GetObject
// (bytes=0-65535) appended to the metadata block.
func (pm *Model) fetchContentCmd(req *PreviewRequest) tea.Cmd {
	token := requestToken(req)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		opt := s3store.S3Option{
			Profile:      req.Profile,
			Endpoint:     req.EndpointURL,
			UsePathStyle: req.PathStyle,
			Region:       req.Region,
		}
		cli, err := s3store.NewS3Client(ctx, opt)
		if err != nil {
			return PreviewContentMsg{Token: token, Err: err}
		}
		s3cli := cli.Client()

		switch req.Kind {
		case "bucket":
			content, err := previewBucket(ctx, s3cli, req.Bucket, req.EndpointURL)
			if err != nil {
				return PreviewContentMsg{Token: token, Err: err}
			}
			return PreviewContentMsg{Token: token, Content: content}
		case "object":
			if req.IsDir {
				content, err := previewDir(ctx, s3cli, req.Bucket, req.Key)
				if err != nil {
					return PreviewContentMsg{Token: token, Err: err}
				}
				return PreviewContentMsg{Token: token, Content: content}
			}
			content, err := previewObject(ctx, s3cli, req)
			if err != nil {
				return PreviewContentMsg{Token: token, Err: err}
			}
			return PreviewContentMsg{Token: token, Content: content}
		}
		return PreviewContentMsg{Token: token, Err: fmt.Errorf("unknown preview kind: %q", req.Kind)}
	}
}

// previewBucket renders the live metadata for a bucket: HeadBucket (region),
// GetBucketLocation (location constraint), GetBucketVersioning (status).
// Failures of any sub-call degrade gracefully — the field is rendered as
// "-" rather than aborting the whole preview.
func previewBucket(ctx context.Context, s3cli *s3.Client, bucket, endpointURL string) (string, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Bucket:     %s\n", bucket)
	if endpointURL != "" {
		fmt.Fprintf(&sb, "Endpoint:   %s\n", endpointURL)
	}

	regionHint := ""
	if out, err := s3cli.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	}); err == nil && out != nil && out.BucketRegion != nil {
		regionHint = aws.ToString(out.BucketRegion)
	}
	fmt.Fprintf(&sb, "Region:     %s\n", orDash(regionHint))

	locStr := ""
	if out, err := s3cli.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucket),
	}); err == nil && out != nil && out.LocationConstraint != "" {
		locStr = string(out.LocationConstraint)
	}
	fmt.Fprintf(&sb, "Location:   %s\n", orDash(locStr))

	verStr := ""
	if out, err := s3cli.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	}); err == nil && out != nil {
		verStr = string(out.Status)
	}
	fmt.Fprintf(&sb, "Versioning: %s\n", orDash(verStr))
	return sb.String(), nil
}

// previewDir renders a summary for an object prefix: an empty/non-empty
// probe via ListObjectsV2 with MaxKeys=1 (KeyCount is capped at 1 by
// MaxKeys, so it can only tell whether any key matches the prefix, not
// how many).
func previewDir(ctx context.Context, s3cli *s3.Client, bucket, key string) (string, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Key:   %s\n", key)
	sb.WriteString("Type:  directory\n")

	out, err := s3cli.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		Prefix:  aws.String(key),
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		fmt.Fprintf(&sb, "Children: %s\n", orDash(""))
		return sb.String(), nil
	}
	// MaxKeys=1 caps KeyCount at 1, so this is an empty/non-empty probe,
	// not an exact child count.
	if aws.ToInt32(out.KeyCount) > 0 {
		sb.WriteString("Children: non-empty\n")
	} else {
		sb.WriteString("Children: empty\n")
	}
	return sb.String(), nil
}

// previewObject renders HeadObject metadata and, for textual content-types,
// appends a ranged GetObject body (first 64 KiB). Binary/image types show
// metadata only — image rendering in terminal is out of scope for this pass.
func previewObject(ctx context.Context, s3cli *s3.Client, req *PreviewRequest) (string, error) {
	head, err := s3cli.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(req.Bucket),
		Key:    aws.String(req.Key),
	})
	if err != nil {
		// HeadObject on a directory key may 404 on some services; fall
		// back to the dir preview path rather than surfacing an error.
		var nf *types.NotFound
		var nk *types.NoSuchKey
		if errors.As(err, &nf) || errors.As(err, &nk) {
			return previewDir(ctx, s3cli, req.Bucket, req.Key)
		}
		return "", err
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Key:           %s\n", req.Key)
	if head.ContentLength != nil {
		fmt.Fprintf(&sb, "Size:          %d bytes\n", aws.ToInt64(head.ContentLength))
	}
	if head.LastModified != nil {
		fmt.Fprintf(&sb, "Modified:      %s\n", head.LastModified.Format(time.RFC3339))
	}
	if head.ETag != nil {
		fmt.Fprintf(&sb, "ETag:          %s\n", aws.ToString(head.ETag))
	}
	if head.ContentType != nil {
		fmt.Fprintf(&sb, "ContentType:   %s\n", aws.ToString(head.ContentType))
	}
	if head.StorageClass != "" {
		fmt.Fprintf(&sb, "StorageClass:  %s\n", string(head.StorageClass))
	}
	if head.VersionId != nil {
		fmt.Fprintf(&sb, "VersionId:     %s\n", aws.ToString(head.VersionId))
	}

	contentType := ""
	if head.ContentType != nil {
		contentType = aws.ToString(head.ContentType)
	}
	if isTextual(contentType, req.Key) {
		body, berr := rangedGetText(ctx, s3cli, req.Bucket, req.Key)
		if berr == nil && body != "" {
			sb.WriteString("\n--- content (first 64 KiB) ---\n")
			sb.WriteString(body)
		} else if berr != nil {
			fmt.Fprintf(&sb, "\n(content fetch failed: %v)\n", berr)
		}
	}
	return sb.String(), nil
}

// rangedGetText fetches bytes=0-65535 of the object and returns it as a
// UTF-8-safe string. Trailing NUL bytes are stripped so binary content that
// happens to slip past the textual sniff does not break the viewport.
func rangedGetText(ctx context.Context, s3cli *s3.Client, bucket, key string) (string, error) {
	out, err := s3cli.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Range:  aws.String("bytes=0-65535"),
	})
	if err != nil {
		return "", err
	}
	defer out.Body.Close() //nolint:errcheck // best-effort cleanup of the ranged GetObject body
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return "", err
	}
	body = bytesClean(body)
	return string(body), nil
}

// isTextual returns true if the content-type or the file extension suggests
// the object is human-readable text.
func isTextual(contentType, key string) bool {
	ct := strings.ToLower(contentType)
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json", "application/xml", "application/x-yaml",
		"application/yaml", "application/x-sh", "application/javascript",
		"application/x-javascript":
		return true
	}
	if ct == "" {
		switch strings.ToLower(ext(key)) {
		case ".txt", ".md", ".markdown", ".log", ".csv", ".tsv",
			".json", ".xml", ".yaml", ".yml", ".ini", ".cfg", ".toml",
			".go", ".py", ".js", ".ts", ".rs", ".c", ".h", ".cpp", ".hpp",
			".java", ".rb", ".sh", ".tf", ".sql", ".html", ".htm", ".css":
			return true
		}
	}
	return false
}

// ext returns the lowercase file extension (including the dot) for key, or
// "" if the key has no extension.
func ext(key string) string {
	idx := strings.LastIndexByte(key, '.')
	if idx < 0 {
		return ""
	}
	return key[idx:]
}

// bytesClean strips bytes that would visually corrupt the viewport (NULs
// and most control characters except newline/tab/carriage-return). It stops
// at the first NUL, treating the object as binary past that point.
func bytesClean(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		if c < 0x20 && c != '\n' && c != '\t' && c != '\r' {
			continue
		}
		out = append(out, c)
	}
	return out
}

// orDash returns s when non-empty, else "-" — a uniform "no data" marker
// for the metadata block.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// SetSize sets the preview panel size.
func (pm *Model) SetSize(width, height int) {
	pm.width = width
	pm.height = height
}

func (pm *Model) Show()          { pm.visible = true }
func (pm *Model) Hide()          { pm.visible = false }
func (pm *Model) Toggle()        { pm.visible = !pm.visible }
func (pm Model) IsVisible() bool { return pm.visible }

func (pm Model) Init() tea.Cmd { return nil }

// Update handles PreviewContentMsg by loading the fetched content into the
// viewport and clearing the loading flag. Messages whose Token does not
// match the pending request are stale fetches for a previously highlighted
// item and are dropped. All other messages are ignored — the preview panel
// is otherwise passive.
func (pm Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case PreviewContentMsg:
		if msg.Token != pm.pendingToken {
			return pm, nil
		}
		pm.loading = false
		if msg.Err != nil {
			pm.content = fmt.Sprintf("preview error: %v", msg.Err)
		} else {
			pm.content = msg.Content
		}
		pm.viewport.SetContent(pm.content)
	}
	return pm, nil
}

// View renders the preview panel. When hidden it returns "" so the panel
// collapses out of the layout.
func (pm Model) View() string {
	if !pm.visible {
		return ""
	}
	pm.viewport.SetWidth(pm.width)
	pm.viewport.SetHeight(pm.height)
	return pm.viewport.View()
}
