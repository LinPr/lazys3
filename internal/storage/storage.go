package storage

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"time"

	fsstore "github.com/LinPr/lazys3/internal/storage/fs"
	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// // Storage is an interface for storage operations that is common
// // to local filesystem and remote object storage.
// type Storage interface {
// 	// Stat returns the Object structure describing object. If src is not
// 	// found, ErrGivenObjectNotFound is returned.
// 	Stat(ctx context.Context, target StorageURL) (*Object, error)

// 	// List the objects and directories/prefixes in the src.
// 	List(ctx context.Context, target StorageURL, followSymlinks bool) <-chan *Object

// 	// Delete deletes the given src.
// 	Delete(ctx context.Context, target StorageURL) error

// 	// MultiDelete deletes all items returned from given urls in batches.
// 	MultiDelete(ctx context.Context, urls <-chan StorageURL) <-chan *Object

// 	// Copy src to dst, optionally setting the given metadata. Src and dst
// 	// arguments are of the same type. If src is a remote type, server side
// 	// copying will be used.
// 	Copy(ctx context.Context, src, dst StorageURL, metadata Metadata) error
// }

type Metadata struct {
	ACL                string
	CacheControl       string
	Expires            string
	StorageClass       string
	ContentType        string
	ContentEncoding    string
	ContentDisposition string
	EncryptionMethod   string
	EncryptionKeyID    string

	UserDefined map[string]string

	// MetadataDirective is used to specify whether the metadata is copied from
	// the source object or replaced with metadata provided when copying S3
	// objects. If MetadataDirective is not set, it defaults to "COPY".
	Directive string
}

// ObjectType is the type of Object.
type ObjectType struct {
	mode os.FileMode
}

// String returns the string representation of ObjectType.
func (o ObjectType) String() string {
	switch mode := o.mode; {
	case mode.IsRegular():
		return "file"
	case mode.IsDir():
		return "directory"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	}
	return ""
}

// MarshalJSON returns the stringer of ObjectType as a marshalled json.
func (o ObjectType) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

// IsDir checks if the object is a directory.
func (o ObjectType) IsDir() bool {
	return o.mode.IsDir()
}

// IsSymlink checks if the object is a symbolic link.
func (o ObjectType) IsSymlink() bool {
	return o.mode&os.ModeSymlink != 0
}

// IsRegular checks if the object is a regular file.
func (o ObjectType) IsRegular() bool {
	return o.mode.IsRegular()
}

// StorageClass represents the storage used to store an object.
type StorageClass string

// Object is a generic type which contains metadata for storage items.
type Object struct {
	StorageURL   *StorageURL  `json:"key,omitempty"`
	Etag         string       `json:"etag,omitempty"`
	ModTime      *time.Time   `json:"last_modified,omitempty"`
	Type         ObjectType   `json:"type,omitempty"`
	Size         int64        `json:"size,omitempty"`
	StorageClass StorageClass `json:"storage_class,omitempty"`
	Err          error        `json:"error,omitempty"`

	// the VersionID field exist only for JSON Marshall, it must not be used for
	// any other purpose. URL.VersionID must be used instead.
	VersionID string `json:"version_id,omitempty"`
}

type Storage struct {
	remote *s3store.S3Store
	local  *fsstore.FileStore
}

func NewStorage(ctx context.Context, option StorageOption) (*Storage, error) {
	s3client, err := s3store.NewS3Client(ctx, option.s3Option)
	if err != nil {
		return nil, err
	}
	fs := fsstore.NewFileStore(ctx, option.localOption)

	return &Storage{
		remote: s3client,
		local:  fs,
	}, nil
}

// PresignGetObject returns a presigned HTTP GET URL for the object, valid
// for the given expiry (zero means 1h, max 7 days).
func (s *Storage) PresignGetObject(ctx context.Context, bucketName string, objectKey string, expiry time.Duration) (string, error) {
	return s.remote.PresignGetObject(ctx, bucketName, objectKey, expiry)
}

// PresignPutObject returns a presigned HTTP PUT URL for uploading the
// object, valid for the given expiry (zero means 1h, max 7 days).
func (s *Storage) PresignPutObject(ctx context.Context, bucketName string, objectKey string, expiry time.Duration) (string, error) {
	return s.remote.PresignPutObject(ctx, bucketName, objectKey, expiry)
}

func (s *Storage) DownloadFile(ctx context.Context, bucketName string, objectKey string, localFile string) error {
	return s.DownloadFileWithProgress(ctx, bucketName, objectKey, localFile, nil)
}

// DownloadFileWithProgress is DownloadFile with a byte-level progress
// callback. progress, when non-nil, is invoked (throttled) as body bytes
// are written locally, with the object's ContentLength as totalBytes
// (-1 when the server did not report it), and once more at completion.
func (s *Storage) DownloadFileWithProgress(ctx context.Context, bucketName string, objectKey string, localFile string, progress ProgressFunc) error {

	result, err := s.remote.GetObject(ctx, bucketName, objectKey)
	if err != nil {
		return err
	}
	defer result.Body.Close() //nolint:errcheck // best-effort cleanup of the GetObject body

	total := int64(-1)
	if result.ContentLength != nil {
		total = *result.ContentLength
	}
	tracker := newProgressTracker(total, progress)

	if localFile == "" || localFile == "-" {
		if _, err = io.Copy(&progressWriter{w: os.Stdout, t: tracker}, result.Body); err != nil {
			return err
		}
		tracker.finish()
		return nil
	}

	// A directory as the destination means "download into it": append the
	// object's base name, matching cp/aws-cli semantics. Without this the
	// temp-file rename below would fail against the existing directory.
	if fi, statErr := os.Stat(localFile); statErr == nil && fi.IsDir() {
		localFile = filepath.Join(localFile, path.Base(objectKey))
	}

	// Stream into a temp file next to localFile and rename on success, so
	// a failed download never truncates an existing local copy or leaves
	// a partial file at the final path.
	file, err := os.CreateTemp(filepath.Dir(localFile), filepath.Base(localFile)+".tmp-*")
	if err != nil {
		log.Printf("Couldn't create file %v. err: %v\n", localFile, err)
		return err
	}
	tmp := file.Name()
	if _, err = io.Copy(&progressWriter{w: file, t: tracker}, result.Body); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	// Close errors matter on the write path (buffered data may fail to
	// flush), so surface them instead of deferring.
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// CreateTemp files are 0600; restore os.Create's default.
	if err := os.Chmod(tmp, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, localFile); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	tracker.finish()
	return nil
}

// stdin is an io.Reader adapter for os.File, enabling it to function solely as
// an io.Reader. The AWS SDK, which accepts an io.Reader for multipart uploads,
// will attempt to use io.Seek if the reader supports it. However, os.Stdin is
// a specific type of file that can not seekable.
type stdin struct {
	file *os.File
}

func (s *stdin) Read(p []byte) (n int, err error) {
	return s.file.Read(p)
}

// func (s *stdin) Seek(offset int64, whence int) (int64, error) {
// 	return s.file.Seek(offset, whence)
// }

// UploadFile reads the local file at fileName and uploads it to the bucket
// using PutObject. It opens the source with os.Open (read-only) so the
// local file is not modified or truncated.
//
// For multipart uploads of large files, callers should use
// UploadFromStdin or the S3Store.Uploader directly.
func (s *Storage) UploadFile(ctx context.Context, fileName string, bucketName string, objectKey string) (*s3.PutObjectOutput, error) {
	return s.UploadFileWithProgress(ctx, fileName, bucketName, objectKey, nil)
}

// UploadFileWithProgress is UploadFile with a byte-level progress
// callback. progress, when non-nil, is invoked (throttled) as file bytes
// are read by the SDK, with the file size as totalBytes, and once more at
// completion. The reader stays seekable so the SDK can compute the
// content length (and rewind for payload hashing/retries — a rewind
// resets the count so bytes are not double-counted).
func (s *Storage) UploadFileWithProgress(ctx context.Context, fileName string, bucketName string, objectKey string, progress ProgressFunc) (*s3.PutObjectOutput, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck // best-effort cleanup of the uploaded file handle

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	tracker := newProgressTracker(info.Size(), progress)

	out, err := s.remote.PutObject(ctx, &progressReadSeeker{rs: file, t: tracker}, bucketName, objectKey)
	if err == nil {
		tracker.finish()
	}
	return out, err
}

// UploadFileMultipart uploads fileName through the s3 manager uploader,
// which switches to a multipart upload when the body exceeds one part.
// partSize <= 0 keeps the manager default (5 MiB). progress, when
// non-nil, receives throttled byte progress with the file size as
// totalBytes, and a final call at completion.
func (s *Storage) UploadFileMultipart(ctx context.Context, fileName string, bucketName string, objectKey string, partSize int64, progress ProgressFunc) (*manager.UploadOutput, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck // best-effort cleanup of the uploaded file handle

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	tracker := newProgressTracker(info.Size(), progress)

	// A plain (non-seekable) reader forces the manager into sequential
	// part reads, so every byte flows through the wrapper exactly once.
	out, err := s.remote.UploadObjectWithPartSize(ctx, &progressReader{r: file, t: tracker}, bucketName, objectKey, partSize)
	if err == nil {
		tracker.finish()
	}
	return out, err
}

func (s *Storage) UploadFromStdin(ctx context.Context, bucketName string, objectKey string) (*manager.UploadOutput, error) {
	return s.UploadFromStdinWithProgress(ctx, bucketName, objectKey, nil)
}

// UploadFromStdinWithProgress is UploadFromStdin with a byte-level
// progress callback. The total size of a stdin stream is unknown, so
// totalBytes is -1.
func (s *Storage) UploadFromStdinWithProgress(ctx context.Context, bucketName string, objectKey string, progress ProgressFunc) (*manager.UploadOutput, error) {
	tracker := newProgressTracker(-1, progress)
	stdinReader := &progressReader{r: &stdin{file: os.Stdin}, t: tracker}
	out, err := s.remote.UploadObject(ctx, stdinReader, bucketName, objectKey)
	if err == nil {
		tracker.finish()
	}
	return out, err
}
