package s3store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

func (s3store *S3Store) UploadFile(ctx context.Context, bucketName string, objectKey string, fileName string) error {
	if s3store.dryRun {
		return nil
	}
	file, err := os.Open(fileName)
	if err != nil {
		log.Printf("Couldn't open file %v to upload. Here's why: %v\n", fileName, err)
	} else {
		defer file.Close() //nolint:errcheck // best-effort cleanup of the uploaded file handle
		_, err = s3store.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(objectKey),
			Body:   file,
		})
		if err != nil {
			var apiErr smithy.APIError
			if errors.As(err, &apiErr) && apiErr.ErrorCode() == "EntityTooLarge" {
				log.Printf("Error while uploading object to %s. The object is too large.\n"+
					"To upload objects larger than 5GB, use the S3 console (160GB max)\n"+
					"or the multipart upload API (5TB max).", bucketName)
			} else {
				log.Printf("Couldn't upload file %v to %v:%v. Here's why: %v\n",
					fileName, bucketName, objectKey, err)
			}
		} else {
			err = s3.NewObjectExistsWaiter(s3store.client).Wait(
				ctx, &s3.HeadObjectInput{Bucket: aws.String(bucketName), Key: aws.String(objectKey)}, time.Minute)
			if err != nil {
				log.Printf("Failed attempt to wait for object %s to exist.\n", objectKey)
			}
		}
	}
	return err
}

func (s3store *S3Store) UploadLargeObject(ctx context.Context, bucketName string, objectKey string, largeObject []byte) error {
	if s3store.dryRun {
		return nil
	}
	largeBuffer := bytes.NewReader(largeObject)
	var partMiBs int64 = 10
	uploader := manager.NewUploader(s3store.client, func(u *manager.Uploader) {
		u.PartSize = partMiBs * 1024 * 1024
	})
	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
		Body:   largeBuffer,
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "EntityTooLarge" {
			log.Printf("Error while uploading object to %s. The object is too large.\n"+
				"The maximum size for a multipart upload is 5TB.", bucketName)
		} else {
			log.Printf("Couldn't upload large object to %v:%v. Here's why: %v\n",
				bucketName, objectKey, err)
		}
	} else {
		err = s3.NewObjectExistsWaiter(s3store.client).Wait(
			ctx, &s3.HeadObjectInput{Bucket: aws.String(bucketName), Key: aws.String(objectKey)}, time.Minute)
		if err != nil {
			log.Printf("Failed attempt to wait for object %s to exist.\n", objectKey)
		}
	}

	return err
}

func (s3store *S3Store) DownloadFile(ctx context.Context, bucketName string, objectKey string, fileName string) error {
	result, err := s3store.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		var noKey *types.NoSuchKey
		if errors.As(err, &noKey) {
			log.Printf("Can't get object %s from bucket %s. No such key exists.\n", objectKey, bucketName)
			err = noKey
		} else {
			log.Printf("Couldn't get object %v:%v. Here's why: %v\n", bucketName, objectKey, err)
		}
		return err
	}
	defer result.Body.Close() //nolint:errcheck // best-effort cleanup of the GetObject body
	file, err := os.Create(fileName)
	if err != nil {
		log.Printf("Couldn't create file %v. Here's why: %v\n", fileName, err)
		return err
	}
	defer file.Close() //nolint:errcheck // best-effort cleanup of the local destination file
	body, err := io.ReadAll(result.Body)
	if err != nil {
		log.Printf("Couldn't read object body from %v. Here's why: %v\n", objectKey, err)
	}
	_, err = file.Write(body)
	return err
}

func (s3store *S3Store) DownloadLargeObject(ctx context.Context, bucketName string, objectKey string) ([]byte, error) {
	var partMiBs int64 = 10
	downloader := manager.NewDownloader(s3store.client, func(d *manager.Downloader) {
		d.PartSize = partMiBs * 1024 * 1024
	})
	buffer := manager.NewWriteAtBuffer([]byte{})
	_, err := downloader.Download(ctx, buffer, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		log.Printf("Couldn't download large object from %v:%v. Here's why: %v\n",
			bucketName, objectKey, err)
	}
	return buffer.Bytes(), err
}

// CopyToFolder copies an object in a bucket to a subfolder in the same bucket.
func (s3store *S3Store) CopyToFolder(ctx context.Context, bucketName string, objectKey string, folderName string) error {
	if s3store.dryRun {
		return nil
	}
	objectDest := fmt.Sprintf("%v/%v", folderName, objectKey)
	_, err := s3store.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(bucketName),
		CopySource: aws.String(copySourcePath(bucketName, objectKey)),
		Key:        aws.String(objectDest),
	})
	if err != nil {
		var notActive *types.ObjectNotInActiveTierError
		if errors.As(err, &notActive) {
			log.Printf("Couldn't copy object %s from %s because the object isn't in the active tier.\n",
				objectKey, bucketName)
			err = notActive
		}
	} else {
		err = s3.NewObjectExistsWaiter(s3store.client).Wait(
			ctx, &s3.HeadObjectInput{Bucket: aws.String(bucketName), Key: aws.String(objectDest)}, time.Minute)
		if err != nil {
			log.Printf("Failed attempt to wait for object %s to exist.\n", objectDest)
		}
	}
	return err
}

// CopyToBucket copies an object in a bucket to another bucket.
func (s3store *S3Store) CopyToBucket(ctx context.Context, sourceBucket string, destinationBucket string, objectKey string) error {
	if s3store.dryRun {
		return nil
	}
	_, err := s3store.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(destinationBucket),
		CopySource: aws.String(copySourcePath(sourceBucket, objectKey)),
		Key:        aws.String(objectKey),
	})
	if err != nil {
		var notActive *types.ObjectNotInActiveTierError
		if errors.As(err, &notActive) {
			log.Printf("Couldn't copy object %s from %s because the object isn't in the active tier.\n",
				objectKey, sourceBucket)
			err = notActive
		}
	} else {
		err = s3.NewObjectExistsWaiter(s3store.client).Wait(
			ctx, &s3.HeadObjectInput{Bucket: aws.String(destinationBucket), Key: aws.String(objectKey)}, time.Minute)
		if err != nil {
			log.Printf("Failed attempt to wait for object %s to exist.\n", objectKey)
		}
	}
	return err
}

// DeleteObjects deletes a list of objects from a bucket.
//
// OSS's bulk-delete endpoint rejects the request with
// "MissingArgument: Missing Some Required Arguments" when the
// x-amz-md5-5 header (Content-MD5) is absent — AWS S3 made that header
// optional, but OSS still requires it. The v2 SDK computes and sends
// Content-MD5 automatically when the input is serialized through the
// middleware, but only when the bucket's API style expects it; on OSS
// we have to ensure the header is set explicitly. The simplest portable
// fix is to fall back to single-key DeleteObject calls when the bulk
// request fails with MissingArgument: this keeps AWS S3 on the fast
// path and routes OSS through the per-object path which has no such
// requirement.
func (s3store *S3Store) DeleteObjects(ctx context.Context, bucketName string, objectKeys []string) error {
	if s3store.dryRun {
		return nil
	}
	var objectIds []types.ObjectIdentifier
	for _, key := range objectKeys {
		objectIds = append(objectIds, types.ObjectIdentifier{Key: aws.String(key)})
	}
	if len(objectIds) == 0 {
		return nil
	}
	output, err := s3store.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucketName),
		Delete: &types.Delete{Objects: objectIds},
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "MissingArgument" {
			// OSS rejects bulk Delete without a Content-MD5 header. Fall
			// back to per-object DeleteObject calls, which OSS accepts.
			return s3store.deleteObjectsIndividually(ctx, bucketName, objectKeys)
		}
		var noBucket *types.NoSuchBucket
		if errors.As(err, &noBucket) {
			log.Printf("Bucket %s does not exist.\n", bucketName)
			return noBucket
		}
		log.Printf("Error deleting objects from bucket %s: %v\n", bucketName, err)
		return err
	}
	if len(output.Errors) > 0 {
		for _, outErr := range output.Errors {
			log.Printf("%s: %s\n", *outErr.Key, *outErr.Message)
		}
		return fmt.Errorf("%s", *output.Errors[0].Message)
	}
	// S3 delete is strongly consistent; no post-delete waiter is needed.
	log.Printf("Deleted %d objects from bucket %s.\n", len(output.Deleted), bucketName)
	return nil
}

// deleteObjectsIndividually deletes each key with a separate DeleteObject
// call. Used as a fallback when the bulk DeleteObjects API rejects the
// request (e.g. OSS's MissingArgument on missing Content-MD5).
func (s3store *S3Store) deleteObjectsIndividually(ctx context.Context, bucketName string, objectKeys []string) error {
	for _, key := range objectKeys {
		_, err := s3store.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(key),
		})
		if err != nil {
			var noBucket *types.NoSuchBucket
			if errors.As(err, &noBucket) {
				log.Printf("Bucket %s does not exist.\n", bucketName)
				return noBucket
			}
			return fmt.Errorf("delete %q: %w", key, err)
		}
		log.Printf("Deleted %s.\n", key)
	}
	return nil
}

func (s3store *S3Store) ListObjectsWithPagination(ctx context.Context, bucket, key string) ([]types.CommonPrefix, []types.Object, error) {
	if s3store.useListObjectsV1 {
		prefixes, objects, err := s3store.listObjectsV1(ctx, bucket, key, "/")
		if err != nil {
			return nil, nil, err
		}
		log.Printf("Total objects found: %d\n", len(objects))
		return prefixes, objects, nil
	}
	paginator := s3.NewListObjectsV2Paginator(s3store.client, &s3.ListObjectsV2Input{
		Bucket:       aws.String(bucket),
		Prefix:       aws.String(key),
		MaxKeys:      aws.Int32(1000), // The maximum number of keys that can be returned in a single request is 1000.
		Delimiter:    aws.String("/"), // Setting a delimiter causes keys that contain the same string between the prefix and the first occurrence of the delimiter to be rolled up into a single result element in CommonPrefixes.
		RequestPayer: s3store.requestPayer(),
	})
	var objects []types.Object
	var prefixes []types.CommonPrefix
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, nil, err
		}

		// 前缀
		prefixes = append(prefixes, page.CommonPrefixes...)
		objects = append(objects, page.Contents...)
	}
	log.Printf("Total objects found: %d\n", len(objects))
	return prefixes, objects, nil
}

// listObjectsV1 paginates the legacy ListObjects (V1) API by hand — the v2
// SDK provides no paginator for it. It is used when S3Option.UseListObjectsV1
// is set (some S3-compatible services do not implement ListObjectsV2). An
// empty delimiter yields a recursive (flat) listing.
func (s3store *S3Store) listObjectsV1(ctx context.Context, bucket, prefix, delimiter string) ([]types.CommonPrefix, []types.Object, error) {
	input := &s3.ListObjectsInput{
		Bucket:       aws.String(bucket),
		Prefix:       aws.String(prefix),
		MaxKeys:      aws.Int32(1000),
		RequestPayer: s3store.requestPayer(),
	}
	if delimiter != "" {
		input.Delimiter = aws.String(delimiter)
	}
	var objects []types.Object
	var prefixes []types.CommonPrefix
	for {
		page, err := s3store.client.ListObjects(ctx, input)
		if err != nil {
			return nil, nil, err
		}
		prefixes = append(prefixes, page.CommonPrefixes...)
		objects = append(objects, page.Contents...)
		if page.IsTruncated == nil || !*page.IsTruncated {
			return prefixes, objects, nil
		}
		// NextMarker is only returned when a delimiter is set; otherwise
		// the last returned key is the next marker.
		switch {
		case page.NextMarker != nil && *page.NextMarker != "":
			input.Marker = page.NextMarker
		case len(page.Contents) > 0:
			input.Marker = page.Contents[len(page.Contents)-1].Key
		default:
			return prefixes, objects, nil
		}
	}
}

func (s3store *S3Store) GetObject(ctx context.Context, bucketName string, objectKey string) (*s3.GetObjectOutput, error) {
	output, err := s3store.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})

	if err != nil {
		var noKey *types.NoSuchKey
		if errors.As(err, &noKey) {
			log.Printf("Can't get object %s from bucket %s. No such key exists.\n", objectKey, bucketName)
			err = noKey
		} else {
			log.Printf("Couldn't get object %v:%v. Here's why: %v\n", bucketName, objectKey, err)
		}
		return nil, err
	}
	return output, nil
}

func (s3store *S3Store) PutObject(ctx context.Context, r io.Reader, bucketName string, objectKey string) (*s3.PutObjectOutput, error) {
	if s3store.dryRun {
		return &s3.PutObjectOutput{}, nil
	}
	output, err := s3store.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
		Body:   r,
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "EntityTooLarge" {
			log.Printf("Error while uploading object to %s. The object is too large.\n"+
				"To upload objects larger than 5GB, use the S3 console (160GB max)\n"+
				"or the multipart upload API (5TB max).", bucketName)
		} else {
			log.Printf("Couldn't upload to %v:%v. Here's why: %v\n",
				bucketName, objectKey, err)
		}
		return nil, err
	}

	// The upload already succeeded; a waiter failure (e.g. HeadObject denied
	// by a write-only policy, or a transient error) must not fail the call.
	if waitErr := s3.NewObjectExistsWaiter(s3store.client).Wait(
		ctx, &s3.HeadObjectInput{Bucket: aws.String(bucketName), Key: aws.String(objectKey)}, time.Minute); waitErr != nil {
		log.Printf("Failed attempt to wait for object %s to exist: %v\n", objectKey, waitErr)
	}

	return output, nil
}

func (s3store *S3Store) UploadObject(ctx context.Context, reader io.Reader, bucketName string, key string) (*manager.UploadOutput, error) {
	if s3store.dryRun {
		return &manager.UploadOutput{}, nil
	}
	return s3store.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
		Body:   reader,
	})

}

// UploadObjectWithPartSize is UploadObject with an explicit multipart
// part size. partSize <= 0 keeps the manager default (5 MiB).
func (s3store *S3Store) UploadObjectWithPartSize(ctx context.Context, reader io.Reader, bucketName string, key string, partSize int64) (*manager.UploadOutput, error) {
	if s3store.dryRun {
		return &manager.UploadOutput{}, nil
	}
	return s3store.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
		Body:   reader,
	}, func(u *manager.Uploader) {
		if partSize > 0 {
			u.PartSize = partSize
		}
	})
}

func (s3store *S3Store) HeadObject(ctx context.Context, bucketName string, objectKey string) (*s3.HeadObjectOutput, error) {
	return s3store.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
}
