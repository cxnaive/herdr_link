package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cxnaive/herdr-desktop/internal/relay"
)

var errHistoryRestart = errors.New("历史版本或游标已变化")

type historyBookmark struct {
	Anchor      string
	All, Paused bool
}
type historyPage struct {
	Available    bool    `json:"available"`
	State        string  `json:"state"`
	Revision     string  `json:"source_revision"`
	Cursor       string  `json:"next_cursor"`
	ReasonCode   string  `json:"reason_code"`
	Reason       string  `json:"reason"`
	Entries      []entry `json:"entries"`
	Provisional  []entry `json:"provisional"`
	Subscription string  `json:"subscription_id"`
	Sequence     int64   `json:"sequence"`
	Total        *int    `json:"total"`
	Progress     struct {
		Source  int64 `json:"source_bytes"`
		Scanned int64 `json:"scanned_bytes"`
	} `json:"progress"`
	Diagnostics struct {
		Truncated       bool   `json:"source_truncated"`
		Incomplete      bool   `json:"continuation_incomplete"`
		Reason          string `json:"continuation_reason"`
		Corrupt         int    `json:"corrupt_records"`
		Oversized       int    `json:"oversized_records"`
		OmittedTools    int    `json:"omitted_tools"`
		OmittedPayloads int    `json:"omitted_payloads"`
	} `json:"diagnostics"`
}
type historyRecovery struct {
	anchor, cursor, expectedRevision   string
	all, older, observe, done, hasUser bool
	head                               *historyPage
	chunks                             [][]entry
	rows                               map[string]entry
	cursors                            map[string]bool
	base                               []entry
	pages, restarts                    int
	diagnosticMessage                  string
	fast                               bool
	restoreAnchor                      string
	restoreAll                         bool
}

func newHistoryRecovery(c *conversation, older, all bool) *historyRecovery {
	anchor := c.historyBookmark.Anchor
	if anchor == "" && len(c.entries) > 0 {
		anchor = c.entries[0].ID
	}
	r := &historyRecovery{anchor: anchor, all: all, older: older, observe: !older, expectedRevision: c.revision, rows: map[string]entry{}, cursors: map[string]bool{}}
	if older {
		r.cursor = c.next
		r.base = append([]entry{}, c.entries...)
	}
	return r
}
func (r *historyRecovery) request(agent relay.Agent) relay.Message {
	req := agent.Request("get_conversation_history")
	req["limit"] = 60
	req["cursor"] = r.cursor
	req["observe"] = r.observe
	return req
}
func decodeHistoryPage(data relay.Message) (historyPage, error) {
	var p historyPage
	b, err := json.Marshal(data)
	if err == nil {
		err = json.Unmarshal(b, &p)
	}
	if err != nil {
		return p, errors.New("历史消息格式无法解析")
	}
	return p, nil
}
func (r *historyRecovery) accept(p historyPage) (preparing bool, err error) {
	if p.State == "preparing" || p.State == "building" {
		if p.Cursor != "" {
			r.cursor = p.Cursor
		}
		return true, nil
	}
	if slices.Contains([]string{"cursor_expired", "invalid_cursor", "source_changed", "snapshot_expired"}, p.ReasonCode) {
		return false, errHistoryRestart
	}
	if !p.Available || p.State == "failed" {
		message := p.Reason
		if message == "" {
			message = "此 Agent 暂无结构化历史，可切换终端"
		}
		return false, errors.New(message)
	}
	if p.Revision == "" {
		return false, errors.New("历史消息缺少版本信息")
	}
	if r.head == nil {
		if r.older && p.Revision != r.expectedRevision {
			return false, errHistoryRestart
		}
		if r.fast && p.Revision != r.expectedRevision {
			r.fast = false
			r.base = nil
			r.anchor = r.restoreAnchor
			r.all = r.restoreAll
		}
	} else if p.Revision != r.head.Revision {
		return false, errHistoryRestart
	}
	if p.Cursor != "" && (r.cursors[p.Cursor] || r.pages > 0 && p.Cursor == r.cursor) {
		return false, errors.New("历史游标未前进，请刷新重试")
	}
	ids := map[string]bool{}
	newRows := 0
	for _, e := range p.Entries {
		if e.ID == "" || ids[e.ID] {
			return false, errors.New("历史消息身份重复或缺失")
		}
		ids[e.ID] = true
		if _, ok := r.rows[e.ID]; !ok {
			newRows++
		}
	}
	if newRows == 0 && p.Cursor != "" {
		return false, errors.New("历史分页未返回新内容，请刷新重试")
	}
	if r.head == nil {
		head := p
		r.head = &head
	}
	for _, e := range p.Entries {
		if _, ok := r.rows[e.ID]; !ok {
			r.rows[e.ID] = e
		}
		if e.Role == "user" {
			r.hasUser = true
		}
	}
	r.chunks = append(r.chunks, p.Entries)
	r.pages++
	r.cursor = p.Cursor
	r.observe = false
	if p.Cursor != "" {
		r.cursors[p.Cursor] = true
	}
	// The recent byte-window boundary is pagination, not lost source records.
	if notice := p.diagnosticNotice(); notice != "" {
		r.diagnosticMessage = notice
	}
	_, anchored := r.rows[r.anchor]
	r.done = p.Cursor == "" || r.older || !r.all && r.hasUser && (r.anchor == "" || anchored)
	return false, nil
}
func (r *historyRecovery) entries() []entry {
	// Newer pages (and the already-loaded tail for an older-page request)
	// retain the freshest version of overlapping tool results.
	values := make(map[string]entry, len(r.rows)+len(r.base))
	for id, e := range r.rows {
		values[id] = e
	}
	if r.older {
		for _, e := range r.base {
			values[e.ID] = e
		}
	}
	out := make([]entry, 0, len(values))
	seen := map[string]bool{}
	add := func(e entry) {
		if !seen[e.ID] {
			out = append(out, values[e.ID])
			seen[e.ID] = true
		}
	}
	for i := len(r.chunks) - 1; i >= 0; i-- {
		for _, e := range r.chunks[i] {
			add(e)
		}
	}
	if r.older {
		for _, e := range r.base {
			add(e)
		}
	} else if r.fast && r.cursor != "" {
		out = mergeHistory(r.base, out, false)
	}
	if !r.older && r.head != nil {
		for _, e := range out {
			seen[e.ID] = true
		}
		for _, e := range r.head.Provisional {
			if !seen[e.ID] {
				out = append(out, e)
				seen[e.ID] = true
			}
		}
	}
	return out
}
func (a *application) cancelHistoryLoad(c *conversation, manual bool) {
	if c.historyCancel != nil {
		c.historyCancel()
	}
	c.historyEpoch++
	c.loading = false
	c.preparing = false
	if manual {
		c.historyBookmark.Paused = true
		c.message = "已暂停加载，已读内容保留"
	}
}
func (a *application) loadHistory(older bool) {
	m, c := a.selected()
	if m == nil || c == nil || m.session == nil || c.loading || a.demo {
		return
	}
	if !c.agent.CanChat() {
		c.message = "Relay 尚未确认主会话，可切换终端直接操作。"
		return
	}
	if older && c.next == "" {
		return
	}
	c.historyBookmark.Paused = false
	c.historyWork = newHistoryRecovery(c, older, c.historyBookmark.All && !older)
	a.beginHistory(m, c)
}
func (a *application) loadAllHistory() {
	m, c := a.selected()
	if m == nil || c == nil || c.loading {
		return
	}
	c.historyBookmark.All = true
	c.historyBookmark.Paused = false
	c.lastCached = time.Time{}
	a.cacheHistory(m, c)
	a.loadHistory(false)
}
func (a *application) refreshRecentHistory(m *machine, c *conversation) {
	if !c.historySynchronized || len(c.entries) == 0 {
		a.loadHistory(false)
		return
	}
	c.historyWork = newRecentRecovery(c)
	a.beginHistory(m, c)
}
func newRecentRecovery(c *conversation) *historyRecovery {
	r := newHistoryRecovery(c, false, false)
	r.fast = true
	r.restoreAnchor = r.anchor
	r.restoreAll = c.historyBookmark.All
	for _, e := range c.entries {
		if !c.provisionalIDs[e.ID] {
			r.base = append(r.base, e)
			r.anchor = e.ID
		}
	}
	return r
}
func (a *application) resumeHistory() {
	m, c := a.selected()
	if m == nil || c == nil || m.session == nil || c.loading {
		return
	}
	c.historyBookmark.Paused = false
	if c.historyWork == nil {
		a.loadHistory(false)
		return
	}
	c.historyWork.restarts = 0
	a.beginHistory(m, c)
}
func (a *application) beginHistory(m *machine, c *conversation) {
	if !c.agent.CanChat() {
		return
	}
	if c.historyWork.older {
		if c.updateCancel != nil {
			c.updateCancel()
		}
	} else {
		a.stopConversationUpdates(m, c)
	}
	c.historyEpoch++
	c.loading = true
	c.message = ""
	c.historyOlder = c.historyWork.older
	c.historyProgress = 0
	c.historyPages = c.historyWork.pages
	c.historyCount = len(c.historyWork.rows)
	ctx, cancel := context.WithCancel(a.ctx)
	c.historyCancel = cancel
	a.readHistoryPage(ctx, m, c, c.historyWork, c.historyEpoch, m.epoch, c.agent)
}
func (a *application) readHistoryPage(ctx context.Context, m *machine, c *conversation, r *historyRecovery, requestEpoch, epoch uint64, agent relay.Agent) {
	if a.mode != 0 {
		a.cancelHistoryLoad(c, false)
		c.historyWork = nil
		return
	}
	if m.session == nil || m.epoch != epoch || ctx.Err() != nil {
		a.cancelHistoryLoad(c, false)
		return
	}
	s := m.session
	req := r.request(agent)
	go func() {
		readCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		data, err := s.Request(readCtx, req)
		a.update(func() {
			if c.historyEpoch != requestEpoch || c.historyWork != r || ctx.Err() != nil {
				return
			}
			if _, ok := exactAgent(m, agent); !ok || m.epoch != epoch {
				a.cancelHistoryLoad(c, false)
				c.historyWork = nil
				return
			}
			fail := func(err error) {
				c.loading = false
				c.preparing = false
				c.resumeOlder = c.resumeOlder || r.older
				c.message = err.Error() + "；已读内容保留，可继续加载。"
				if c.historyCancel != nil {
					c.historyCancel()
				}
			}
			if err != nil {
				fail(err)
				return
			}
			p, err := decodeHistoryPage(data)
			if err != nil {
				fail(err)
				return
			}
			preparing, err := r.accept(p)
			if errors.Is(err, errHistoryRestart) && r.restarts < 3 {
				next := newHistoryRecovery(c, false, r.all || c.historyBookmark.All)
				next.restarts = r.restarts + 1
				c.resumeOlder = c.resumeOlder || r.older
				c.historyWork = next
				c.message = "历史已变化，正在重新连接已读位置…"
				a.readHistoryPage(ctx, m, c, next, requestEpoch, epoch, agent)
				return
			}
			if err != nil {
				fail(err)
				return
			}
			c.preparing = preparing
			c.historyPages = r.pages
			c.historyCount = len(r.rows)
			c.historyProgress = 0
			if preparing {
				if p.Progress.Source > 0 {
					c.historyProgress = min(1, float64(p.Progress.Scanned)/float64(p.Progress.Source))
				}
				time.AfterFunc(800*time.Millisecond, func() {
					a.update(func() {
						if ctx.Err() == nil && c.historyEpoch == requestEpoch && c.historyWork == r {
							a.readHistoryPage(ctx, m, c, r, requestEpoch, epoch, agent)
						}
					})
				})
				return
			}
			if !r.done {
				a.readHistoryPage(ctx, m, c, r, requestEpoch, epoch, agent)
				return
			}
			c.applyHistoryEntries(r.entries(), r.older)
			c.revision = r.head.Revision
			if !r.fast || r.cursor == "" {
				c.next = r.cursor
			}
			c.loading = false
			c.preparing = false
			c.historyWork = nil
			c.message = r.diagnosticMessage
			if len(c.entries) > 0 {
				c.historyBookmark.Anchor = c.entries[0].ID
			}
			c.historyBookmark.Paused = false
			c.historySynchronized = true
			if !r.older {
				c.provisionalIDs = map[string]bool{}
				for _, e := range r.head.Provisional {
					c.provisionalIDs[e.ID] = true
				}
				c.subscription = r.head.Subscription
				c.sequence = r.head.Sequence
			}
			c.lastCached = time.Time{}
			a.cacheHistory(m, c)
			if c.historyCancel != nil {
				c.historyCancel()
			}
			continueOlder := c.resumeOlder && !r.older
			c.resumeOlder = false
			if continueOlder && c.next != "" {
				a.loadHistory(true)
			}
		})
	}()
}
func (p historyPage) diagnosticNotice() string {
	d := p.Diagnostics
	if d.Incomplete {
		message := "部分续接记录暂不可用，可稍后刷新。"
		if d.Reason != "" {
			message += "\n" + d.Reason
		}
		return message
	}
	if d.Corrupt > 0 {
		return fmt.Sprintf("日志中有 %d 条记录无法解析，其余消息已加载。", d.Corrupt)
	}
	if d.Oversized > 0 || d.OmittedTools > 0 || d.OmittedPayloads > 0 {
		return "部分过大的日志内容或工具详情未返回，其余消息已加载。"
	}
	return ""
}
func (c *conversation) historyLabel() string {
	if c.preparing {
		return "正在整理历史…"
	}
	if c.historyPages > 0 {
		return fmt.Sprintf("正在恢复历史 · %d 页 · %d 条", c.historyPages, c.historyCount)
	}
	if c.historyOlder {
		return "正在加载更早消息…"
	}
	return "正在同步…"
}

func (c *conversation) applyHistoryEntries(entries []entry, older bool) {
	c.entries = entries
	// Only explicit backward paging suppresses FollowEnd. A recent refresh
	// follows the tail unless the reader already scrolled away from it.
	if older {
		c.list.HoldPlace()
	}
}
