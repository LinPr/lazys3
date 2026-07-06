package objectlist

import (
	"sort"
	"strings"
)

// sortField selects which Object attribute orders the listing.
type sortField int

const (
	sortByName sortField = iota
	sortBySize
	sortByTime
	sortFieldCount
)

func (f sortField) String() string {
	switch f {
	case sortBySize:
		return "size"
	case sortByTime:
		return "time"
	default:
		return "name"
	}
}

// sortObjects orders the slice for display: directories always come first,
// then files; within each group items are ordered by the given field, with
// case-insensitive name as tiebreak. desc reverses the field comparison but
// keeps directories on top.
func sortObjects(items []Object, field sortField, desc bool) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.isDir != b.isDir {
			return a.isDir
		}
		less, equal := compareBy(a, b, field)
		if equal {
			return byName(a, b, desc)
		}
		if desc {
			return !less
		}
		return less
	})
}

func compareBy(a, b Object, field sortField) (less, equal bool) {
	switch field {
	case sortBySize:
		return a.size < b.size, a.size == b.size
	case sortByTime:
		return a.modTime.Before(b.modTime), a.modTime.Equal(b.modTime)
	default:
		an, bn := strings.ToLower(a.name), strings.ToLower(b.name)
		return an < bn, an == bn
	}
}

func byName(a, b Object, desc bool) bool {
	an, bn := strings.ToLower(a.name), strings.ToLower(b.name)
	if desc {
		return an > bn
	}
	return an < bn
}
