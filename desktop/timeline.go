package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

const chatContentWidth = 800
const chatGutter = 24

type timelineTool struct {
	key   string
	value map[string]any
}
type renderedText struct{ source, rendered string }

func (c *conversation) markdownText(id, src string) string {
	if c.markdownCache == nil || len(c.markdownCache) > 2200 {
		c.markdownCache = map[string]renderedText{}
	}
	if previous, ok := c.markdownCache[id]; ok && previous.source == src {
		return previous.rendered
	}
	value := citationMarkdown(src)
	c.markdownCache[id] = renderedText{src, value}
	return value
}

type timelineBlock struct {
	key     string
	message *entry
	tools   []timelineTool
	images  []imageReference
}

func conversationBlocks(entries []entry) []timelineBlock {
	blocks := []timelineBlock{}
	pending := timelineBlock{}
	flush := func() {
		if len(pending.tools) > 0 {
			blocks = append(blocks, pending)
			pending = timelineBlock{}
		}
	}
	for i := range entries {
		e := &entries[i]
		boundary := e.Role == "user" || strings.TrimSpace(e.Text) != "" || len(e.Tools) == 0
		if boundary {
			flush()
			if e.Text != "" || len(e.Images) > 0 || e.Role == "user" {
				blocks = append(blocks, timelineBlock{key: "message:" + e.ID, message: e})
			}
		}
		for j, tool := range e.Tools {
			id, _ := tool["id"].(string)
			if id == "" {
				id = fmt.Sprint(j)
			}
			key := e.ID + ":tool:" + id
			if pending.key == "" {
				pending.key = "tools:" + key
			}
			pending.tools = append(pending.tools, timelineTool{key, tool})
		}
		if len(e.Tools) > 0 {
			pending.images = append(pending.images, entryImages(*e, true)...)
		}
	}
	flush()
	return blocks
}
func toolValue(v any) string {
	if s, ok := v.(string); ok {
		var parsed any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			b, _ := json.MarshalIndent(parsed, "", "  ")
			return string(b)
		}
		return s
	}
	if v == nil {
		return ""
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
func (a *application) timelineView(c *ui.Context, m *machine, chat *conversation) {
	a.configureChatMarkdown(m, chat)
	blocks := conversationBlocks(chat.entries)
	if query := strings.ToLower(strings.TrimSpace(chat.search)); query != "" {
		filtered := []timelineBlock{}
		for _, block := range blocks {
			content := ""
			if block.message != nil {
				content = block.message.Text
			}
			for _, tool := range block.tools {
				content += toolValue(tool.value)
			}
			if strings.Contains(strings.ToLower(content), query) {
				filtered = append(filtered, block)
			}
		}
		blocks = filtered
	}
	chat.list.Key = func(i int) any { return blocks[i].key }
	chat.list.FollowEnd = chat.search == ""
	ui.List(c.Key(m.config.ID+chat.agent.Identity()), &chat.list, len(blocks), func(i int) {
		block := blocks[i]
		bc := c.Key(block.key)
		ui.Column(bc).MaxWidth(chatContentWidth).FillWidth().AlignSelf(ui.Center).Gap(10).Children(func() {
			if e := block.message; e != nil {
				if e.Role == "user" {
					ui.Column(bc).FillWidth().AlignItems(ui.End).Gap(8).Children(func() {
						if e.Text != "" {
							ui.Text(bc, e.Text).Selectable().MaxWidthPercent(85).Padding(12, 16).Radius(12).Background(c.Theme().Surface)
						}
						a.conversationImagesView(bc, m, chat, entryImages(*e, false))
					})
				} else {
					ui.Text(bc, chat.agent.Provider()).FontSize(12).TextColor(c.Theme().TextMuted)
					if e.Text != "" {
						a.markdownView(bc, chat.markdownText(e.ID, e.Text), chat.md)
					}
					a.conversationImagesView(bc, m, chat, entryImages(*e, false))
				}
				return
			}
			if chat.folds[block.key] == nil {
				chat.folds[block.key] = new(bool)
			}
			ui.Accordion(bc, func() {
				if ui.AccordionItem(bc, fmt.Sprintf("%d 项工具调用", len(block.tools)), chat.folds[block.key], func() {
					ui.Accordion(bc.Key("tools"), func() {
						for _, tool := range block.tools {
							tc := bc.Key(tool.key)
							if chat.folds[tool.key] == nil {
								chat.folds[tool.key] = new(bool)
							}
							name, _ := tool.value["name"].(string)
							if name == "" {
								name = "工具详情"
							}
							if ui.AccordionItem(tc, name, chat.folds[tool.key], func() {
								state, _ := tool.value["state"].(string)
								if state != "" {
									ui.Text(tc, statusLabel(state)).FontSize(11).TextColor(c.Theme().TextMuted)
								}
								for _, field := range []struct{ key, label string }{{"input", "输入"}, {"output", "输出"}} {
									value := toolValue(tool.value[field.key])
									if value == "" {
										continue
									}
									fc := tc.Key(field.key)
									ui.Text(fc, field.label).Bold().FontSize(11).TextColor(c.Theme().TextMuted)
									ui.Text(fc, value).Font("monospace").FontSize(12).Selectable()
								}
							}).Changed() {
								chat.list.HoldPlace()
							}
						}
					})
				}).Changed() {
					chat.list.HoldPlace()
				}
			})
			a.conversationImagesView(bc, m, chat, block.images)
		})
	}).Grow(1).Padding(14, chatGutter).Gap(20).Children(func() {
		if len(blocks) == 0 {
			if chat.loading || chat.preparing {
				ui.Row(c).Gap(10).Children(func() { ui.Spinner(c).Size(20, 20); ui.Text(c, "正在加载对话…").TextColor(c.Theme().TextMuted) })
			} else {
				ui.Text(c, "暂无消息").TextColor(c.Theme().TextMuted)
			}
		}
	})
}
