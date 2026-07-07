package types

import tea "charm.land/bubbletea/v2"

type TaskFinishedMsg struct {
	TaskID      string
	SectionID   int
	SectionType string
	Err         error
	Msg         tea.Msg
}

type ErrMsg struct {
	Err error
}

func (e ErrMsg) Error() string { return e.Err.Error() }

// SyncPollMsg is emitted by a tea.Every ticker while a sync operation is
// running. syncmodal.PollCmd snapshots the sync's shared progress state
// into the message; the TUI's Update forwards it to the transfer panel and
// re-arms the ticker while Active is true.
type SyncPollMsg struct {
	// TransferID is the transfer-panel row the poll updates. It is
	// assigned when the sync starts and stays stable for the lifetime
	// of the sync.
	TransferID string
	// Active reports whether the sync is still registered (running).
	// When false the poll loop stops re-arming.
	Active bool
	// FilesDone counts files whose transfer completed.
	FilesDone int
	// CurrentFile is the file most recently reported by the progress
	// callback; Bytes/Total are its byte progress (Total 0 for deletes,
	// -1 when unknown).
	CurrentFile string
	Bytes       int64
	Total       int64
}

// ShowInputModalMsg asks the root model to open the input modal. Chained
// modal flows (sync's src → dst → flags) emit this from an onConfirm
// callback so the next modal opens on the live model rather than on a
// stale captured copy.
type ShowInputModalMsg struct {
	Title       string
	Placeholder string
	OnConfirm   func(string) tea.Cmd
}

// StatusUpdateMsg refreshes the persistent status bar with the current
// navigation context, the focused pane, the multi-select count and the
// transfer-row tallies. The TUI's Update emits this after dispatching to
// the active list so the bar always reflects the post-update state; it is
// deduplicated against the previous emission, so every field except
// ClearInfo participates in change detection.
//
// Track D owns this message type; the statusbar component is the only
// consumer.
type StatusUpdateMsg struct {
	Profile string
	Bucket  string
	Prefix  string
	// SelectedCount mirrors the FOCUSED pane's multi-selection in dual
	// mode (the local pane's when it is focused).
	SelectedCount int
	// Pane names the focused pane while dual-pane mode is active
	// ("local" / "remote"); empty in single-pane mode.
	Pane string
	// Transfer-row tallies from transferpanel.Counts: running includes
	// queued rows, failed includes canceled ones.
	TransfersRunning int
	TransfersDone    int
	TransfersFailed  int
	// ClearInfo dismisses the bar's transient info note. emitStatusUpdate
	// sets it only when a navigation-ish field (profile/bucket/prefix/
	// selection/pane) changed, so a background transfer finishing never
	// wipes a note the user is still reading. Set AFTER the dedup
	// snapshot, so it never participates in change detection.
	ClearInfo bool
}
