package bucketlist

import (
	"context"
	"fmt"
	"strings"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/storage/uri"
)

type Bucket struct {
	name   string
	region string
}

func (b Bucket) Title() string       { return b.name }
func (b Bucket) Description() string { return b.region }
func (b Bucket) FilterValue() string { return b.name }

func (i Bucket) GetPreviewContent() string {
	// 通过网络获取到数据
	var content strings.Builder
	content.WriteString(fmt.Sprintf("Name: %s\n", i.name))
	content.WriteString(fmt.Sprintf("Description: %s\n", "A mock bucket for demonstration"))
	content.WriteString(fmt.Sprintf("Size: %s\n", "12.3 GB"))
	content.WriteString(fmt.Sprintf("Objects: %s\n", "1,234"))
	content.WriteString(fmt.Sprintf("Region: %s\n", i.region))
	content.WriteString(fmt.Sprintf("Storage Class: %s\n", "STANDARD"))
	content.WriteString(fmt.Sprintf("Created: %s\n", "2023-01-01T12:00:00Z"))
	content.WriteString(fmt.Sprintf("Modified: %s\n", "2024-06-01T08:30:00Z"))
	content.WriteString("\n--- Storage Details ---\n")
	content.WriteString("• Versioning: Enabled\n")
	content.WriteString("• Encryption: AES-256\n")
	content.WriteString("• Public Access: Blocked\n")

	return content.String()
}

type Option struct {
	S3Uri       string
	Profile     string
	Region      string
	PathStyle   bool
	EndpointUrl string
}

func FetchBucketList(o Option) ([]Bucket, error) {

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
		return listBuckets(cli)
	}

	parsedUri, err := uri.ParseS3Uri(o.S3Uri)
	if err != nil {
		return nil, err
	}
	if parsedUri.GetBucket() == "" {
		return listBuckets(cli)
	}

	return nil, nil
	// return listObjects(cli, parsedUri.GetBucket(), parsedUri.GetKey())
}

func listBuckets(cli *s3store.S3Store) ([]Bucket, error) {
	buckets, err := cli.ListBuckets(context.TODO())
	if err != nil {
		return nil, err
	}
	bucketList := make([]Bucket, 0, len(buckets))
	for _, bucket := range buckets {
		// fmt.Printf("%s\t%s\n", bucket.CreationDate.Format(time.DateTime), *bucket.Name)
		bucketList = append(bucketList, Bucket{
			name: *bucket.Name,
		})
	}

	return bucketList, nil
}
