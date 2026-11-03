// Startup locations: per-profile [profiles.<name>] entries in the lazys3
// config and the optional s3://bucket/prefix positional argument open a
// remote listing directly, without ever calling ListBuckets. This matters
// for bucket-scoped credentials (s3:ListBucket + object permissions only):
// such policies cannot grant s3:ListAllMyBuckets, so the bucket picker is
// useless there and the user would otherwise have to jump with 'g' on
// every launch.

package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/profilelist"
	"github.com/LinPr/lazys3/internal/tui/state"
)

// StartLocation is a remote location to open on startup. Prefix is a
// normalized listing prefix ("" for the bucket root, otherwise
// "/"-terminated).
type StartLocation struct {
	Bucket string
	Prefix string
}

// ParseStartLocation parses "s3://bucket[/prefix]". The prefix is
// normalized like a goto target: leading/trailing slashes are dropped and
// a non-empty result is "/"-terminated. A bare bucket selects the root.
func ParseStartLocation(s string) (StartLocation, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "s3://") {
		return StartLocation{}, fmt.Errorf("start location %q: want s3://bucket[/prefix/]", s)
	}
	rest := strings.TrimPrefix(s, "s3://")
	bucket, prefix, _ := strings.Cut(rest, "/")
	if bucket == "" {
		return StartLocation{}, fmt.Errorf("start location %q: empty bucket", s)
	}
	if strings.ContainsAny(bucket, " \t") {
		return StartLocation{}, fmt.Errorf("start location %q: bucket %q contains whitespace", s, bucket)
	}
	return StartLocation{Bucket: bucket, Prefix: normalizePrefix(prefix)}, nil
}

// String renders the location as an s3:// URI, without a trailing slash on
// the bucket root.
func (l StartLocation) String() string {
	if l.Prefix == "" {
		return "s3://" + l.Bucket
	}
	return "s3://" + l.Bucket + "/" + l.Prefix
}

// startLocationFor returns the location to open for a freshly selected
// profile. A CLI positional argument wins over the config (and is consumed
// so a later profile switch falls back to that profile's own config).
func (m *Model) startLocationFor(profile string) (StartLocation, bool) {
	if m.cliStart != nil {
		loc := *m.cliStart
		m.cliStart = nil
		return loc, true
	}
	p, ok := m.profileStarts[profile]
	if !ok || p.Bucket == "" {
		return StartLocation{}, false
	}
	return StartLocation{Bucket: p.Bucket, Prefix: p.Prefix}, true
}

// openStartLocation switches the model to the object list at the given
// location and arms the listing fetch. It performs no network I/O itself;
// the caller returns the tea.Cmd from objectlist.Fetch.
func (m *Model) openStartLocation(loc StartLocation) {
	m.selectedBucket = loc.Bucket
	m.selectedObject = loc.Prefix
	m.state = state.ActiveObjectList
	m.objectlist.RestorePosition(loc.Bucket, loc.Prefix)
	m.objectlist.SetObjects(nil)
	m.objectlist.SetTitle(loc.String())
}

// autoSelectProfile handles the --profile flag once the shared config has
// been read: it moves the picker's cursor to that profile and runs the
// normal forward navigation (which applies the start location, if any).
func (m *Model) autoSelectProfile(res profilelist.ReadAwsConfigResult) tea.Cmd {
	name := m.startProfile
	m.startProfile = ""
	if res.Err != nil {
		return errCmd(fmt.Errorf("--profile %s: %w", name, res.Err))
	}
	if !m.profileList.SelectByName(name) {
		return errCmd(fmt.Errorf("--profile %q: no such profile in the AWS shared config", name))
	}
	return m.handleForward(nil)
}

// ensureBucketListLoaded fetches the bucket list on first use. Profiles
// with a configured start location skip the ListBuckets call entirely, so
// the bucket list stays unloaded until the user actually backs out to it;
// only then is the (possibly denied) call made.
func (m *Model) ensureBucketListLoaded() tea.Cmd {
	if m.bucketList.Fetched() || m.bucketList.Loading() || m.bucketList.Option == nil {
		return nil
	}
	opt := m.bucketListOptionFromState()
	m.bucketList.SetLoading(true)
	return bucketlist.FetchBucketListCmd(&opt)
}
