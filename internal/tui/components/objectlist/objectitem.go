package objectlist

import (
	"context"
	"fmt"
	"strings"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/storage/uri"
)

type Object struct {
	name string
}

type Option struct {
	S3Uri       string
	Profile     string
	Region      string
	PathStyle   bool
	EndpointUrl string
}

func (b Object) Title() string       { return b.name }
func (b Object) Description() string { return "null" }
func (b Object) FilterValue() string { return b.name }

func (i Object) GetPreviewContent() string {
	// 通过网络获取到数据
	var content strings.Builder
	content.WriteString("• Versioning: Enabled\n")
	content.WriteString("• Encryption: AES-256\n")
	content.WriteString("• Public Access: Blocked\n")

	return content.String()
}

func FetchObjectList(o Option) ([]Object, error) {
	opt := s3store.S3Option{
		UsePathStyle: o.PathStyle,
		Region:       o.Region,
		Profile:      o.Profile,
		// Endpoint:     o.EndpointUrl,
		// NoVerifySSL: o.NoVerifySSL,
		// DryRun:      o.DryRun,
	}

	cli, err := s3store.NewS3Client(context.TODO(), opt)
	if err != nil {
		return nil, err
	}

	if o.S3Uri == "" {
		// return listBuckets(cli)
		return nil, fmt.Errorf("S3Uri is empty")
	}

	parsedUri, err := uri.ParseS3Uri(o.S3Uri)
	if err != nil {
		return nil, err
	}
	if parsedUri.GetBucket() == "" {
		// return listBuckets(cli)
		return nil, fmt.Errorf("S3Uri is empty")
	}
	return listObjects(cli, parsedUri.GetBucket(), parsedUri.GetKey())
}

func listObjects(cli *s3store.S3Store, bucket, key string) ([]Object, error) {
	prefixes, objs, err := cli.ListObjectsWithPagination(context.TODO(), bucket, key)
	if err != nil {
		return nil, err
	}

	objectList := make([]Object, 0, len(prefixes)+len(objs))
	for _, prefix := range prefixes {
		// log.Println("-------------prefix:", *prefix.Prefix)
		objectList = append(objectList, Object{name: *prefix.Prefix})
	}
	for _, obj := range objs {
		// log.Println("-------------object:", *obj.Key)
		objectList = append(objectList, Object{name: *obj.Key})
	}
	return objectList, nil
}
