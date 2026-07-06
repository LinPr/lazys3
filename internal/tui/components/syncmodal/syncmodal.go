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

	"github.com/LinPr/lazys3/internal/storage"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/types"
	tea "github.com/charmbracelet/bubbletea/v2"
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

// progressState is the shared struct the sync's worker goroutines write
// (via record) and the poll loop reads (via snapshot). Sync's progress
// callback fires repeatedly DURING each file transfer (throttled) plus a
// final call with transferred==total, so a file counts as done exactly
// once: on its first observation with total >= 0 and transferred >= total.
// Deletes report (0,0) and therefore count immediately.
type progressState struct {
	mu        sync.Mutex
	doneFiles map[string]struct{}
	filesDone int
	curFile   string
	curBytes  int64
	curTotal  int64
}

func newProgressState() *progressState {
	return &progressState{doneFiles: make(map[string]struct{})}
}

// record applies one progress callback observation. Callbacks run on
// worker goroutines; the mutex keeps the done-set and counters coherent.
func (ps *progressState) record(file string, transferred, total int64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.curFile, ps.curBytes, ps.curTotal = file, transferred, total
	if total >= 0 && transferred >= total {
		if _, ok := ps.doneFiles[file]; !ok {
			ps.doneFiles[file] = struct{}{}
			ps.filesDone++
		}
	}
}

// snapshot returns the current counters for the poll loop.
func (ps *progressState) snapshot() (filesDone int, file string, bytes, total int64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.filesDone, ps.curFile, ps.curBytes, ps.curTotal
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

func unregister(id string) {
	regMu.Lock()
	defer regMu.Unlock()
	delete(registry, id)
}

func lookup(id string) (*progressState, bool) {
	regMu.Lock()
	defer regMu.Unlock()
	ps, ok := registry[id]
	return ps, ok
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
	// Label is the row label echoed on every TransferDoneMsg (and thus
	// into the persistent history). Empty falls back to
	// "sync <src> -> <dst>"; the dual-pane dir copies pass their
	// "dir: ..." row label so the history matches the panel.
	Label string
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
