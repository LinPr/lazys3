package objectlist

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/storage/uri"
	"github.com/LinPr/lazys3/internal/strutil"
	"github.com/LinPr/lazys3/internal/tui/components/preview"
	tea "github.com/charmbracelet/bubbletea/v2"
)

// Object represents a single entry in the object list. A directory entry
// (CommonPrefix) has isDir=true and only a name; a file entry carries the
// full S3 metadata surfaced by ListObjectsV2.
//
// Connection hints (bucket, profile, endpointURL, region, pathStyle) are
// stamped onto each Object by listObjects from the Option used to fetch the
// list, so the preview layer can build a fresh S3 client without reaching
// back into TUI state.
type Object struct {
	name         string
	prefix       string // listing prefix the object was fetched under
	size         int64
	modTime      time.Time
	storageClass string
	etag         string
	isDir        bool

	// Connection hints populated by listObjects from the active Option.
	bucket      string
	profile     string
	endpointURL string
	region      string
	pathStyle   bool
}

// Name returns the object's key (full path for files, prefix for directories).
func (o Object) Name() string { return o.name }

// DisplayName returns the key relative to the listing prefix ("one.txt"
// inside "syncdir/", child prefixes as "sub/"). Operations keep using the
// full key via Name/Title; only rendering and filtering use this.
func (o Object) DisplayName() string {
	name := strings.TrimPrefix(o.name, o.prefix)
	if name == "" {
		return o.name
	}
	return name
}
func (o Object) Size() int64          { return o.size }
func (o Object) ModTime() time.Time   { return o.modTime }
func (o Object) StorageClass() string { return o.storageClass }
func (o Object) ETag() string         { return o.etag }
func (o Object) IsDir() bool          { return o.isDir }

// Option carries the configuration needed to build an S3 client for object
// listing. EndpointURL/PathStyle are plumbed from the active profile so
// non-AWS services (Aliyun OSS, Huawei OBS, Tencent COS, MinIO) connect
// through the correct endpoint with path-style addressing.
type Option struct {
	S3Uri       string
	Profile     string
	Region      string
	PathStyle   bool
	EndpointURL string
}

func (o Object) Title() string { return o.name }

// Description renders a one-line metadata summary for the list delegate.
// Directories return "<dir>"; files render humanised size + mod-time so the
// list reads like a file browser.
func (o Object) Description() string {
	if o.isDir {
		return "<dir>"
	}
	return fmt.Sprintf("%s  %s",
		strutil.HumanizeBytes(o.size),
		o.modTime.Format("2006-01-02 15:04"))
}

func (o Object) FilterValue() string { return o.DisplayName() }

// GetPreviewRequest returns the parameters the preview/metadata overlays
// need to fetch live content or metadata for this object. The connection
// hints and bucket name are stamped onto the Object by listObjects at fetch
// time, so the returned request is fully populated and the overlay layer
// can build a fresh S3 client directly.
func (o Object) GetPreviewRequest() *preview.PreviewRequest {
	return &preview.PreviewRequest{
		Bucket:      o.bucket,
		Key:         o.name,
		Size:        o.size,
		Profile:     o.profile,
		EndpointURL: o.endpointURL,
		PathStyle:   o.pathStyle,
		Region:      o.region,
	}
}

type FetchObjectListResultMsg struct {
	Objects []Object
	Err     error
}

// fetchTimeout bounds an object-list fetch so a hung endpoint cannot leave
// the TUI loading forever.
const fetchTimeout = 30 * time.Second

func FetchObjectListCmd(o Option) tea.Cmd {
	log.Printf("FetchObjectListCmd option: S3Uri=%q Profile=%q Region=%q PathStyle=%v Endpoint=%q", o.S3Uri, o.Profile, o.Region, o.PathStyle, o.EndpointURL)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		opt := s3store.S3Option{
			UsePathStyle: o.PathStyle,
			Region:       o.Region,
			Profile:      o.Profile,
			Endpoint:     o.EndpointURL,
		}

		cli, err := s3store.NewS3Client(ctx, opt)
		if err != nil {
			return FetchObjectListResultMsg{Err: err}
		}

		if o.S3Uri == "" {
			return FetchObjectListResultMsg{Err: fmt.Errorf("s3uri is empty")}
		}

		parsedURI, err := uri.ParseS3Uri(o.S3Uri)
		if err != nil {
			return FetchObjectListResultMsg{Err: err}
		}
		if parsedURI.GetBucket() == "" {
			return FetchObjectListResultMsg{Err: fmt.Errorf("bucket is empty")}
		}
		objects, err := listObjects(ctx, cli, parsedURI.GetBucket(), parsedURI.GetKey(), o)
		if err != nil {
			return FetchObjectListResultMsg{Err: err}
		}
		// An empty prefix is valid (e.g. a bucket with no objects yet).
		// Surface an empty list rather than an error so the UI can render
		// the empty state.
		return FetchObjectListResultMsg{Objects: objects, Err: nil}
	}
}

// listObjects lists the CommonPrefixes (directories) and Contents (files)
// for the given bucket/prefix. Each Object is stamped with the connection
// hints from the active Option so the preview layer can issue follow-up
// calls (HeadObject/GetObject) on the same endpoint/profile/path-style.
// Directories are sorted before files; each group is sorted alphabetically
// (case-insensitive).
func listObjects(ctx context.Context, cli *s3store.S3Store, bucket, key string, o Option) ([]Object, error) {
	prefixes, objs, err := cli.ListObjectsWithPagination(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	objectList := make([]Object, 0, len(prefixes)+len(objs))
	for _, prefix := range prefixes {
		if prefix.Prefix == nil {
			continue
		}
		objectList = append(objectList, Object{
			name:        aws.ToString(prefix.Prefix),
			prefix:      key,
			isDir:       true,
			bucket:      bucket,
			profile:     o.Profile,
			endpointURL: o.EndpointURL,
			region:      o.Region,
			pathStyle:   o.PathStyle,
		})
	}
	for _, obj := range objs {
		if obj.Key == nil {
			continue
		}
		o := Object{
			name:         aws.ToString(obj.Key),
			prefix:       key,
			size:         aws.ToInt64(obj.Size),
			storageClass: string(obj.StorageClass),
			etag:         aws.ToString(obj.ETag),
			isDir:        false,
			bucket:       bucket,
			profile:      o.Profile,
			endpointURL:  o.EndpointURL,
			region:       o.Region,
			pathStyle:    o.PathStyle,
		}
		if obj.LastModified != nil {
			o.modTime = *obj.LastModified
		}
		objectList = append(objectList, o)
	}
	sortSlice(objectList)
	return objectList, nil
}

// NewDirObject constructs a directory entry with the given prefix key.
// Parent-package tests use it to stage a listing without a live fetch.
func NewDirObject(name string) Object { return Object{name: name, isDir: true} }

// NewFileObject constructs a file entry with the given key. Parent-package
// tests use it to stage a listing without a live fetch.
func NewFileObject(name string) Object { return Object{name: name} }

// sortSlice orders directories first, then files, each alphabetical
// (case-insensitive). It is the default order applied at fetch time;
// Model.SetObjects re-sorts by the user's active sort mode.
func sortSlice(items []Object) {
	sortObjects(items, sortByName, false)
}
