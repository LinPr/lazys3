package objectlist

import (
	"github.com/LinPr/lazys3/internal/tui/components/filter"
	"github.com/charmbracelet/bubbles/v2/list"
)

// substringFilter is the shared case-insensitive substring matcher (see
// components/filter). It replaces the default fuzzy matcher so `/foo`
// narrows to names actually containing "foo", which reads better for
// path-like keys.
var substringFilter list.FilterFunc = filter.Substring
