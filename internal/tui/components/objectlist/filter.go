package objectlist

import (
	"charm.land/bubbles/v2/list"
	"github.com/LinPr/lazys3/internal/tui/components/filter"
)

// substringFilter is the shared case-insensitive substring matcher (see
// components/filter). It replaces the default fuzzy matcher so `/foo`
// narrows to names actually containing "foo", which reads better for
// path-like keys.
var substringFilter list.FilterFunc = filter.Substring
