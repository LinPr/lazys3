package s3store

import (
	"context"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// copySourcePath returns "<bucket>/<key>" with each path segment
// percent-encoded, as the x-amz-copy-source header requires the caller to
// URL-encode the value (the SDK sends it verbatim). QueryEscape is used per
// segment so '+', '%', '?', '#' and non-ASCII characters survive the
// server-side decode.
func copySourcePath(bucket, key string) string {
	elements := strings.Split(bucket+"/"+key, "/")
	for i, element := range elements {
		elements[i] = url.QueryEscape(element)
	}
	return strings.Join(elements, "/")
}

// ListObjectsRecursive lists every object under prefix in a flat
// (non-delimited) enumeration. It is the recursive counterpart of
// ListObjectsWithPagination: the delimiter is cleared so S3 returns every
// nested key in Contents (no CommonPrefixes grouping).
//
// Track C sync uses this to build a source/destination inventory for the
// merge-compare: each key's relative path is the full key with the listing
// prefix trimmed, so the same key on src and dst produces the same relative
// path and the compare can pair them.
//
// The returned objects are sorted by key on the S3 side; callers should
// re-sort by relative path once they have trimmed the prefix.
func (s3store *S3Store) ListObjectsRecursive(ctx context.Context, bucket, prefix string) ([]types.Object, error) {
	if s3store.useListObjectsV1 {
		_, objects, err := s3store.listObjectsV1(ctx, bucket, prefix, "")
		return objects, err
	}
	input := &s3.ListObjectsV2Input{
		Bucket:       aws.String(bucket),
		Prefix:       aws.String(prefix),
		MaxKeys:      aws.Int32(1000),
		RequestPayer: s3store.requestPayer(),
		// Delimiter intentionally unset so the listing is recursive.
	}
	paginator := s3.NewListObjectsV2Paginator(s3store.client, input)
	var objects []types.Object
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		objects = append(objects, page.Contents...)
	}
	return objects, nil
}

// CopyObject performs a server-side CopyObject from srcBucket/srcKey to
// dstBucket/dstKey. It is the arbitrary-source/arbitrary-destination
// counterpart of CopyToBucket / CopyToFolder (which only cover same-key or
// same-bucket cases). Track C sync uses it for s3→s3 transfers.
//
// The CopySource is "<srcBucket>/<srcKey>" (URL-encoded) as required by the
// S3 API; for versioned copies the caller would need to append
// "?versionId=...", which lazys3 sync does not currently support.
func (s3store *S3Store) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	if s3store.dryRun {
		return nil
	}
	_, err := s3store.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		CopySource: aws.String(copySourcePath(srcBucket, srcKey)),
		Key:        aws.String(dstKey),
	})
	return err
}
