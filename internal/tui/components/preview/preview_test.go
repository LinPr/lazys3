package preview

import (
	"strings"
	"testing"
	"time"
)

type fakeItem struct {
	content string
	req     *PreviewRequest
}

func (f fakeItem) FilterValue() string                { return f.content }
func (f fakeItem) GetPreviewContent() string          { return f.content }
func (f fakeItem) GetPreviewRequest() *PreviewRequest { return f.req }

func objectReq(key string) *PreviewRequest {
	return &PreviewRequest{
		Kind:    "object",
		Profile: "default",
		Bucket:  "bkt",
		Key:     key,
		ModTime: time.Now(),
	}
}

func TestUpdateDropsStalePreviewContentMsg(t *testing.T) {
	pm := NewPreviewModel()
	pm.Show()

	staleReq := objectReq("old.txt")
	if cmd := pm.SetContent(fakeItem{content: "old fallback", req: staleReq}); cmd == nil {
		t.Fatal("expected fetch cmd for object item")
	}
	staleToken := requestToken(staleReq)

	newReq := objectReq("new.txt")
	if cmd := pm.SetContent(fakeItem{content: "new fallback", req: newReq}); cmd == nil {
		t.Fatal("expected fetch cmd for object item")
	}

	// The stale fetch completes last; its content must be dropped.
	model, _ := pm.Update(PreviewContentMsg{Token: staleToken, Content: "old live content"})
	pm = model.(Model)
	if pm.content != "new fallback" {
		t.Fatalf("stale msg overwrote content: got %q", pm.content)
	}
	if !pm.loading {
		t.Fatal("stale msg cleared the loading flag")
	}

	// The matching fetch is applied.
	model, _ = pm.Update(PreviewContentMsg{Token: requestToken(newReq), Content: "new live content"})
	pm = model.(Model)
	if pm.content != "new live content" {
		t.Fatalf("matching msg not applied: got %q", pm.content)
	}
	if pm.loading {
		t.Fatal("matching msg did not clear the loading flag")
	}
}

func TestUpdateDropsStaleMsgAfterSyncOnlyItem(t *testing.T) {
	pm := NewPreviewModel()
	pm.Show()

	staleReq := objectReq("old.txt")
	pm.SetContent(fakeItem{content: "old fallback", req: staleReq})
	// Move to a synchronous-only item (nil request, e.g. a Profile).
	if cmd := pm.SetContent(fakeItem{content: "profile content"}); cmd != nil {
		t.Fatal("expected nil cmd for sync-only item")
	}

	model, _ := pm.Update(PreviewContentMsg{Token: requestToken(staleReq), Content: "old live content"})
	pm = model.(Model)
	if pm.content != "profile content" {
		t.Fatalf("stale msg overwrote sync-only content: got %q", pm.content)
	}
}

func TestRequestTokenDistinguishesRequests(t *testing.T) {
	a := requestToken(objectReq("a.txt"))
	b := requestToken(objectReq("b.txt"))
	if a == b {
		t.Fatal("tokens for different keys must differ")
	}
	if a != requestToken(objectReq("a.txt")) {
		t.Fatal("token must be stable for the same request")
	}
	if strings.TrimSpace(a) == "" {
		t.Fatal("token must be non-empty")
	}
}
