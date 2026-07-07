// The 'g' go-to-path flow: a modal that jumps the focused pane straight to
// a location without walking the tree. Remote grammar (object/bucket
// lists): a full s3://bucket/prefix/ URI switches bucket within the current
// profile, a leading '/' resolves from the current bucket's root, anything
// else is relative to the current prefix; '..' segments resolve within the
// prefix and never climb above the bucket root. An empty listing is a valid
// destination — S3 prefixes are virtual, so no existence check is made.
// The local pane accepts absolute, ~-expanded, or relative-to-current
// paths; a nonexistent or non-directory target surfaces on the status bar
// and the pane keeps its listing (locallist's generation-guarded fetch).
//
// Like every modal flow, onConfirm runs against a stale captured model, so
// the confirm emits a goto*Msg that tui.go routes back here on the live
// model.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/state"
)

// gotoRemoteMsg carries the confirmed remote goto input back to the live
// model (parsed there, where the current bucket/prefix are authoritative).
type gotoRemoteMsg struct{ input string }

// gotoLocalMsg carries the confirmed local goto path back to the live model.
type gotoLocalMsg struct{ path string }

// gotoRemoteTitle documents the accepted grammar in the modal title. The
// bucket list has no current location to resolve from, so its title only
// advertises the URI form.
const (
	gotoRemoteTitle     = "Go to (s3://bucket/prefix/ | /from-root | relative/)"
	gotoBucketListTitle = "Go to (s3://bucket/prefix/)"
)

// handleGotoKey ('g' outside overlays and filters) opens the go-to modal
// for the focused pane. The profile list has no location to jump within, so
// it only hints.
func (m *Model) handleGotoKey() tea.Cmd {
	if m.localFocused() {
		dir := m.localList.Dir()
		if dir == "" {
			dir = "~"
		}
		m.modal.Show(
			"Go to directory",
			dir,
			func(p string) tea.Cmd {
				return func() tea.Msg { return gotoLocalMsg{path: p} }
			},
		)
		return nil
	}
	switch m.state {
	case state.ActiveProfileList:
		m.statusBar.SetInfo("go to: open a bucket first")
		return nil
	case state.ActiveBucketList, state.ActiveObjectList:
		title := gotoRemoteTitle
		placeholder := m.remoteLocation()
		if placeholder == "" {
			// Bucket list: prefill the highlighted bucket's URI so a bare
			// enter jumps into it; only full s3:// URIs work here.
			title = gotoBucketListTitle
			placeholder = "s3://"
			if b := m.bucketList.GetSelectedBucket(); b != nil {
				placeholder = "s3://" + b.Title() + "/"
			}
		}
		m.modal.Show(
			title,
			placeholder,
			func(input string) tea.Cmd {
				return func() tea.Msg { return gotoRemoteMsg{input: input} }
			},
		)
	}
	return nil
}

// normalizePrefix canonicalizes a raw prefix: empty/'.' segments collapse,
// '..' pops one segment (clamped at the bucket root), and any non-empty
// result is '/'-terminated so it lists as a prefix.
func normalizePrefix(p string) string {
	var segs []string
	for _, s := range strings.Split(p, "/") {
		switch s {
		case "", ".":
		case "..":
			if len(segs) > 0 {
				segs = segs[:len(segs)-1]
			}
		default:
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		return ""
	}
	return strings.Join(segs, "/") + "/"
}

// parseGotoTarget resolves the goto input against the current location and
// returns the target bucket and normalized prefix ("" = bucket root).
func parseGotoTarget(input, curBucket, curPrefix string) (bucket, prefix string, err error) {
	input = strings.TrimSpace(input)
	switch {
	case strings.HasPrefix(input, "s3://"):
		rest := strings.TrimPrefix(input, "s3://")
		bucket, rest, _ = strings.Cut(rest, "/")
		if bucket == "" {
			return "", "", fmt.Errorf("go to: empty bucket in %q", input)
		}
		return bucket, normalizePrefix(rest), nil
	case curBucket == "":
		return "", "", fmt.Errorf("go to: type a full s3://bucket/prefix/ URI (or open a bucket first)")
	case strings.HasPrefix(input, "/"):
		return curBucket, normalizePrefix(input), nil
	default:
		return curBucket, normalizePrefix(curPrefix + "/" + input), nil
	}
}

// handleGotoRemote parses the confirmed input on the live model and jumps
// the remote pane there: the outgoing location's cursor is memoised, the
// selected bucket/prefix switch, and the listing is re-fetched through the
// same option plumbing every navigation uses.
func (m *Model) handleGotoRemote(input string) tea.Cmd {
	// The bucket list has no current location: selectedBucket may still
	// hold a previously-visited bucket (handleBackward keeps it), so
	// resolving relative/rooted input there would silently jump into the
	// stale bucket. Blank it so only full s3:// URIs parse, as documented.
	curBucket, curPrefix := m.selectedBucket, m.selectedObject
	if m.state == state.ActiveBucketList {
		curBucket, curPrefix = "", ""
	}
	bucket, prefix, err := parseGotoTarget(input, curBucket, curPrefix)
	if err != nil {
		return errCmd(err)
	}
	// Memoise the cursor of the location being left (object list only; the
	// bucket list has no prefix position to remember).
	if m.state == state.ActiveObjectList && m.selectedBucket != "" {
		m.objectlist.RememberPosition(m.selectedBucket, m.selectedObject)
	}
	m.selectedBucket = bucket
	m.selectedObject = prefix
	m.state = state.ActiveObjectList
	m.objectlist.RestorePosition(bucket, prefix)
	m.objectlist.SetObjects([]objectlist.Object{})

	s3uri := fmt.Sprintf("s3://%s", bucket)
	if prefix != "" {
		s3uri = fmt.Sprintf("s3://%s/%s", bucket, prefix)
	}
	m.objectlist.SetTitle(s3uri)
	return m.objectlist.Fetch(m.objectListOptionFromState())
}

// handleGotoLocal expands and resolves the confirmed path on the live model
// and routes it through locallist's generation-guarded fetch: a nonexistent
// or non-directory target surfaces as a status-bar error and the pane keeps
// its current listing.
func (m *Model) handleGotoLocal(p string) tea.Cmd {
	p = strings.TrimSpace(p)
	if p == "" {
		return nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return errCmd(fmt.Errorf("go to %s: %w", p, err))
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		if dir := m.localList.Dir(); dir != "" {
			p = filepath.Join(dir, p)
		} else if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
	}
	return m.localList.GoTo(filepath.Clean(p))
}
