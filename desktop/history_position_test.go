package main

import (
	"strings"
	"testing"

	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo/ui"
)

func historyPositionFixture(t *testing.T, entries []entry) (*application, *machine, *conversation, *ui.Tester) {
	t.Helper()
	a, m, c := managementFixture(t)
	c.entries = entries
	c.loading = len(entries) == 0
	c.list = ui.ListState{}
	tt := ui.NewTester(func(ctx *ui.Context) { a.timelineView(ctx, m, c) }, 900, 500)
	return a, m, c, tt
}

func assertHistoryEnd(t *testing.T, c *conversation) {
	t.Helper()
	_, last := c.list.Visible()
	want := len(conversationBlocks(c.entries)) - 1
	if !c.list.AtEnd() || last != want {
		t.Fatalf("history opened away from latest message: visible last=%d want=%d, atEnd=%v", last, want, c.list.AtEnd())
	}
}
func visibleHistoryKey(c *conversation) string {
	first, _ := c.list.Visible()
	blocks := conversationBlocks(c.entries)
	if first < 0 || first >= len(blocks) {
		return ""
	}
	return blocks[first].key
}

func TestHistoryFirstAsyncPageOpensAtLatestMessage(t *testing.T) {
	_, _, c, tt := historyPositionFixture(t, nil)
	c.applyHistoryEntries(historyEntries(100, 180), false)
	c.loading = false
	tt.Frame()
	assertHistoryEnd(t, c)
}

func TestHistoryCachedTailFollowsFreshTailAfterSynchronization(t *testing.T) {
	_, _, c, tt := historyPositionFixture(t, historyEntries(0, 80))
	assertHistoryEnd(t, c)
	c.applyHistoryEntries(historyEntries(0, 150), false)
	tt.Frame()
	assertHistoryEnd(t, c)
	// A normal refresh with a longer last message must keep following its bottom, too.
	rows := historyEntries(0, 150)
	rows[len(rows)-1].Text = "Long response\n\n" + strings.Repeat("A paragraph in the latest response.\n\n", 30)
	c.applyHistoryEntries(rows, false)
	tt.Frame()
	assertHistoryEnd(t, c)
}

func TestHistoryRefreshPreservesDeliberatelyScrolledReadingPosition(t *testing.T) {
	_, _, c, tt := historyPositionFixture(t, historyEntries(0, 150))
	c.list.ScrollTo(25, ui.Start)
	tt.Frame()
	before := visibleHistoryKey(c)
	if c.list.AtEnd() {
		t.Fatal("fixture did not scroll into history")
	}
	c.applyHistoryEntries(historyEntries(0, 180), false)
	tt.Frame()
	if got := visibleHistoryKey(c); got != before || c.list.AtEnd() {
		t.Fatalf("refresh interrupted reading: %s -> %s", before, got)
	}
}

func TestHistoryOlderPagesKeepTheVisibleMessage(t *testing.T) {
	for _, atEnd := range []bool{false, true} {
		t.Run(map[bool]string{false: "reading earlier", true: "reading latest"}[atEnd], func(t *testing.T) {
			end := 180
			_, _, c, tt := historyPositionFixture(t, historyEntries(100, end))
			if !atEnd {
				c.list.ScrollTo(15, ui.Start)
				tt.Frame()
			}
			before := visibleHistoryKey(c)
			c.applyHistoryEntries(historyEntries(0, end), true)
			tt.Frame()
			if got := visibleHistoryKey(c); got != before {
				t.Fatalf("older page displaced visible message: %s -> %s", before, got)
			}
		})
	}
}

func TestHistoryLiveBatchStillFollowsAfterInitialLoad(t *testing.T) {
	_, _, c, tt := historyPositionFixture(t, nil)
	c.applyHistoryEntries(historyEntries(0, 80), false)
	c.loading = false
	tt.Frame()
	c.subscription = "sub"
	c.revision = "r"
	c.sequence = 1
	if !c.applyBatch(relay.Message{"kind": "batch", "subscription_id": "sub", "source_revision": "r", "sequence": 2, "entries": historyEntries(80, 100)}) {
		t.Fatal("batch refused")
	}
	tt.Frame()
	assertHistoryEnd(t, c)
}

func TestHistoryJumpToLatestClearsSearchWithoutReloading(t *testing.T) {
	a, m, c := managementFixture(t)
	c.entries = historyEntries(0, 150)
	c.list = ui.ListState{}
	m.session = nil // Jumping within loaded history also works offline.
	tt := ui.NewTester(a.view, 1280, 840)
	assertHistoryEnd(t, c)
	c.list.ScrollTo(25, ui.Start)
	tt.Frame()
	if err := tt.Click("回到最新"); err != nil {
		t.Fatal(err)
	}
	assertHistoryEnd(t, c)
	if c.loading {
		t.Fatal("local jump started a network history load")
	}
	c.searchOpen = true
	c.search = "25"
	tt.Frame()
	if err := tt.Click("回到最新"); err != nil {
		t.Fatal(err)
	}
	if c.search != "" {
		t.Fatal("latest jump retained a filter hiding the latest message")
	}
	assertHistoryEnd(t, c)
}

func TestHistoryBackgroundRefreshDoesNotJumpWithinSearch(t *testing.T) {
	_, _, c, tt := historyPositionFixture(t, historyEntries(0, 150))
	c.search = "25"
	tt.Frame()
	c.applyHistoryEntries(historyEntries(0, 180), false)
	tt.Frame()
	first, last := c.list.Visible()
	if first != 0 || last != 1 || c.search != "25" {
		t.Fatalf("search disturbed by refresh: %d..%d, %q", first, last, c.search)
	}
}

func TestHistoryScrollWhileRefreshingOverridesTailFollowing(t *testing.T) {
	_, _, c, tt := historyPositionFixture(t, historyEntries(0, 150))
	assertHistoryEnd(t, c)
	c.loading = true
	tt.Scroll(450, 250, 0, -900)
	before := visibleHistoryKey(c)
	if c.list.AtEnd() {
		t.Fatal("scroll did not leave the tail")
	}
	c.applyHistoryEntries(historyEntries(0, 200), false)
	c.loading = false
	tt.Frame()
	if got := visibleHistoryKey(c); got != before || c.list.AtEnd() {
		t.Fatalf("late refresh pulled reader to tail: %s -> %s", before, got)
	}
}

func TestHistoryRestoredDepthDoesNotBecomeTheDefaultReadingPosition(t *testing.T) {
	a, m, c := managementFixture(t)
	key := m.config.ID + ":" + c.agent.Identity()
	if err := a.store.saveHistory(key, historyEntries(0, 900), "r", historyBookmark{Anchor: "entry-00000", All: true}); err != nil {
		t.Fatal(err)
	}
	delete(a.chats, key)
	_, c = a.selected()
	if len(c.entries) != 300 || c.historyBookmark.Anchor != "entry-00000" {
		t.Fatal("fixture failed to restore a trimmed cache with deep history")
	}
	tt := ui.NewTester(func(ctx *ui.Context) { a.timelineView(ctx, m, c) }, 900, 500)
	assertHistoryEnd(t, c)
	r := newHistoryRecovery(c, false, c.historyBookmark.All)
	for end := 1100; end > 0; {
		start := max(0, end-100)
		cursor := ""
		if start > 0 {
			cursor = historyEntries(start, start+1)[0].ID
		}
		if _, err := r.accept(historyPage{Available: true, Revision: "r", Cursor: cursor, Entries: historyEntries(start, end)}); err != nil {
			t.Fatal(err)
		}
		end = start
	}
	if !r.done || r.pages != 11 {
		t.Fatal("deep recovery did not finish")
	}
	c.applyHistoryEntries(r.entries(), false)
	tt.Frame()
	assertHistoryEnd(t, c)
	if c.entries[0].ID != "entry-00000" || c.entries[len(c.entries)-1].ID != "entry-01099" {
		t.Fatal("scroll fix lost history or reversed message order")
	}
}
