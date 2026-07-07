package bucketlist

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/storage/uri"
	"github.com/LinPr/lazys3/internal/tui/components/preview"
	tea "github.com/charmbracelet/bubbletea/v2"
)

// Bucket carries a single S3 bucket plus the connection hints (profile,
// endpoint, region, path-style) needed to issue follow-up calls against it
// from the metadata overlay. The hints are populated by listBuckets from the
// Option used to fetch the list, so Bucket stays self-contained for the
// overlay component without reaching back into TUI state.
type Bucket struct {
	name        string
	region      string
	profile     string
	endpointURL string
	pathStyle   bool
}

func (b Bucket) Name() string   { return b.name }
func (b Bucket) Region() string { return b.region }

func (b Bucket) Title() string       { return b.name }
func (b Bucket) Description() string { return b.region }
func (b Bucket) FilterValue() string { return b.name }

// GetPreviewRequest returns the parameters the metadata overlay needs to
// fetch live bucket metadata (HeadBucket/GetBucketLocation/GetBucketVersioning).
// The connection hints travel on the Bucket itself (populated by listBuckets
// from the active Option), so the overlay layer can build a fresh S3 client.
func (b Bucket) GetPreviewRequest() *preview.PreviewRequest {
	return &preview.PreviewRequest{
		Bucket:      b.name,
		Profile:     b.profile,
		EndpointURL: b.endpointURL,
		PathStyle:   b.pathStyle,
		Region:      b.region,
	}
}

// Option carries the configuration needed to build an S3 client for bucket
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

type FetchBucketListResultMsg struct {
	Buckets []Bucket
	Err     error
}

// fetchTimeout bounds a bucket-list fetch so a hung endpoint cannot leave
// the TUI waiting forever.
const fetchTimeout = 30 * time.Second

func FetchBucketListCmd(o *Option) tea.Cmd {
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
			return FetchBucketListResultMsg{Err: err}
		}

		if o.S3Uri == "" {
			buckets, err := listBuckets(ctx, cli, o)
			return FetchBucketListResultMsg{Buckets: buckets, Err: err}
		}

		parsedURI, err := uri.ParseS3Uri(o.S3Uri)
		if err != nil {
			return FetchBucketListResultMsg{Err: err}
		}
		if parsedURI.GetBucket() == "" {
			buckets, err := listBuckets(ctx, cli, o)
			return FetchBucketListResultMsg{Buckets: buckets, Err: err}
		}
		return nil
	}
}

// NewBucket constructs a bucket entry with the given name. Parent-package
// tests use it to stage a listing without a live fetch (mirroring
// objectlist.NewFileObject).
func NewBucket(name string) Bucket { return Bucket{name: name} }

// listBuckets lists the buckets visible to the given client and decorates
// each with the connection hints from the active Option, so the preview
// layer can issue follow-up calls (HeadBucket, GetBucketLocation, ...) on
// the same endpoint/profile/path-style without re-deriving them.
func listBuckets(ctx context.Context, cli *s3store.S3Store, o *Option) ([]Bucket, error) {
	buckets, err := cli.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	bucketList := make([]Bucket, 0, len(buckets))
	for _, bucket := range buckets {
		b := Bucket{
			name:        aws.ToString(bucket.Name),
			profile:     o.Profile,
			endpointURL: o.EndpointURL,
			pathStyle:   o.PathStyle,
			region:      o.Region,
		}
		bucketList = append(bucketList, b)
	}

	return bucketList, nil
}
