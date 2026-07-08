// Package syncmodal owns the chained-modal flow that drives
// (*storage.Storage).Sync from the TUI.
//
// The flow prompts for three values in sequence (source, destination,
// flags) using Track B's modal as a black box. On the final confirm it
// builds a storage.SyncOptions from the flags string, parses src/dst
// via storage.NewStorageURL, and returns a tea.Cmd that:
//
//   - adds a sync transfer to the panel (TransferAddMsg with Op="sync")
//   - emits a TransferStartMsg
//   - runs (*Storage).Sync in a goroutine, polling a shared progress
//     struct via tea.Every + SyncPollMsg
//   - on completion returns a TransferDoneMsg and a TaskFinishedMsg
//
// The package is intentionally small: it owns no UI surface of its own
// and is invoked from handler.go's `s` key branch.
package syncmodal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/LinPr/lazys3/internal/storage"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// Flags is the parsed, typed view of the user's flags string.
type Flags struct {
	Delete   bool
	SizeOnly bool
	DryRun   bool
	Exclude  []string
	Include  []string
}

// ParseFlags parses a flags string like "--delete --size-only --dry-run
// --exclude=*.log --include=*.txt" into a Flags struct. Unknown flags
// are silently dropped; this is a TUI, not a CLI, so we keep the
// surface minimal.
func ParseFlags(s string) Flags {
	var f Flags
	for _, field := range strings.Fields(s) {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		switch {
		case field == "--delete":
			f.Delete = true
		case field == "--size-only":
			f.SizeOnly = true
		case field == "--dry-run":
			f.DryRun = true
		case strings.HasPrefix(field, "--exclude="):
			f.Exclude = append(f.Exclude, strings.TrimPrefix(field, "--exclude="))
		case strings.HasPrefix(field, "--include="):
			f.Include = append(f.Include, strings.TrimPrefix(field, "--include="))
		}
	}
	return f
}

// FileProgress is a per-file snapshot of one sync plan entry, exposed to
// the transfers overlay's detail view via PerFile. Transferred is
// max-so-far and clamped to Size; Done flips on the file's completion
// event (transferred >= total, which deletes report as (0,0)). Failed
// marks files still without that event when the sync returned — the task
// errored, or a cancel stopped it from ever running — so a finished
// sync's cached snapshot has no forever-"running/queued" rows.
type FileProgress struct {
	Rel         string
	Size        int64
	Transferred int64
	Done        bool
	Failed      bool
	Deleted     bool
}

// progressState is the shared struct the sync's worker goroutines write
// (via record) and the poll loop reads (via snapshot). Sync's progress
// callback fires repeatedly DURING each file transfer (throttled) plus a
// final call with transferred==total, so a file counts as done exactly
// once: on its first observation with total >= 0 and transferred >= total.
// Deletes report (0,0) and therefore count immediately.
//
// Once SyncOptions.OnPlan delivers the plan (setPlan), the state also
// tracks per-file byte progress and the whole-directory aggregate
// (doneBytes/totalBytes over the non-delete entries), and mirrors that
// aggregate into the transfer row's *transferpanel.Progress on every
// record — so the row bar, the panel tick loop, and the 100%-on-done
// rule all see directory-accurate bytes instead of the current file's.
type progressState struct {
	mu        sync.Mutex
	doneFiles map[string]struct{}
	filesDone int
	curFile   string
	curBytes  int64
	curTotal  int64

	plan       []FileProgress
	planIdx    map[string]int
	planSet    bool
	totalBytes int64
	doneBytes  int64
	row        *transferpanel.Progress
}

func newProgressState() *progressState {
	return &progressState{doneFiles: make(map[string]struct{})}
}

// setPlan installs the OnPlan result: per-file sizes in plan order, the
// aggregate byte total (deletes excluded), and an initial 0/total report
// on the row Progress so its bar turns determinate as soon as planning
// finishes. Called once, before any transfer task runs.
func (ps *progressState) setPlan(files []storage.PlannedTransfer) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.plan = make([]FileProgress, len(files))
	ps.planIdx = make(map[string]int, len(files))
	ps.totalBytes = 0
	for i, f := range files {
		ps.plan[i] = FileProgress{Rel: f.Rel, Size: f.Size, Deleted: f.Delete}
		ps.planIdx[f.Rel] = i
		if !f.Delete {
			ps.totalBytes += f.Size
		}
	}
	ps.planSet = true
	if ps.row != nil {
		ps.row.Report(ps.doneBytes, ps.totalBytes)
	}
}

// record applies one progress callback observation. Callbacks run on
// worker goroutines; the mutex keeps the done-set and counters coherent.
func (ps *progressState) record(file string, transferred, total int64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.curFile, ps.curBytes, ps.curTotal = file, transferred, total
	done := total >= 0 && transferred >= total
	if done {
		if _, ok := ps.doneFiles[file]; !ok {
			ps.doneFiles[file] = struct{}{}
			ps.filesDone++
		}
	}
	if i, ok := ps.planIdx[file]; ok {
		f := &ps.plan[i]
		if done {
			f.Done = true
		}
		if !f.Deleted {
			// Max-so-far, clamped to the planned size (an upload may
			// re-read its body once and reset the SDK's count); the
			// completion event snaps the file to its full size so the
			// aggregate converges to totalBytes even when the object
			// changed between listing and transfer.
			nt := transferred
			if done || nt > f.Size {
				nt = f.Size
			}
			if nt > f.Transferred {
				ps.doneBytes += nt - f.Transferred
				f.Transferred = nt
			}
		}
	}
	if ps.planSet && ps.row != nil {
		ps.row.Report(ps.doneBytes, ps.totalBytes)
	}
}

// snapshot returns the counters for the poll loop. Once the plan is
// known, bytes/total are the DIRECTORY aggregate (deletes excluded);
// before that they are the last raw callback pair (a plain per-file
// observation), matching the pre-plan indeterminate row bar.
func (ps *progressState) snapshot() (filesDone int, file string, bytes, total int64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.planSet {
		return ps.filesDone, ps.curFile, ps.doneBytes, ps.totalBytes
	}
	return ps.filesDone, ps.curFile, ps.curBytes, ps.curTotal
}

// aggregateBytes returns the whole-plan byte progress; ok is false until
// the plan is known.
func (ps *progressState) aggregateBytes() (done, total int64, ok bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.doneBytes, ps.totalBytes, ps.planSet
}

// perFile returns a copy of the per-file plan snapshot; ok is false until
// the plan is known.
func (ps *progressState) perFile() ([]FileProgress, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if !ps.planSet {
		return nil, false
	}
	out := make([]FileProgress, len(ps.plan))
	copy(out, ps.plan)
	return out, true
}

// registry maps a transfer ID to its running sync's progress state so
// PollCmd can snapshot it. Entries are registered synchronously by NewCmd
// (before the first poll can fire) and unregistered when the sync Cmd
// returns, which is how the poll loop knows to stop.
var (
	regMu    sync.Mutex
	registry = make(map[string]*progressState)
)

func register(id string, ps *progressState) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[id] = ps
}

// unregister removes the live entry and, when the sync got as far as a
// plan, moves its final per-file snapshot into the completed-plan cache so
// the transfers overlay's detail view keeps working after completion.
func unregister(id string) {
	regMu.Lock()
	ps, ok := registry[id]
	delete(registry, id)
	regMu.Unlock()
	if !ok {
		return
	}
	if files, ok := ps.perFile(); ok {
		// The sync has returned, so every file either produced its
		// completion event (successful transfers always do, even when the
		// object shrank below its planned size) or is terminally failed.
		for i := range files {
			if !files[i].Done {
				files[i].Failed = true
			}
		}
		cacheCompleted(id, files)
	}
}

func lookup(id string) (*progressState, bool) {
	regMu.Lock()
	defer regMu.Unlock()
	ps, ok := registry[id]
	return ps, ok
}

// completedCap bounds the completed-plan cache: the transfer panel itself
// keeps at most 100 history rows, but per-file plans can be large, so only
// the most recent finished syncs stay inspectable in the detail view.
const completedCap = 16

var (
	completedMu    sync.Mutex
	completedPlans = make(map[string][]FileProgress)
	completedOrder []string
)

// cacheCompleted stores a finished sync's final per-file snapshot, evicting
// the oldest entry beyond completedCap.
func cacheCompleted(id string, files []FileProgress) {
	completedMu.Lock()
	defer completedMu.Unlock()
	if _, ok := completedPlans[id]; !ok {
		completedOrder = append(completedOrder, id)
	}
	completedPlans[id] = files
	for len(completedOrder) > completedCap {
		oldest := completedOrder[0]
		completedOrder = completedOrder[1:]
		delete(completedPlans, oldest)
	}
}

// PerFile returns the named sync's per-file plan snapshot: live from the
// registry while the sync runs, from the completed-plan cache after it
// finishes. ok is false when the sync never reached planning or its cache
// entry was evicted.
func PerFile(transferID string) ([]FileProgress, bool) {
	if ps, ok := lookup(transferID); ok {
		return ps.perFile()
	}
	completedMu.Lock()
	defer completedMu.Unlock()
	files, ok := completedPlans[transferID]
	if !ok {
		return nil, false
	}
	out := make([]FileProgress, len(files))
	copy(out, files)
	return out, true
}

// AggregateBytes returns the named RUNNING sync's whole-plan byte progress
// (deletes excluded from the totals). ok is false before the plan is known
// and after the sync unregisters.
func AggregateBytes(transferID string) (doneBytes, totalBytes int64, ok bool) {
	ps, found := lookup(transferID)
	if !found {
		return 0, 0, false
	}
	return ps.aggregateBytes()
}

// CmdDeps is the bundle of values the handler passes into NewCmd. It is
// a struct (not individual args) so the handler does not need to import
// storage/transferpanel/types itself to call us.
type CmdDeps struct {
	// Ctx is the cancellable context the sync runs under. The handler
	// stores its CancelFunc on the transfer row so the user can abort.
	// Nil falls back to context.Background().
	Ctx context.Context
	// Storage is the storage.Storage the sync runs against. The handler
	// builds it from the active profile + endpoint/path-style.
	Storage *storage.Storage
	// StorageFn lazily builds the Storage inside the returned Cmd (off
	// the Update goroutine — NewStorage can block on credential
	// resolution). Used when Storage is nil.
	StorageFn func(context.Context) (*storage.Storage, error)
	// Src is the source path (local dir or s3://bucket/prefix/).
	Src string
	// Dst is the destination path.
	Dst string
	// Flags is the parsed flags string.
	Flags Flags
	// TransferID is the transfer-panel row the sync updates. The
	// handler assigns it via transferpanel.NewID before calling NewCmd.
	TransferID string
	// Label is the row label echoed on every TransferDoneMsg. Empty
	// falls back to "sync <src> -> <dst>"; the dual-pane dir copies pass
	// their "dir: ..." row label so the done message matches the panel.
	Label string
	// Progress, when non-nil, is the transfer row's shared byte counter.
	// It receives the AGGREGATE directory progress (doneBytes/totalBytes
	// over the whole plan, deletes excluded) on every callback once the
	// plan is known, so the row bar and the panel's 100%-on-done rule are
	// directory-accurate rather than tracking the current file.
	Progress *transferpanel.Progress
}

// NewCmd returns a tea.Cmd that runs the sync and emits the transfer
// panel messages. The handler batches the returned Cmd with
// tea.Every(200ms, ...) for the polling loop.
//
// The Cmd is structured so the actual sync runs in a goroutine and
// reports progress via a shared struct; the goroutine returns a
// tea.Msg (TransferDoneMsg) on completion.
func NewCmd(deps CmdDeps) tea.Cmd {
	// Register the shared progress state synchronously so a poll firing
	// before the sync goroutine starts still sees an active sync.
	ps := newProgressState()
	ps.row = deps.Progress
	register(deps.TransferID, ps)
	label := deps.Label
	if label == "" {
		label = labelFor(deps.Src, deps.Dst)
	}
	return func() tea.Msg {
		defer unregister(deps.TransferID)
		// Re-validate inputs here so the Cmd is self-contained.
		src, err := storage.NewStorageURL(deps.Src)
		if err != nil {
			return transferpanel.TransferDoneMsg{
				ID:    deps.TransferID,
				Err:   fmt.Errorf("parse src %q: %w", deps.Src, err),
				Op:    transferpanel.OpSync,
				Label: label,
			}
		}
		dst, err := storage.NewStorageURL(deps.Dst)
		if err != nil {
			return transferpanel.TransferDoneMsg{
				ID:    deps.TransferID,
				Err:   fmt.Errorf("parse dst %q: %w", deps.Dst, err),
				Op:    transferpanel.OpSync,
				Label: label,
			}
		}

		ctx := deps.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		st := deps.Storage
		if st == nil && deps.StorageFn != nil {
			st, err = deps.StorageFn(ctx)
			if err != nil {
				return transferpanel.TransferDoneMsg{
					ID:    deps.TransferID,
					Err:   fmt.Errorf("sync: build storage: %w", err),
					Op:    transferpanel.OpSync,
					Label: label,
				}
			}
		}
		if st == nil {
			return transferpanel.TransferDoneMsg{
				ID:    deps.TransferID,
				Err:   errors.New("sync: storage is nil"),
				Op:    transferpanel.OpSync,
				Label: label,
			}
		}

		opt := storage.SyncOptions{
			Delete:      deps.Flags.Delete,
			SizeOnly:    deps.Flags.SizeOnly,
			DryRun:      deps.Flags.DryRun,
			Exclude:     deps.Flags.Exclude,
			Include:     deps.Flags.Include,
			Concurrency: 4,
			OnPlan:      ps.setPlan,
		}

		res, err := st.Sync(ctx, src, dst, opt, ps.record)
		if err != nil {
			return transferpanel.TransferDoneMsg{
				ID:    deps.TransferID,
				Err:   err,
				Op:    transferpanel.OpSync,
				Label: label,
			}
		}
		// Attach the summary as the row note; a fast sync can finish
		// before the first 200ms poll, so it must not come from the
		// poll loop.
		note := fmt.Sprintf("%d up, %d down, %d cp, %d rm, %d skip",
			res.Uploaded, res.Downloaded, res.Copied, res.Deleted, res.Skipped)
		var firstErr error
		if len(res.Errors) > 0 {
			firstErr = res.Errors[0]
			note += fmt.Sprintf(", %d failed", len(res.Errors))
		}
		return transferpanel.TransferDoneMsg{
			ID:    deps.TransferID,
			Err:   firstErr,
			Op:    transferpanel.OpSync,
			Label: label,
			Note:  note,
		}
	}
}

// labelFor is a tiny helper used in error/done messages so the panel
// always has a non-empty label even before the sync result is known.
func labelFor(src, dst string) string {
	return fmt.Sprintf("sync %s -> %s", src, dst)
}

// PollCmd returns the func for tea.Every that snapshots the running
// sync's progress into a SyncPollMsg. tea.Every fires once, so the TUI's
// SyncPollMsg handler re-arms it while Active is true; once the sync Cmd
// returns and unregisters its state, Active flips false and the loop
// stops.
func PollCmd(transferID string) func(time.Time) tea.Msg {
	return func(time.Time) tea.Msg {
		msg := types.SyncPollMsg{TransferID: transferID}
		if ps, ok := lookup(transferID); ok {
			msg.Active = true
			msg.FilesDone, msg.CurrentFile, msg.Bytes, msg.Total = ps.snapshot()
		}
		return msg
	}
}
