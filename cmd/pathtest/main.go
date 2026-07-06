// Command pathtest renders the objectlist component outside a terminal
// event loop, for eyeballing layout changes. Rendering with real objects
// is covered in-package by objectlist's render tests; this stub only
// exercises the empty-state view.
package main

import (
	"fmt"

	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
)

func main() {
	m := objectlist.NewModel()
	m.SetSize(80, 20)
	m.SetObjects(nil)
	fmt.Print(m.View())
}
