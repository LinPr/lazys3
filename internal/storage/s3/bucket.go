package s3store

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// ListBuckets lists the buckets in the current account.
func (s3store *S3Store) ListBuckets(ctx context.Context) ([]types.Bucket, error) {
	var err error
	var output *s3.ListBucketsOutput
	var buckets []types.Bucket
	bucketPaginator := s3.NewListBucketsPaginator(s3store.client, &s3.ListBucketsInput{})
	for bucketPaginator.HasMorePages() {
		output, err = bucketPaginator.NextPage(ctx)
		if err != nil {
			var apiErr smithy.APIError
			if errors.As(err, &apiErr) && apiErr.ErrorCode() == "AccessDenied" {
				log.Println("You don't have permission to list buckets for this account.")
				err = apiErr
			} else {
				log.Printf("Couldn't list buckets for your account. Here's why: %v\n", err)
			}
			break
		} else {
			buckets = append(buckets, output.Buckets...)
		}
	}
	return buckets, err
}

func (s3store *S3Store) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	_, err := s3store.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	exists := true
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) {
			switch apiError.(type) {
			case *types.NotFound:
				log.Printf("Create Bucket %v is available.\n", bucketName)
				exists = false
				err = nil
			default:
				log.Printf("Either you don't have access to bucket %v or another error occurred. "+
					"Here's what happened: %v\n", bucketName, err)
			}
		}
	} else {
		log.Printf("Bucket %v exists and you already own it.", bucketName)
	}

	return exists, err
}

func (s3store *S3Store) CreateBucket(ctx context.Context, name string, region string) error {
	if s3store.dryRun {
		return nil
	}
	input := &s3.CreateBucketInput{
		Bucket: aws.String(name),
	}
	// Aliyun OSS (and several other S3-compatible services) reject a
	// CreateBucketConfiguration whose LocationConstraint does not match
	// the endpoint's region. Only attach the constraint when the caller
	// actually supplied a region; passing "" sends an empty constraint
	// which OSS also rejects. Empty region => omit the field entirely.
	if region != "" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}
	_, err := s3store.client.CreateBucket(ctx, input)
	if err != nil {
		var owned *types.BucketAlreadyOwnedByYou
		var exists *types.BucketAlreadyExists
		if errors.As(err, &owned) {
			log.Printf("You already own bucket %s.\n", name)
			err = owned
		} else if errors.As(err, &exists) {
			log.Printf("Bucket %s already exists in region %s.\n", name, region)
			err = exists
		}
		return err
	}
	log.Println("Waiting for bucket to be created...")
	waiter := s3.NewBucketExistsWaiter(s3store.client)
	return waiter.Wait(ctx, &s3.HeadBucketInput{Bucket: aws.String(name)}, time.Minute)
}

// DeleteBucket deletes a bucket. The bucket must be empty or an error is returned.
func (s3store *S3Store) DeleteBucket(ctx context.Context, bucketName string) error {
	if s3store.dryRun {
		return nil
	}
	_, err := s3store.client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName)})
	if err != nil {
		var noBucket *types.NoSuchBucket
		if errors.As(err, &noBucket) {
			log.Printf("Bucket %s does not exist.\n", bucketName)
			err = noBucket
		} else {
			log.Printf("Couldn't delete bucket %v. Here's why: %v\n", bucketName, err)
		}
	} else {
		err = s3.NewBucketNotExistsWaiter(s3store.client).Wait(
			ctx, &s3.HeadBucketInput{Bucket: aws.String(bucketName)}, time.Minute)
		if err != nil {
			log.Printf("Failed attempt to wait for bucket %s to be deleted.\n", bucketName)
		} else {
			log.Printf("Deleted %s.\n", bucketName)
		}
	}
	return err
}

func (s3store *S3Store) HeadBucket(ctx context.Context, bucketName string) (*s3.HeadBucketOutput, error) {
	return s3store.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
}
