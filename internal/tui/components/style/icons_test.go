package style

import "testing"

// withNerdFont enables icons for one test and restores the previous state.
func withNerdFont(t *testing.T, v bool) {
	t.Helper()
	prev := nerdFont
	SetNerdFont(v)
	t.Cleanup(func() { SetNerdFont(prev) })
}

func TestIconForDisabledReturnsEmpty(t *testing.T) {
	withNerdFont(t, false)
	if g, c := IconFor("main.go", false, false); g != "" || c != nil {
		t.Errorf("disabled IconFor = (%q, %v), want empty", g, c)
	}
	if g, _ := BucketIcon(); g != "" {
		t.Errorf("disabled BucketIcon = %q, want empty", g)
	}
}

func TestIconForLookups(t *testing.T) {
	withNerdFont(t, true)

	// Exact filename (case-insensitive) wins over the extension.
	byName, _ := IconFor("Makefile", false, false)
	if byName != iconByName["makefile"].glyph {
		t.Errorf("Makefile glyph = %q", byName)
	}
	if g, _ := IconFor("go.mod", false, false); g != iconByName["go.mod"].glyph {
		t.Errorf("go.mod glyph = %q", g)
	}

	// Extension lookup, including for full key paths.
	goGlyph := iconByExt["go"].glyph
	if g, _ := IconFor("main.go", false, false); g != goGlyph {
		t.Errorf("main.go glyph = %q, want %q", g, goGlyph)
	}
	if g, _ := IconFor("some/prefix/tool.PY", false, false); g != iconByExt["py"].glyph {
		t.Errorf("tool.PY glyph = %q", g)
	}

	// Fallbacks: generic file, dir, symlink.
	if g, _ := IconFor("no-extension", false, false); g != iconFile.glyph {
		t.Errorf("generic file glyph = %q", g)
	}
	if g, _ := IconFor("photos/", true, false); g != iconDir.glyph {
		t.Errorf("dir glyph = %q", g)
	}
	if g, _ := IconFor("link", false, true); g != iconSymlink.glyph {
		t.Errorf("symlink glyph = %q", g)
	}
	// A symlink-to-dir keeps the symlink glyph.
	if g, _ := IconFor("linkdir/", true, true); g != iconSymlink.glyph {
		t.Errorf("symlink-to-dir glyph = %q", g)
	}

	if g, c := BucketIcon(); g != iconBucket.glyph || c == nil {
		t.Errorf("BucketIcon = (%q, %v)", g, c)
	}

	// Every icon returns a color when enabled.
	if _, c := IconFor("main.go", false, false); c == nil {
		t.Error("enabled IconFor should return a color")
	}
}
