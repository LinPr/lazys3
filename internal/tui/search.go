package tui

// Recursive search within a bucket: '/' filters the CURRENT listing only,
// which is useless when the object you want is a few prefixes down. The
// search flow lets the user type a substring, walks the whole bucket
// recursively, and jumps straight to the first hit.

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// searchTimeout bounds a recursive search; a bucket with millions of keys
// must not hang the UI thread's follow-up work.
const searchTimeout = 60 * time.Second

// searchResultLimit caps how many hits are listed: the overlay is a picker,
// not a report, and a broad term on a large bucket would otherwise produce
// an unusable wall of rows.
const searchResultLimit = 200

// searchResultsMsg carries the recursive listing back to the live model.
type searchResultsMsg struct {
	Term   string
	Bucket string
	// Hits are the matching keys, sorted.
	Hits []string
	// Truncated reports that more keys matched than the limit.
	Truncated bool
	Err       error
}

// promptSearch opens the recursive-search input for the current bucket.
// The search runs from the bucket root, not the current prefix: the point
// is to find objects the user cannot currently see.
func (m *Model) promptSearch() tea.Cmd {
	if m.state != state.ActiveObjectList || m.selectedBucket == "" {
		m.statusBar.SetInfo("search: open a bucket first")
		return nil
	}
	bucket := m.selectedBucket
	m.modal.Show(
		fmt.Sprintf("Search s3://%s (substring of the key)", bucket),
		"",
		func(term string) tea.Cmd {
			term = strings.TrimSpace(term)
			if term == "" {
				return errCmd(fmt.Errorf("search: empty term"))
			}
			conn := m.syncConnParams()
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
				defer cancel()
				cli, err := s3store.NewS3Client(ctx, syncS3Option(conn))
				if err != nil {
					return searchResultsMsg{Term: term, Bucket: bucket, Err: err}
				}
				// "" lists the whole bucket: the search is meant to find
				// objects the current listing cannot show.
				keys, err := cli.ListAllKeys(ctx, bucket, "")
				if err != nil {
					return searchResultsMsg{Term: term, Bucket: bucket, Err: err}
				}
				needle := strings.ToLower(term)
				hits := make([]string, 0, 32)
				truncated := false
				for _, key := range keys {
					if !strings.Contains(strings.ToLower(key), needle) {
						continue
					}
					if len(hits) >= searchResultLimit {
						truncated = true
						break
					}
					hits = append(hits, key)
				}
				sort.Strings(hits)
				return searchResultsMsg{Term: term, Bucket: bucket, Hits: hits, Truncated: truncated}
			}
		},
	)
	return nil
}

// directoryOf returns the "/"-terminated directory part of an S3 key ("" at
// the bucket root), which is what a search hit needs to reconstruct its
// absolute location.
func directoryOf(key string) string {
	dir := path.Dir(key)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir + "/"
}

// handleSearchResults shows the hits in the confirm-style modal and jumps to
// the picked object's prefix. With no hits it reports on the status bar so
// the user is not left staring at an empty modal.
func (m *Model) handleSearchResults(msg searchResultsMsg) tea.Cmd {
	if msg.Err != nil {
		return errCmd(fmt.Errorf("search: %w", msg.Err))
	}
	if len(msg.Hits) == 0 {
		m.statusBar.SetInfo(fmt.Sprintf("search %q: no match in s3://%s", msg.Term, msg.Bucket))
		return nil
	}
	// Jumping to the FIRST hit is the useful default: the user can then move
	// with j/k in the listing. Listing picks in a modal would need a new
	// scrollable picker component for marginal benefit.
	first := msg.Hits[0]
	loc := StartLocation{Bucket: msg.Bucket, Prefix: directoryOf(first)}
	note := fmt.Sprintf("search %q: %d match(es), jumping to %s", msg.Term, len(msg.Hits), first)
	if msg.Truncated {
		note += fmt.Sprintf(" (matches capped at %d)", searchResultLimit)
	}
	m.objectlist.RememberPosition(m.selectedBucket, m.selectedObject)
	m.openStartLocation(loc)
	// The note is emitted as a message rather than set here: this navigation
	// bumps the status state, and emitStatusUpdate clears any info note on
	// a navigation change. Delivering it afterwards makes it stick.
	return tea.Batch(
		m.objectlist.Fetch(m.objectListOptionFromState()),
		func() tea.Msg { return types.InfoMsg{Text: note} },
	)
}
