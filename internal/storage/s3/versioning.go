package s3store

import (
	"context"
	"net/url"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ObjectVersion is one entry in an object's version history: either a
// stored version or a delete marker (IsDeleteMarker).
type ObjectVersion struct {
	Key            string
	VersionID      string
	Size           int64
	LastModified   time.Time
	ETag           string
	StorageClass   string
	IsLatest       bool
	IsDeleteMarker bool
}

// PutBucketVersioning enables (Enabled) or suspends (Suspended) versioning
// on the bucket. Once a bucket has been versioned it can never return to
// the unversioned state, only to Suspended.
func (s3store *S3Store) PutBucketVersioning(ctx context.Context, bucket string, enabled bool) error {
	if s3store.dryRun {
		return nil
	}
	status := types.BucketVersioningStatusSuspended
	if enabled {
		status = types.BucketVersioningStatusEnabled
	}
	_, err := s3store.client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucket),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: status,
		},
	})
	return err
}

// GetBucketVersioning returns the bucket's versioning status: "Enabled",
// "Suspended", or "" for a bucket that has never been versioned.
func (s3store *S3Store) GetBucketVersioning(ctx context.Context, bucket string) (string, error) {
	out, err := s3store.client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return "", err
	}
	return string(out.Status), nil
}

// ListObjectVersions returns the full version history of exactly one key,
// newest first. The ListObjectVersions API only filters by prefix, so the
// results are narrowed to Key == key; versions and delete markers are
// merged into one chronological slice. Pagination follows the
// KeyMarker/VersionIdMarker cursor until the listing is no longer
// truncated.
func (s3store *S3Store) ListObjectVersions(ctx context.Context, bucket, key string) ([]ObjectVersion, error) {
	input := &s3.ListObjectVersionsInput{
		Bucket:       aws.String(bucket),
		Prefix:       aws.String(key),
		MaxKeys:      aws.Int32(1000),
		RequestPayer: s3store.requestPayer(),
	}
	var versions []ObjectVersion
	for {
		page, err := s3store.client.ListObjectVersions(ctx, input)
		if err != nil {
			return nil, err
		}
		for _, v := range page.Versions {
			if aws.ToString(v.Key) != key {
				continue
			}
			versions = append(versions, ObjectVersion{
				Key:          aws.ToString(v.Key),
				VersionID:    aws.ToString(v.VersionId),
				Size:         aws.ToInt64(v.Size),
				LastModified: aws.ToTime(v.LastModified),
				ETag:         aws.ToString(v.ETag),
				StorageClass: string(v.StorageClass),
				IsLatest:     aws.ToBool(v.IsLatest),
			})
		}
		for _, m := range page.DeleteMarkers {
			if aws.ToString(m.Key) != key {
				continue
			}
			versions = append(versions, ObjectVersion{
				Key:            aws.ToString(m.Key),
				VersionID:      aws.ToString(m.VersionId),
				LastModified:   aws.ToTime(m.LastModified),
				IsLatest:       aws.ToBool(m.IsLatest),
				IsDeleteMarker: true,
			})
		}
		if page.IsTruncated == nil || !*page.IsTruncated {
			break
		}
		input.KeyMarker = page.NextKeyMarker
		input.VersionIdMarker = page.NextVersionIdMarker
	}
	// Versions and DeleteMarkers arrive as two separate lists; interleave
	// them newest-first so the latest entry (version or marker) comes first.
	sort.SliceStable(versions, func(i, j int) bool {
		if versions[i].IsLatest != versions[j].IsLatest {
			return versions[i].IsLatest
		}
		return versions[i].LastModified.After(versions[j].LastModified)
	})
	return versions, nil
}

// GetObjectVersion is GetObject pinned to a specific version. The caller
// owns the returned Body and must close it.
func (s3store *S3Store) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (*s3.GetObjectOutput, error) {
	return s3store.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(key),
		VersionId:    aws.String(versionID),
		RequestPayer: s3store.requestPayer(),
	})
}

// DeleteObjectVersion permanently removes one specific version (or delete
// marker) of the key. Deleting the current delete marker "undeletes" the
// object: the previous version becomes latest again.
func (s3store *S3Store) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	if s3store.dryRun {
		return nil
	}
	_, err := s3store.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:    aws.String(bucket),
		Key:       aws.String(key),
		VersionId: aws.String(versionID),
	})
	return err
}

// RestoreObjectVersion makes an old version the newest one by server-side
// copying "bucket/key?versionId=..." onto the same key. Under Enabled
// versioning the original stays in the history and a new version with
// identical content is created on top; under Suspended (or never-versioned)
// status the copy is written as the "null" version, overwriting any existing
// null version of the key. storageClass, when non-empty, is applied to the
// copy — CopyObject does not inherit the source's storage class, so without
// it the restored version would silently land in the endpoint default
// (STANDARD). CopyObject caps at 5 GiB, and archived source versions must be
// thawed first; both limits surface as the server's error.
func (s3store *S3Store) RestoreObjectVersion(ctx context.Context, bucket, key, versionID, storageClass string) error {
	if s3store.dryRun {
		return nil
	}
	input := &s3.CopyObjectInput{
		Bucket:     aws.String(bucket),
		CopySource: aws.String(copySourcePath(bucket, key) + "?versionId=" + url.QueryEscape(versionID)),
		Key:        aws.String(key),
	}
	if storageClass != "" {
		input.StorageClass = types.StorageClass(storageClass)
	}
	_, err := s3store.client.CopyObject(ctx, input)
	return err
}
