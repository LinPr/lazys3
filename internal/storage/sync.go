package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/LinPr/lazys3/internal/parallel"
	"github.com/LinPr/lazys3/internal/strutil"
)

// SyncOptions controls the behaviour of (*Storage).Sync. All fields are
// optional; a zero-value SyncOptions performs a default (size-and-mtime)
// sync with no delete, no dry-run, no exclude/include filters, and a
// small fixed concurrency.
type SyncOptions struct {
	// Delete removes destination objects that do not exist in the source.
	Delete bool
	// SizeOnly makes the comparison use only object sizes; modification
	// times are ignored. Useful when mtime is unreliable (e.g. when the
	// source is S3 and the destination is a local FS that does not
	// preserve S3 mtime exactly).
	SizeOnly bool
	// DryRun skips actual transfers/deletes and just counts what would
	// happen. The SyncResult returned reflects what would have been done.
	DryRun bool
	// Exclude is a list of glob patterns; any relative path matching one
	// of these is skipped on both source and destination.
	Exclude []string
	// Include is a list of glob patterns; a path must match at least one
	// to be considered. When empty, no include filter is applied.
	Include []string
	// Concurrency caps the number of in-flight transfer/delete tasks.
	// Defaults to 4 when non-positive. Values below 2 are raised to 2.
	Concurrency int
	// PartSize is the multipart part size used for large uploads, in
	// bytes. When non-positive the s3manager default (5 MiB) is used.
	PartSize int64
}

// SyncResult summarises a Sync run. Counts are best-effort: each
// submitted task increments exactly one of Uploaded/Downloaded/Copied/
// Deleted, and Skipped counts files that were left untouched because
// the strategy said so or because an exclude/include filter dropped
// them. Errors is the list of per-task failures; the corresponding
// task still counted in its bucket.
type SyncResult struct {
	Uploaded   int
	Downloaded int
	Copied     int
	Deleted    int
	Skipped    int
	Errors     []error
}

// SyncProgressFunc receives per-file progress events during Sync. It is
// an alias so existing func literals keep compiling. transferredBytes /
// totalBytes are byte counts for that file; see (*Storage).Sync for the
// per-direction semantics. transferredBytes == totalBytes (with
// totalBytes >= 0) is reported exactly once per file, after that file's
// transfer succeeded — mid-stream reports never reach totalBytes, so
// consumers may treat it as the file's completion event.
type SyncProgressFunc = func(file string, transferredBytes, totalBytes int64)

// syncTracker adapts the per-file Sync progress callback into a byte
// tracker bound to rel. It returns nil (a valid no-op tracker) when
// progress is nil.
func syncTracker(rel string, size int64, progress SyncProgressFunc) *progressTracker {
	if progress == nil {
		return nil
	}
	return newProgressTracker(size, func(transferred, total int64) {
		progress(rel, transferred, total)
	})
}

// syncObject is a lightweight flattened representation of either a local
// file or a remote S3 key, suitable for the merge-compare step. It holds
// just enough metadata (size, mtime, location) for the strategy to
// decide whether to copy.
type syncObject struct {
	// rel is the relative path under the source/destination root. It is
	// the comparison key — the same file on src and dst must produce the
	// same rel.
	rel string
	// size is the object size in bytes.
	size int64
	// mtime is the object's modification time. For remote objects it is
	// the S3 LastModified; for local files it is the file ModTime.
	mtime time.Time
	// url is the absolute location of the object: a StorageURL pointing
	// at the local file or the s3://bucket/key. May be nil for the
	// zero-value sentinel used in delete-only paths.
	url *StorageURL
	// isLocal reports whether this object is a local file (true) or a
	// remote S3 object (false). It is consulted by shouldCopy to apply
	// the upload-roundtrip idempotence window.
	isLocal bool
}

// Sync recursively copies objects from src to dst. Both src and dst are
// StorageURLs produced by NewStorageURL — they may be local paths or
// s3://bucket/prefix/ URIs. Supported directions:
//
//   - local → s3   (upload each new/changed file)
//   - s3   → local (download each new/changed object)
//   - s3   → s3    (server-side CopyObject)
//   - local → local (rejected — use cp instead)
//
// The progress callback, when non-nil, is invoked during each file's
// transfer (throttled to at most one call per 100ms per file) with the
// file's relative path and the per-file transferred/total byte counts,
// and once more at completion with transferred == total. Uploads and
// downloads report real byte progress; server-side CopyObject transfers
// no bytes through the client, so s3→s3 copies report a single
// completion call with (size, size). Deletes report (0, 0) on
// completion. The callback is invoked from worker goroutines and must be
// safe for concurrent use.
//
// Sync does not abort on the first per-file error: it collects errors
// into SyncResult.Errors and continues, so a single failing transfer
// does not abort the rest of the sync. When ctx is cancelled, Sync stops
// scheduling new transfers, waits for in-flight ones to abort, and
// returns the partial result together with ctx.Err().
func (s *Storage) Sync(
	ctx context.Context,
	src, dst *StorageURL,
	opt SyncOptions,
	progress SyncProgressFunc,
) (*SyncResult, error) {
	if src == nil || dst == nil {
		return nil, errors.New("sync: src and dst must be non-nil")
	}
	if opt.Concurrency == 0 {
		opt.Concurrency = 4
	}

	switch {
	case !src.IsRemote() && !dst.IsRemote():
		return nil, errors.New("sync: local-to-local sync is not supported, use cp")
	case src.IsRemote() && !dst.IsRemote():
		return s.syncS3ToLocal(ctx, src, dst, opt, progress)
	case !src.IsRemote() && dst.IsRemote():
		return s.syncLocalToS3(ctx, src, dst, opt, progress)
	case src.IsRemote() && dst.IsRemote():
		return s.syncS3ToS3(ctx, src, dst, opt, progress)
	default:
		return nil, fmt.Errorf("sync: unsupported src=%v dst=%v", src, dst)
	}
}

// listLocalObjects walks the local directory at src and returns a flat,
// relative-path-keyed slice sorted by rel. Non-directory paths are
// rejected: syncing a single file makes every other object on the other
// side "only-in-dst", which combined with Delete would wipe it — use cp
// for single files.
func listLocalObjects(src *StorageURL) ([]syncObject, error) {
	abs := src.Absolute()
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("sync: %q is not a directory, use cp for single files", abs)
	}

	root := abs
	var out []syncObject
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("rel %q: %w", p, err)
		}
		// S3 keys use '/', so normalise the OS-specific separator.
		rel = filepath.ToSlash(rel)
		u, err := NewStorageURL(p)
		if err != nil {
			return fmt.Errorf("parse %q: %w", p, err)
		}
		mt := fi.ModTime()
		out = append(out, syncObject{
			rel:     rel,
			size:    fi.Size(),
			mtime:   mt,
			url:     u,
			isLocal: true,
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %q: %w", root, walkErr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}

// remoteSyncKey resolves the key/prefix a sync URL refers to. For
// IsBucket (empty Path) it is "" so the whole bucket is listed.
func remoteSyncKey(src *StorageURL) string {
	if !src.IsBucket() && src.Prefix == "" {
		return src.Path
	}
	return src.Prefix
}

// remoteRel converts a listed key into its sync-relative path under the
// slash-terminated listing prefix. ok is false for folder placeholder
// keys ("dir/", created by consoles and many tools) at any depth and for
// the listing prefix itself — those are not files.
func remoteRel(key, prefix string) (string, bool) {
	if key == "" || strings.HasSuffix(key, "/") {
		return "", false
	}
	rel := strings.TrimPrefix(key, prefix)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "", false
	}
	return rel, true
}

// listRemoteObjects lists every object under src.Prefix recursively
// (Delimiter cleared) and returns a flat, relative-path-keyed slice
// sorted by rel. The relative path is the full key with the listing
// prefix trimmed. A non-slash-terminated prefix is listed as prefix+"/"
// so sibling keys sharing it as a name prefix (foo vs foobar) never
// leak into the result.
func (s *Storage) listRemoteObjects(ctx context.Context, src *StorageURL) ([]syncObject, error) {
	bucket := src.Bucket
	prefix := remoteSyncKey(src)
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	objects, err := s.remote.ListObjectsRecursive(ctx, bucket, prefix)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", src, err)
	}
	out := make([]syncObject, 0, len(objects))
	for _, obj := range objects {
		key := awsStringOr(obj.Key, "")
		rel, ok := remoteRel(key, prefix)
		if !ok {
			continue
		}
		u, err := NewStorageURL("s3://" + bucket + "/" + key)
		if err != nil {
			return nil, fmt.Errorf("parse s3://%s/%s: %w", bucket, key, err)
		}
		mt := time.Now()
		if obj.LastModified != nil {
			mt = *obj.LastModified
		}
		out = append(out, syncObject{
			rel:     rel,
			size:    awsInt64Or(obj.Size, 0),
			mtime:   mt,
			url:     u,
			isLocal: false,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}

// listRemoteSourceObjects is listRemoteObjects for the sync source. When
// a non-slash-terminated source matches nothing under key+"/" but an
// object exists at the exact key, the source is a single object and sync
// rejects it — mirroring the local non-directory source rejection —
// instead of silently syncing nothing (which with Delete would wipe the
// destination).
func (s *Storage) listRemoteSourceObjects(ctx context.Context, src *StorageURL) ([]syncObject, error) {
	out, err := s.listRemoteObjects(ctx, src)
	if err != nil {
		return nil, err
	}
	key := remoteSyncKey(src)
	if len(out) == 0 && key != "" && !strings.HasSuffix(key, "/") {
		if _, headErr := s.remote.HeadObject(ctx, src.Bucket, key); headErr == nil {
			return nil, fmt.Errorf("sync: source %s is a single object, not a prefix, use cp for single files", src)
		}
	}
	return out, nil
}

// awsStringOr returns the dereferenced value of p, or dflt when p is
// nil. Used to safely read the pointer-typed fields of types.Object
// without pulling the SDK types into this file's public surface.
func awsStringOr(p *string, dflt string) string {
	if p == nil {
		return dflt
	}
	return *p
}

// awsInt64Or is the int64 counterpart of awsStringOr.
func awsInt64Or(p *int64, dflt int64) int64 {
	if p == nil {
		return dflt
	}
	return *p
}

// compilePatterns compiles a list of glob patterns into a slice of
// regexps using strutil.WildCardToRegexp. A pattern matches the whole
// path (anchored). An empty input yields an empty slice.
func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re := strutil.WildCardToRegexp(p)
		re = strutil.MatchFromStartToEnd(re)
		re = strutil.AddNewLineFlag(re)
		r, err := regexp.Compile(re)
		if err != nil {
			return nil, fmt.Errorf("compile pattern %q: %w", p, err)
		}
		out = append(out, r)
	}
	return out, nil
}

// isExcluded reports whether rel matches any exclude pattern. An empty
// exclude slice means "nothing is excluded".
func isExcluded(rel string, patterns []*regexp.Regexp) bool {
	for _, r := range patterns {
		if r.MatchString(rel) {
			return true
		}
	}
	return false
}

// isIncluded reports whether rel matches any include pattern. An empty
// include slice means "everything is included".
func isIncluded(rel string, patterns []*regexp.Regexp) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, r := range patterns {
		if r.MatchString(rel) {
			return true
		}
	}
	return false
}

// shouldCopy decides whether a common source/destination pair should be
// re-transferred. When SizeOnly is true, only a size difference triggers
// a copy. Otherwise (size-and-mtime strategy), a copy is needed when
// the source is newer than the destination OR the sizes differ.
//
// The mtime comparison is best-effort: S3 LastModified has
// millisecond precision and is the server's clock; local files carry
// the client's mtime. So the size-only strategy is the deterministic
// one — mtime is only consulted as a tiebreaker when sizes differ or
// when the source is strictly newer.
//
// Idempotence on real S3-compatible services: when the source is a
// local file and the destination is the S3 object that was just
// uploaded, the local file's mtime is almost always strictly newer
// than the S3 LastModified (S3 stamps LastModified with the server's
// receive time, which lags the file's mtime by the round-trip). To
// make a second identical sync skip rather than re-upload, we treat
// "src is local, dst is remote, sizes equal, src newer by less than
// 2s" as "in sync". The 2s window absorbs the upload round-trip and
// OSS's coarser-than-millisecond clock without masking a genuine
// same-size rewrite that happens more than 2s after the prior sync.
func shouldCopy(src, dst syncObject, sizeOnly bool) bool {
	if sizeOnly {
		return src.size != dst.size
	}
	// src strictly newer → copy unless the size matches and the source
	// is local / dst is remote AND the gap is small. In that case the
	// src mtime being newer is just the upload round-trip talking, not a
	// real content change.
	if src.mtime.After(dst.mtime) {
		if src.size == dst.size && src.isLocal && !dst.isLocal {
			if src.mtime.Sub(dst.mtime) < 2*time.Second {
				return false
			}
		}
		return true
	}
	// sizes differ → copy regardless of mtime.
	if src.size != dst.size {
		return true
	}
	return false
}

// transferKind enumerates the per-direction transfer types a sync task
// can perform. It's used by planAndRun to bump the right SyncResult
// counter on submission.
type transferKind int

const (
	kindNone transferKind = iota
	kindUpload
	kindDownload
	kindCopy
	kindDelete
)

// planAndRun is the canonical sync flow:
//
//  1. Build exclude/include regexps from the user's glob patterns.
//  2. Merge-compare the two sorted slices by relative path.
//  3. For only-in-src (and common pairs the strategy says differ),
//     submit a transfer task via the parallel.Manager.
//  4. For only-in-dst, when opt.Delete is set, submit a delete task.
//
// All task submission happens on the main goroutine so the result
// counters are never written from a worker — workers only return
// errors via the Waiter.
//
// buildTransferTask is per-direction: it knows whether to upload,
// download, or copy. It receives the resolved src/dst syncObjects and
// returns the parallel.Task plus a transferKind so the loop can bump
// the right counter. buildDeleteTask is the per-direction delete
// equivalent (used only when opt.Delete is set).
func (s *Storage) planAndRun(
	ctx context.Context,
	srcObjects, dstObjects []syncObject,
	opt SyncOptions,
	_ SyncProgressFunc, // progress flows through the build*Task closures
	buildTransferTask func(srcObj, dstObj syncObject) (parallel.Task, transferKind),
	buildDeleteTask func(dstObj syncObject) (parallel.Task, transferKind),
) (*SyncResult, error) {
	excludePatterns, err := compilePatterns(opt.Exclude)
	if err != nil {
		return nil, err
	}
	includePatterns, err := compilePatterns(opt.Include)
	if err != nil {
		return nil, err
	}

	mgr := parallel.New(opt.Concurrency)
	waiter := parallel.NewWaiter()

	var result SyncResult
	errDoneCh := make(chan struct{})
	go func() {
		defer close(errDoneCh)
		for err := range waiter.Err() {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			result.Errors = append(result.Errors, err)
		}
	}()

	bump := func(k transferKind) {
		switch k {
		case kindUpload:
			result.Uploaded++
		case kindDownload:
			result.Downloaded++
		case kindCopy:
			result.Copied++
		case kindDelete:
			result.Deleted++
		}
	}

	i, j := 0, 0
	for i < len(srcObjects) || j < len(dstObjects) {
		// Stop scheduling new tasks once the context is cancelled; the
		// in-flight ones abort on their own ctx checks / SDK calls.
		if ctx.Err() != nil {
			break
		}
		var (
			srcObj  syncObject
			dstObj  syncObject
			common  bool
			onlyDst bool
		)
		switch {
		case i < len(srcObjects) && j < len(dstObjects):
			sRel := srcObjects[i].rel
			dRel := dstObjects[j].rel
			switch {
			case sRel < dRel:
				srcObj = srcObjects[i]
				i++
			case sRel == dRel:
				srcObj = srcObjects[i]
				dstObj = dstObjects[j]
				common = true
				i++
				j++
			default:
				dstObj = dstObjects[j]
				onlyDst = true
				j++
			}
		case i < len(srcObjects):
			srcObj = srcObjects[i]
			i++
		default:
			dstObj = dstObjects[j]
			onlyDst = true
			j++
		}

		// Apply exclude/include on the relative path. The path is the
		// same on src and dst by construction (that's what the
		// merge-compare guarantees).
		rel := srcObj.rel
		if rel == "" {
			rel = dstObj.rel
		}
		if isExcluded(rel, excludePatterns) || !isIncluded(rel, includePatterns) {
			result.Skipped++
			continue
		}

		if onlyDst {
			if !opt.Delete {
				result.Skipped++
				continue
			}
			task, kind := buildDeleteTask(dstObj)
			bump(kind)
			if opt.DryRun {
				continue
			}
			mgr.Run(task, waiter)
			continue
		}

		if common && !shouldCopy(srcObj, dstObj, opt.SizeOnly) {
			result.Skipped++
			continue
		}
		task, kind := buildTransferTask(srcObj, dstObj)
		bump(kind)
		if opt.DryRun {
			continue
		}
		mgr.Run(task, waiter)
	}

	mgr.Close()
	waiter.Wait()
	<-errDoneCh
	if err := ctx.Err(); err != nil {
		return &result, err
	}
	return &result, nil
}

// syncLocalToS3 uploads new/changed local files to a remote prefix.
func (s *Storage) syncLocalToS3(
	ctx context.Context,
	src, dst *StorageURL,
	opt SyncOptions,
	progress SyncProgressFunc,
) (*SyncResult, error) {
	srcObjects, err := listLocalObjects(src)
	if err != nil {
		return nil, err
	}
	dstObjects, err := s.listRemoteObjects(ctx, dst)
	if err != nil {
		return nil, err
	}

	buildTransferTask := func(srcObj, _ syncObject) (parallel.Task, transferKind) {
		dstKey := joinS3Key(dst.Prefix, srcObj.rel)
		dstBucket := dst.Bucket
		srcPath := srcObj.url.Absolute()
		size := srcObj.size
		rel := srcObj.rel
		task := func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			f, err := os.Open(srcPath)
			if err != nil {
				return fmt.Errorf("open %q: %w", srcPath, err)
			}
			defer func() { _ = f.Close() }()
			tracker := syncTracker(rel, size, progress)
			// Upload through the manager so files beyond the single-PUT
			// limit go multipart, honouring opt.PartSize. The plain
			// (non-seekable) reader keeps the manager in sequential part
			// reads so every byte is counted exactly once.
			body := &progressReader{r: f, t: tracker}
			if _, err := s.remote.UploadObjectWithPartSize(ctx, body, dstBucket, dstKey, opt.PartSize); err != nil {
				return fmt.Errorf("upload %s -> s3://%s/%s: %w", srcPath, dstBucket, dstKey, err)
			}
			tracker.finish()
			return nil
		}
		return task, kindUpload
	}

	buildDeleteTask := func(dstObj syncObject) (parallel.Task, transferKind) {
		bucket := dstObj.url.Bucket
		key := dstObj.url.Path
		rel := dstObj.rel
		task := func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.remote.DeleteObjects(ctx, bucket, []string{key}); err != nil {
				return fmt.Errorf("delete s3://%s/%s: %w", bucket, key, err)
			}
			if progress != nil {
				progress(rel, 0, 0)
			}
			return nil
		}
		return task, kindDelete
	}

	return s.planAndRun(ctx, srcObjects, dstObjects, opt, progress, buildTransferTask, buildDeleteTask)
}

// syncS3ToLocal downloads new/changed remote objects to a local
// directory.
func (s *Storage) syncS3ToLocal(
	ctx context.Context,
	src, dst *StorageURL,
	opt SyncOptions,
	progress SyncProgressFunc,
) (*SyncResult, error) {
	srcObjects, err := s.listRemoteSourceObjects(ctx, src)
	if err != nil {
		return nil, err
	}

	// A missing local destination is an empty listing. Create it only on
	// a real run — dry-run must not mutate the filesystem.
	dstAbs := dst.Absolute()
	var dstObjects []syncObject
	if _, statErr := os.Stat(dstAbs); statErr != nil && os.IsNotExist(statErr) {
		if !opt.DryRun {
			if err := os.MkdirAll(dstAbs, 0o755); err != nil {
				return nil, fmt.Errorf("mkdir %q: %w", dstAbs, err)
			}
		}
	} else {
		dstObjects, err = listLocalObjects(dst)
		if err != nil {
			return nil, err
		}
	}

	buildTransferTask := func(srcObj, _ syncObject) (parallel.Task, transferKind) {
		srcBucket := srcObj.url.Bucket
		srcKey := srcObj.url.Path
		dstPath := filepath.Join(dst.Absolute(), filepath.FromSlash(srcObj.rel))
		size := srcObj.size
		rel := srcObj.rel
		task := func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
				return fmt.Errorf("mkdir %q: %w", filepath.Dir(dstPath), err)
			}
			tracker := syncTracker(rel, size, progress)
			if err := s.downloadFileTo(ctx, srcBucket, srcKey, dstPath, tracker); err != nil {
				return fmt.Errorf("download s3://%s/%s -> %s: %w", srcBucket, srcKey, dstPath, err)
			}
			tracker.finish()
			return nil
		}
		return task, kindDownload
	}

	buildDeleteTask := func(dstObj syncObject) (parallel.Task, transferKind) {
		dstPath := dstObj.url.Absolute()
		rel := dstObj.rel
		task := func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := os.Remove(dstPath); err != nil {
				return fmt.Errorf("delete %s: %w", dstPath, err)
			}
			if progress != nil {
				progress(rel, 0, 0)
			}
			return nil
		}
		return task, kindDelete
	}

	return s.planAndRun(ctx, srcObjects, dstObjects, opt, progress, buildTransferTask, buildDeleteTask)
}

// syncS3ToS3 performs server-side CopyObjects between two
// buckets/prefixes.
func (s *Storage) syncS3ToS3(
	ctx context.Context,
	src, dst *StorageURL,
	opt SyncOptions,
	progress SyncProgressFunc,
) (*SyncResult, error) {
	srcObjects, err := s.listRemoteSourceObjects(ctx, src)
	if err != nil {
		return nil, err
	}
	dstObjects, err := s.listRemoteObjects(ctx, dst)
	if err != nil {
		return nil, err
	}

	buildTransferTask := func(srcObj, _ syncObject) (parallel.Task, transferKind) {
		srcBucket := srcObj.url.Bucket
		srcKey := srcObj.url.Path
		dstBucket := dst.Bucket
		dstKey := joinS3Key(dst.Prefix, srcObj.rel)
		size := srcObj.size
		rel := srcObj.rel
		task := func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.remote.CopyObject(ctx, srcBucket, srcKey, dstBucket, dstKey); err != nil {
				return fmt.Errorf("copy s3://%s/%s -> s3://%s/%s: %w", srcBucket, srcKey, dstBucket, dstKey, err)
			}
			// Server-side copy moves no bytes through the client, so the
			// finest granularity available is file completion.
			if progress != nil {
				progress(rel, size, size)
			}
			return nil
		}
		return task, kindCopy
	}

	buildDeleteTask := func(dstObj syncObject) (parallel.Task, transferKind) {
		bucket := dstObj.url.Bucket
		key := dstObj.url.Path
		rel := dstObj.rel
		task := func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.remote.DeleteObjects(ctx, bucket, []string{key}); err != nil {
				return fmt.Errorf("delete s3://%s/%s: %w", bucket, key, err)
			}
			if progress != nil {
				progress(rel, 0, 0)
			}
			return nil
		}
		return task, kindDelete
	}

	return s.planAndRun(ctx, srcObjects, dstObjects, opt, progress, buildTransferTask, buildDeleteTask)
}

// downloadFileTo is a minimal local-download helper that writes the
// object body to dstPath without going through the S3Store.DownloadFile
// waiter (which adds a minute-long HeadObject wait that gofakes3 does
// not implement and that is unnecessary for sync). tracker may be nil;
// when non-nil it receives throttled byte progress as the body is
// written.
func (s *Storage) downloadFileTo(ctx context.Context, bucket, key, dstPath string, tracker *progressTracker) error {
	out, err := s.remote.GetObject(ctx, bucket, key)
	if err != nil {
		return err
	}
	defer func() { _ = out.Body.Close() }()
	// Stream into a temp file in the destination directory and rename on
	// success, so a failed or cancelled transfer never truncates an
	// existing local copy or leaves a partial file at the final path.
	f, err := os.CreateTemp(filepath.Dir(dstPath), filepath.Base(dstPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := io.Copy(&progressWriter{w: f, t: tracker}, out.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	// Close errors matter on the write path (buffered data may fail to
	// flush), so surface them instead of deferring.
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// CreateTemp files are 0600; restore os.Create's default.
	if err := os.Chmod(tmp, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dstPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// joinS3Key concatenates a remote prefix and a relative path into a
// full S3 key, ensuring exactly one '/' between them. Empty prefix
// yields the rel verbatim.
func joinS3Key(prefix, rel string) string {
	if prefix == "" {
		return rel
	}
	if strings.HasSuffix(prefix, "/") {
		return prefix + rel
	}
	return prefix + "/" + rel
}
