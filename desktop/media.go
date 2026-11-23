package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
	"golang.org/x/image/draw"
)

type imageReference struct{ source, label string }
type conversationImage struct {
	label   string
	bitmap  *ui.Bitmap
	data    []byte
	loading bool
	message string
}

func entryImages(e entry, toolsOnly bool) []imageReference {
	refs := []imageReference{}
	seen := map[string]bool{}
	add := func(source, label string) {
		if source == "" || seen[source] || len(refs) >= 8 {
			return
		}
		if !strings.HasPrefix(source, "data:image/") {
			ext := strings.ToLower(path.Ext(source))
			if !slices.Contains([]string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"}, ext) {
				return
			}
		}
		if label == "" {
			label = path.Base(source)
			if strings.HasPrefix(source, "data:") {
				label = "图像"
			}
		}
		seen[source] = true
		refs = append(refs, imageReference{source, label})
	}
	fromText := func(src string) {
		doc := parser.New(parser.Default)
		doc.Set(src)
		for i := 0; i < doc.Len(); i++ {
			in := doc.Inlines(i)
			for j, v := range in {
				if v.Kind == parser.Image {
					label := "图像"
					if j+1 < len(in) {
						label = in[j+1].Text
					}
					add(v.Dest, label)
				}
			}
		}
	}
	if !toolsOnly {
		fromText(e.Text)
		for _, value := range e.Images {
			switch v := value.(type) {
			case string:
				add(v, "")
			case map[string]any:
				source, _ := v["data_url"].(string)
				if source == "" {
					source, _ = v["path"].(string)
				}
				label, _ := v["name"].(string)
				add(source, label)
			}
		}
	}
	if toolsOnly {
		for _, tool := range e.Tools {
			output, _ := tool["output"].(string)
			fromText(output)
			input := tool["input"]
			if s, ok := input.(string); ok {
				var v map[string]any
				if json.Unmarshal([]byte(s), &v) == nil {
					input = v
				}
			}
			if v, ok := input.(map[string]any); ok {
				for _, key := range []string{"path", "file_path", "filePath", "image_path", "filename"} {
					s, _ := v[key].(string)
					add(s, "")
				}
			}
		}
	}
	return refs
}
func decodePreviewImage(data []byte) (*ui.Bitmap, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 24000000 {
		return nil, errors.New("图像暂无法预览")
	}
	return ui.DecodeBitmap(data)
}
func readImage(ctx context.Context, s relay.Requester, agent relay.Agent, base, source string, filesystem bool) ([]byte, *ui.Bitmap, error) {
	var data []byte
	if strings.HasPrefix(source, "data:image/") {
		if len(source) > 16*1024*1024 {
			return nil, nil, errors.New("图像过大")
		}
		header, body, ok := strings.Cut(source, ",")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return nil, nil, errors.New("图像格式无效")
		}
		var err error
		data, err = base64.StdEncoding.DecodeString(body)
		if err != nil {
			return nil, nil, err
		}
	} else {
		if s == nil {
			return nil, nil, errors.New("连接电脑后查看图像")
		}
		p, _, err := fileReference(agent.Cwd, base, source, filesystem)
		if err != nil {
			return nil, nil, err
		}
		doc, err := relay.ReadDocument(ctx, s, agent, p)
		if err != nil {
			return nil, nil, err
		}
		if doc.Kind != "image" {
			return nil, nil, errors.New("该文件不是图片")
		}
		data = doc.Data
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 24000000 {
		return nil, nil, errors.New("图像暂无法预览")
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	if max(cfg.Width, cfg.Height) > 1024 {
		scale := float64(1024) / float64(max(cfg.Width, cfg.Height))
		thumb := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(cfg.Width)*scale)), max(1, int(float64(cfg.Height)*scale))))
		draw.CatmullRom.Scale(thumb, thumb.Bounds(), im, im.Bounds(), draw.Src, nil)
		im = thumb
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return data, ui.NewBitmap(im), nil
}
func (a *application) openReference(m *machine, chat *conversation, base, dest string) {
	if strings.HasPrefix(dest, "#") {
		if chat.md != nil {
			chat.md.Anchor = dest
			chat.md.AnchorVersion++
		}
		return
	}
	u, err := url.Parse(dest)
	if err == nil && (u.Scheme == "https" || u.Scheme == "http" || u.Scheme == "mailto") {
		if !a.demo {
			go mygo.Shell.OpenExternal(dest)
		}
		return
	}
	p, anchor, err := fileReference(chat.agent.Cwd, base, dest, m.filesystemRead)
	if err != nil {
		a.status = err.Error()
		return
	}
	a.status = ""
	a.files = true
	a.details = false
	f := filesFor(chat)
	if p == "" || strings.HasSuffix(dest, "/") {
		a.loadDirectory(m, chat, p, false)
		return
	}
	f.path = relay.ParentDirectory(p)
	a.openFile(m, chat, p)
	f.md.Anchor = anchor
	f.md.AnchorVersion++
}
func (a *application) configureChatMarkdown(m *machine, chat *conversation) {
	if chat.md != nil && chat.mdTarget == chat.agent.Target && chat.mdEpoch == m.epoch {
		return
	}
	chat.mdTarget, chat.mdEpoch = chat.agent.Target, m.epoch
	chat.images = nil
	chat.md = &markdown.Options{WrapCode: true, OnLink: func(dest string) { a.openReference(m, chat, "", dest) }}
	chat.md.Block = standaloneLinkBlock(chat.md.OnLink)
	a.configureMath(chat.md)
}
func (a *application) configureFileMarkdown(m *machine, chat *conversation, f *fileBrowser) {
	base := f.previewPath
	s, agent := a.fileReader(m), f.agent
	f.md = &markdown.Options{Anchor: f.md.Anchor, AnchorVersion: f.md.AnchorVersion}
	f.md.OnLink = func(dest string) {
		if chat.files != f || m.fileEpoch != f.fileEpoch {
			return
		}
		if strings.HasPrefix(dest, "#") {
			f.md.Anchor = dest
			f.md.AnchorVersion++
			return
		}
		a.openReference(m, chat, base, dest)
	}
	f.md.WrapCode = true
	f.md.Block = standaloneLinkBlock(f.md.OnLink)
	a.configureMath(f.md)
	f.md.LoadImage = func(source string) (ui.ImageSource, error) {
		ctx, cancel := context.WithTimeout(s.Context, 30*time.Second)
		defer cancel()
		_, bitmap, err := readImage(ctx, s, agent, base, source, s.Filesystem)
		return bitmap, err
	}
}

func standaloneLinkBlock(open func(string)) func(*ui.Context, *parser.Document, int) bool {
	return func(c *ui.Context, d *parser.Document, i int) bool {
		if d.Block(i).Kind != parser.Paragraph {
			return false
		}
		in := d.Inlines(i)
		if len(in) < 2 || in[0].Kind != parser.Link || in[0].Next != len(in) {
			return false
		}
		if ui.Button(c, parser.PlainText(in[1:])).TextColor(c.Theme().Accent).Clicked() {
			open(in[0].Dest)
		}
		return true
	}
}
func (a *application) conversationImagesView(c *ui.Context, m *machine, chat *conversation, refs []imageReference) {
	if chat.images == nil {
		chat.images = map[string]*conversationImage{}
	}
	for _, ref := range refs {
		key := fmt.Sprintf("%x", sha256.Sum256([]byte(ref.source)))
		pic := chat.images[key]
		if pic == nil {
			if len(chat.images) >= 16 {
				if ui.Button(c, "查看图片："+ref.label).Clicked() {
					a.openReference(m, chat, "", ref.source)
				}
				continue
			}
			pending := 0
			for _, v := range chat.images {
				if v.loading {
					pending++
				}
			}
			if pending >= 2 {
				ui.Text(c, "图像等待加载…").FontSize(11)
				continue
			}
			pic = &conversationImage{label: ref.label, loading: true}
			chat.images[key] = pic
			s, agent, epoch := a.fileReader(m), chat.agent, m.epoch
			source := ref.source
			load := func() {
				ctx, cancel := context.WithTimeout(s.Context, 30*time.Second)
				defer cancel()
				data, bitmap, err := readImage(ctx, s, agent, "", source, s.Filesystem)
				update := func() {
					if chat.images[key] != pic {
						return
					}
					pic.loading = false
					if epoch != m.epoch || chat.agent.Target != agent.Target {
						return
					}
					if err != nil {
						pic.message = "暂无法预览"
						return
					}
					pic.data, pic.bitmap = data, bitmap
					chat.list.HoldPlace()
				}
				if a.win != nil {
					a.win.Update(update)
				} else {
					update()
				}
			}
			if a.win == nil {
				load()
			} else {
				go load()
			}
		}
		ic := c.Key("image:" + key)
		ui.Column(ic).Gap(5).MaxWidth(420).Children(func() {
			if pic.bitmap != nil {
				w, h := pic.bitmap.Size()
				if ui.Image(ic, pic.bitmap).FillWidth().AspectRatio(float32(w) / float32(h)).MaxHeight(260).Label("图像预览：" + pic.label).Clicked() {
					a.showImage(pic)
				}
			} else if pic.loading {
				ui.Spinner(ic).Size(16, 16)
			}
			ui.Text(ic, pic.label).FontSize(11).TextColor(c.Theme().TextMuted)
			if pic.message != "" {
				ui.Text(ic, pic.message).FontSize(11)
				if ui.Button(ic, "重试图像").Clicked() {
					delete(chat.images, key)
				}
			}
			if ui.Button(ic, "放大查看").Disabled(pic.bitmap == nil).Clicked() {
				a.showImage(pic)
			}
		})
	}
}
func (a *application) showImage(pic *conversationImage) {
	copy := *pic
	a.imagePreview = &copy
	a.imageZoom = 1
	if len(pic.data) == 0 {
		return
	}
	copy.loading = true
	load := func() {
		bitmap, err := decodePreviewImage(pic.data)
		update := func() {
			if a.imagePreview != &copy {
				return
			}
			copy.loading = false
			if err == nil {
				copy.bitmap = bitmap
			}
		}
		if a.win == nil {
			update()
		} else {
			a.win.Update(update)
		}
	}
	if a.win == nil {
		load()
	} else {
		go load()
	}
}
func (a *application) imageDialog(c *ui.Context) {
	pic := a.imagePreview
	if pic == nil {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		if pic.loading {
			ui.Spinner(c).Size(16, 16)
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, pic.label).Bold().Grow(1)
			if ui.Button(c, "缩小").Clicked() {
				a.imageZoom = max(.25, a.imageZoom/1.25)
			}
			ui.Text(c, fmt.Sprintf("%.0f%%", a.imageZoom*100)).FontSize(12)
			if ui.Button(c, "放大").Clicked() {
				a.imageZoom = min(4, a.imageZoom*1.25)
			}
			if ui.Button(c, "关闭图片").Clicked() {
				open = false
			}
		})
		if pic.bitmap != nil {
			w, h := pic.bitmap.Size()
			ui.Scroll(c).Height(560).FillWidth().Children(func() {
				width := float32(720 * a.imageZoom)
				ui.Image(c, pic.bitmap).Size(width, width*float32(h)/float32(w))
			})
		}
	}).Width(800).MaxWidth(1000)
	if !open {
		a.imagePreview = nil
	}
}
