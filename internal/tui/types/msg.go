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
	// callback. Bytes/Total are the sync's AGGREGATE byte progress over
	// the whole plan (deletes excluded) once the plan is known; before
	// planning finishes they are the last raw per-file pair (Total 0 for
	// deletes, -1 when unknown).
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
// navigation context, the focused pane and the multi-select count. The
// TUI's Update emits this after dispatching to the active list so the bar
// always reflects the post-update state; it is deduplicated against the
// previous emission, so every field except ClearInfo participates in
// change detection. Transfer state deliberately does NOT travel here: it
// changes on every 200ms tick, which would either defeat the dedup (an
// infinite self-perpetuating emit loop) or go stale — the bar pulls a
// TransferStats snapshot on the render path instead.
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
	// ClearInfo dismisses the bar's transient info note. emitStatusUpdate
	// sets it only when a navigation-ish field (profile/bucket/prefix/
	// selection/pane) changed, so a background transfer finishing never
	// wipes a note the user is still reading. Set AFTER the dedup
	// snapshot, so it never participates in change detection.
	ClearInfo bool
}

// InfoMsg sets the status bar's transient info note from a component's
// tea.Cmd (e.g. the o/O sort keys announcing the new sort mode). It keeps
// the note's existing lifecycle: the next navigation-ish StatusUpdateMsg
// (ClearInfo) dismisses it.
type InfoMsg struct {
	Text string
}

// TransferStats is the snapshot the status bar's transfer segment renders
// from. tui.go pulls it from transferpanel.Stats() on the RENDER path
// (composeView), never through the deduped StatusUpdateMsg — the byte
// counters move on every 200ms tick, and every Update pass ends in a
// render, so the segment stays live without any extra message traffic.
type TransferStats struct {
	// Active upload/download rows (queued or running). The progress bar
	// and the per-direction batch counts render while either is > 0.
	UpActive   int
	DownActive int
	// Current-batch tallies: total counts every upload/download queued
	// since the batch began (a batch starts when a transfer is added
	// while none was active), done counts the completed ones. Failed and
	// canceled rows leave the batch total.
	UpDone    int
	UpTotal   int
	DownDone  int
	DownTotal int
	// Aggregate byte progress over the current batch's upload/download
	// rows whose total is known: live bytes from the active rows plus the
	// folded final bytes of rows already terminal, so the bar never moves
	// backwards when one transfer of a burst completes. BytesTotal == 0
	// means no such row knows its total (render an indeterminate bar).
	BytesDone  int64
	BytesTotal int64
	// Lifetime completed-transfer counts, accumulated over the whole
	// program run and never reset (they survive the panel's row pruning).
	LifetimeUp   int
	LifetimeDown int
	// Failed counts the panel's failed+canceled rows (any op), rendered
	// as the ✗ tally.
	Failed int
	// Frame is the panel's tick frame, driving the indeterminate bounce.
	Frame int
}
