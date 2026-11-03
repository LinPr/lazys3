package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/LinPr/lazys3/internal/storage"
	fsstore "github.com/LinPr/lazys3/internal/storage/fs"
	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/syncmodal"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// syncConnParams resolves the active profile's endpoint/path-style and
// name for building a storage.Storage inside a sync Cmd. Resolved up front
// by the prompt flows (never inside a modal callback, which runs against a
// stale model).
func (m *Model) syncConnParams() connParams {
	conn := connParams{
		configFile:     m.awsFiles.ConfigFile,
		credentialFile: m.awsFiles.CredentialsFile,
	}
	if p := m.profileList.GetSelectedProfile(); p != nil {
		conn.endpointURL = p.EndpointURL
		conn.pathStyle = shouldUsePathStyle(conn.endpointURL)
		conn.profile = m.selectedProfile
	}
	// The region MUST travel with the sync client too: signing with the
	// wrong region makes S3-compatible services reject the request
	// (SignatureDoesNotMatch) even though their endpoints are custom.
	conn.region = m.selectedRegion
	conn.client = m.s3ClientOptions()
	return conn
}

// showInputModalCmd asks the root Update to open the input modal on the
// live model (see types.ShowInputModalMsg).
func showInputModalCmd(title, placeholder string, onConfirm func(string) tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		return types.ShowInputModalMsg{
			Title:       title,
			Placeholder: placeholder,
			OnConfirm:   onConfirm,
		}
	}
}

// connParams carries the connection facts a sync Cmd needs to build a
// storage.Storage: the active profile's endpoint/path-style/name plus the
// resolved AWS shared file paths.
type connParams struct {
	endpointURL    string
	pathStyle      bool
	profile        string
	region         string
	configFile     string
	credentialFile string
	client         s3store.ClientOptions
}

// syncS3Option converts the resolved connection facts into the SDK
// options a sync client needs. It exists as a named function so the
// identity assertion in s3identity_test.go covers the sync path too:
// this builder previously omitted Region.
func syncS3Option(conn connParams) s3store.S3Option {
	return s3store.S3Option{
		UsePathStyle:   conn.pathStyle,
		Profile:        conn.profile,
		Region:         conn.region,
		Endpoint:       conn.endpointURL,
		ConfigFile:     conn.configFile,
		CredentialFile: conn.credentialFile,
		Client:         conn.client,
	}
}

// startSyncCmd dispatches the user-typed sync flow ('s' key) with the
// default "sync src -> dst" row label.
func startSyncCmd(src, dst, flagsStr string, conn connParams) tea.Cmd {
	label := fmt.Sprintf("sync %s -> %s", src, dst)
	return syncTransferCmd(src, dst, label, syncmodal.ParseFlags(flagsStr), conn)
}

// syncTransferCmd dispatches one sync via syncmodal.NewCmd, plus the first
// tea.Every poll tick. The SyncPollMsg handler in tui.go re-arms the ticker
// while the sync is registered, so per-file progress keeps flowing to the
// panel. The row carries the sync context's CancelFunc so 'x' aborts it.
// Besides the 's' flow it backs the dual-pane directory copies, which pass
// zero Flags (a plain recursive one-way copy) and a "dir: ..." label.
//
// The storage.Storage is built lazily inside the sync Cmd (StorageFn):
// NewStorage resolves credentials and can block on network I/O, so it must
// not run on the Update goroutine.
func syncTransferCmd(src, dst, label string, flags syncmodal.Flags, conn connParams) tea.Cmd {
	id := transferpanel.NewID()

	// Use the S3Option that the objectlist flow already uses, so the sync
	// talks to the same endpoint as the listing.
	s3opt := syncS3Option(conn)
	storageOpt := storage.NewStorageOption(s3opt, fsstore.LocalOption{})
	ctx, cancel := context.WithCancel(context.Background())

	// The row's shared Progress receives the AGGREGATE directory bytes
	// (whole-plan done/total) from syncmodal once planning finishes, so the
	// row bar tracks the directory, not the current file; until then it
	// stays indeterminate (-1 total).
	prog := transferpanel.NewProgress()

	add := addTransferCmd(transferpanel.Transfer{
		ID:       id,
		Op:       transferpanel.OpSync,
		Label:    label,
		Status:   transferpanel.StatusRunning,
		Cancel:   cancel,
		Progress: prog,
	})

	syncCmd := syncmodal.NewCmd(syncmodal.CmdDeps{
		Ctx: ctx,
		StorageFn: func(ctx context.Context) (*storage.Storage, error) {
			return storage.NewStorage(ctx, *storageOpt)
		},
		Src:        src,
		Dst:        dst,
		Flags:      flags,
		TransferID: id,
		Label:      label,
		Progress:   prog,
	})

	poll := tea.Every(200*time.Millisecond, syncmodal.PollCmd(id))

	// Sequence the add before the sync so the row exists when the sync's
	// TransferDoneMsg arrives; the poll ticker runs alongside.
	return tea.Batch(tea.Sequence(add, syncCmd), poll)
}

// promptSync starts the chained-modal sync flow. It prompts for source,
// then destination, then a flags string, and on the final confirm runs
// the sync via the syncmodal package.
//
// The chained-modal approach reuses Track B's modal as-is: each Show
// call's onConfirm callback opens the next modal. The final callback
// builds a storage.Storage from the active profile's endpoint/path-style
// and dispatches the sync via syncmodal.NewCmd + tea.Every for the
// polling loop.
//
// The single-pane flow passes the current s3://bucket/prefix as the
// default source (so the user can sync the current prefix to a local dir
// by typing the destination and hitting enter) and an empty destination.
// Dual-pane mode prefills src with the focused pane's location and dst
// with the other pane's (see promptDualSync); both stay editable. The
// default flags string is always empty (a default size-and-mtime sync).
func (m *Model) promptSync(defaultSrc, defaultDst string) tea.Cmd {
	// Capture the active profile's endpoint/path-style so the final
	// callback can build a storage.Storage. We resolve these up front
	// (rather than inside the callback) because the user could navigate
	// away while the modal is open.
	conn := m.syncConnParams()

	// The chained modals are reopened via ShowInputModalMsg rather than by
	// calling m.modal.Show inside the callbacks: the callbacks run against
	// a stale captured model, so the next modal must be opened message-style
	// on the live model.
	m.modal.Show(
		"Sync source (s3://bucket/prefix or local dir)",
		defaultSrc,
		func(src string) tea.Cmd {
			// Destination: empty for the user to type in single-pane (a
			// local path for s3→local, or s3://bucket/prefix/ for
			// local→s3 / s3→s3); the other pane's location in dual mode.
			return showInputModalCmd("Sync destination", defaultDst, func(dst string) tea.Cmd {
				return showInputModalCmd(
					"Sync flags (--delete --size-only --dry-run --exclude=*.log)",
					"",
					func(flagsStr string) tea.Cmd {
						return startSyncCmd(src, dst, flagsStr, conn)
					},
				)
			})
		},
	)
	return nil
}

// refreshAfterOp returns the tea.Cmd that re-fetches the list touched by
// the completed op. Downloads/uploads don't need a refresh of the remote
// list, but deletes/copies/renames/mb/rb/sync do.
func (m *Model) refreshAfterOp(done transferpanel.TransferDoneMsg) tea.Cmd {
	// A local-filesystem op (dual-pane local delete) never changes the
	// remote listing: refresh the local pane only.
	if done.Local {
		return m.localList.Refresh()
	}
	var cmds []tea.Cmd
	switch done.Op {
	case transferpanel.OpDelete, transferpanel.OpCopy, transferpanel.OpRename,
		transferpanel.OpUpload:
		if m.state == state.ActiveObjectList {
			cmds = append(cmds, m.objectlist.Fetch(m.objectListOptionFromState()))
		}
	case transferpanel.OpMakeBucket, transferpanel.OpDeleteBucket:
		if m.state == state.ActiveBucketList {
			opt := m.bucketListOptionFromState()
			m.bucketList.SetLoading(true)
			cmds = append(cmds, bucketlist.FetchBucketListCmd(&opt))
		}
	case transferpanel.OpSync:
		// A sync may add/delete objects in the currently viewed listing.
		switch m.state {
		case state.ActiveObjectList:
			cmds = append(cmds, m.objectlist.Fetch(m.objectListOptionFromState()))
		case state.ActiveBucketList:
			opt := m.bucketListOptionFromState()
			m.bucketList.SetLoading(true)
			cmds = append(cmds, bucketlist.FetchBucketListCmd(&opt))
		}
	}
	// Downloads and syncs may have created files in the local pane's
	// directory; refresh it (cursor kept) while dual mode is showing it.
	if m.dualPane && (done.Op == transferpanel.OpDownload || done.Op == transferpanel.OpSync) {
		if cmd := m.localList.Refresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}
