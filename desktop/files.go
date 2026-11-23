package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/cxnaive/herdr-desktop/internal/pdfview"
	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

type fileBrowser struct {
	agent                                             relay.Agent
	page                                              relay.Directory
	path, query, message, previewPath, previewMessage string
	metadata                                          *relay.FileMetadata
	document                                          *relay.Document
	bitmap                                            *ui.Bitmap
	loading, attempted, downloading                   bool
	downloadBytes                                     int64
	downloadMessage                                   string
	cancel, cancelDownload                            context.CancelFunc
	operation                                         uint64
	list                                              ui.ListState
	md                                                *markdown.Options
	pdf                                               *pdfview.Viewer
	pdfLoading                                        bool
	expanded                                          bool
	showHidden                                        bool
	jumpPath                                          string
	filesystem                                        bool
	fileEpoch                                         uint64
}

func (f *fileBrowser) closePDF() {
	if f.pdf != nil {
		f.pdf.Close()
		f.pdf = nil
	}
	f.pdfLoading = false
}

func filesFor(chat *conversation) *fileBrowser {
	f := chat.files
	if f == nil || f.agent.Target != chat.agent.Target || f.agent.Cwd != chat.agent.Cwd {
		if f != nil {
			f.closePDF()
			if f.cancel != nil {
				f.cancel()
			}
			if f.cancelDownload != nil {
				f.cancelDownload()
			}
		}
		f = &fileBrowser{agent: chat.agent, md: &markdown.Options{}}
		chat.files = f
	}
	return f
}
func (a *application) ensureFiles(m *machine, chat *conversation) {
	f := filesFor(chat)
	if !f.attempted {
		a.loadDirectory(m, chat, fileHome(m, chat.agent), false)
	}
	if f.document != nil && f.document.Kind == "pdf" && f.pdf == nil && f.previewMessage == "" {
		a.preparePDF(f)
	}
}
func (a *application) fileRequestCurrent(m *machine, chat *conversation, f *fileBrowser, epoch, operation uint64) bool {
	return m.epoch == epoch && m.fileEpoch == f.fileEpoch && m.filesystemRead == f.filesystem && chat.files == f && f.operation == operation && slices.ContainsFunc(m.agents, func(agent relay.Agent) bool { return agent.Target == f.agent.Target && agent.Cwd == f.agent.Cwd })
}
func (a *application) loadDirectory(m *machine, chat *conversation, p string, more bool) {
	f := filesFor(chat)
	f.filesystem, f.fileEpoch = m.filesystemRead, m.fileEpoch
	f.closePDF()
	f.md = &markdown.Options{}
	if f.cancel != nil {
		f.cancel()
	}
	f.operation++
	operation, epoch := f.operation, m.epoch
	var previous *relay.Directory
	if more {
		copy := f.page
		previous = &copy
	} else {
		f.page = relay.Directory{}
		f.query = ""
		f.list = ui.ListState{}
	}
	f.path, f.previewPath, f.message, f.previewMessage = p, "", "", ""
	f.document, f.metadata, f.bitmap = nil, nil, nil
	f.attempted = true
	if a.demo {
		f.page = demoDirectory(p)
		f.loading = false
		return
	}
	if m.session == nil {
		f.message = "连接电脑后查看文件"
		f.loading = false
		return
	}
	if !m.capabilities["workspace_directory_v1"] {
		f.message = "请更新这台电脑上的 Relay 后查看文件"
		f.loading = false
		return
	}
	f.loading = true
	reader := a.fileReader(m)
	ctx, cancel := context.WithTimeout(reader.Context, 30*time.Second)
	f.cancel = cancel
	s, agent := reader, f.agent
	go func() {
		defer cancel()
		page, err := relay.ListDirectory(ctx, s, agent, p, previous)
		a.win.Update(func() {
			if !a.fileRequestCurrent(m, chat, f, epoch, operation) {
				return
			}
			f.loading = false
			if err != nil {
				log.Printf("directory read: %v", err)
				f.message = "无法读取目录，请刷新重试"
				return
			}
			f.page = page
		})
	}()
}
func (a *application) openFile(m *machine, chat *conversation, p string) {
	f := filesFor(chat)
	f.filesystem, f.fileEpoch = m.filesystemRead, m.fileEpoch
	f.closePDF()
	f.attempted = true
	if f.cancel != nil {
		f.cancel()
	}
	f.operation++
	operation, epoch := f.operation, m.epoch
	f.previewPath, f.previewMessage = p, ""
	f.document, f.metadata, f.bitmap = nil, nil, nil
	f.loading = false
	if a.demo {
		f.document = demoDocument(p)
		f.metadata = &relay.FileMetadata{Path: p, Kind: f.document.Kind, Size: int64(len(f.document.Data)), ReadOnly: true}
		f.loading = false
		a.configureFileMarkdown(m, chat, f)
		return
	}
	if m.session == nil {
		f.previewMessage = "连接电脑后查看文件"
		return
	}
	if !m.capabilities["workspace_file_download_v1"] {
		f.previewMessage = "请更新这台电脑上的 Relay 后查看文件"
		return
	}
	f.loading = true
	reader := a.fileReader(m)
	ctx, cancel := context.WithTimeout(reader.Context, 2*time.Minute)
	f.cancel = cancel
	s, agent := reader, f.agent
	go func() {
		defer cancel()
		meta, err := relay.GetFileMetadata(ctx, s, agent, p)
		var doc *relay.Document
		var bitmap *ui.Bitmap
		previewMessage := ""
		if err == nil {
			if meta.PreviewAvailable && (meta.Kind == "text" || meta.Kind == "image" || meta.Kind == "pdf") {
				value, readErr := relay.ReadDocument(ctx, s, agent, p)
				if readErr != nil {
					previewMessage = "预览暂不可用，可下载原文件"
				} else {
					doc = &value
					if value.Kind == "image" {
						cfg, _, decodeErr := image.DecodeConfig(bytes.NewReader(value.Data))
						if decodeErr == nil && cfg.Width > 0 && cfg.Height > 0 && int64(cfg.Width)*int64(cfg.Height) <= 24000000 {
							bitmap, decodeErr = ui.DecodeBitmap(value.Data)
						}
						if decodeErr != nil || bitmap == nil {
							previewMessage = "此图片暂不支持预览，可下载查看"
						}
					}
				}
			} else {
				previewMessage = "此文件暂不支持预览，可下载查看"
			}
		}
		a.win.Update(func() {
			if !a.fileRequestCurrent(m, chat, f, epoch, operation) {
				return
			}
			f.loading = false
			if err != nil {
				log.Printf("file read: %v", err)
				f.previewMessage = "无法读取文件，请重新打开"
				return
			}
			f.metadata, f.document, f.bitmap, f.previewMessage = &meta, doc, bitmap, previewMessage
			if doc != nil && doc.Kind == "pdf" {
				a.preparePDF(f)
			}
			a.configureFileMarkdown(m, chat, f)
		})
	}()
}
func (a *application) downloadFile(m *machine, chat *conversation) {
	f := filesFor(chat)
	if f.downloading || f.metadata == nil || m.session == nil || a.demo {
		return
	}
	meta, agent, session := *f.metadata, f.agent, a.fileReader(m)
	f.downloading, f.downloadBytes, f.downloadMessage = true, 0, "选择保存位置…"
	ctx, cancel := context.WithCancel(session.Context)
	f.cancelDownload = cancel
	go func() {
		defer cancel()
		destination, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.win, Title: "下载文件", DefaultPath: safeDownloadName(meta.Path)})
		if err == nil && destination != "" && ctx.Err() == nil {
			err = saveDownload(ctx, destination, func(out *os.File) error {
				return relay.Download(ctx, session, agent, meta, out, func(n int64) {
					a.win.Update(func() { f.downloadBytes = n; f.downloadMessage = "正在下载 " + path.Base(meta.Path) })
				})
			})
		}
		cancelled := ctx.Err() != nil
		a.win.Update(func() {
			f.downloading = false
			switch {
			case cancelled || destination == "":
				f.downloadMessage = "已取消下载"
			case err != nil:
				log.Printf("download: %v", err)
				f.downloadMessage = "下载未完成，请重新打开文件后重试"
			default:
				f.downloadMessage = "已保存到 " + destination
			}
		})
	}()
}

// Only publish the complete verified file, leaving an existing destination
// untouched on cancellation, transfer failure or disk error.
func saveDownload(ctx context.Context, destination string, transfer func(*os.File) error) error {
	f, err := os.CreateTemp(filepath.Dir(destination), ".herdr-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = transfer(f)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(f.Name(), destination)
}
func safeDownloadName(p string) string {
	name := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, path.Base(p))
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." {
		return "download"
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9') {
		name = "_" + name
	}
	return name
}
func fileSize(n int64) string {
	if n >= 1024*1024*1024 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
	}
	if n >= 1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
	if n >= 1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%d B", n)
}
func (a *application) fileView(c *ui.Context, m *machine, chat *conversation) {
	a.ensureFiles(m, chat)
	f, t := filesFor(chat), c.Theme()
	panel := ui.Column(c.Key("files:"+m.config.ID+chat.agent.Identity())).FillHeight().Padding(16).Gap(12).BorderWidth(0, 0, 0, 1).BorderColor(t.Border)
	if f.expanded {
		panel.Grow(1)
	} else {
		panel.Width(380)
	}
	panel.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			title := "工作区文件"
			if m.filesystemRead {
				title = "电脑文件 · 只读"
			}
			ui.Text(c, title).Bold().Grow(1)
			expandLabel := "展开预览"
			if f.expanded {
				expandLabel = "收窄预览"
			}
			if ui.Button(c, expandLabel).Clicked() {
				f.expanded = !f.expanded
			}
			if ui.Button(c, "收起").Clicked() {
				a.files = false
			}
		})
		if m.capabilities["filesystem_read_opt_in_v1"] && ui.Button(c, "访问范围").Disabled(m.session == nil).Clicked() {
			a.openFileAccess(m)
		}
		if f.previewPath != "" {
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "返回目录").Clicked() {
					f.closePDF()
					if f.cancel != nil {
						f.cancel()
					}
					f.operation++
					f.previewPath = ""
					f.document = nil
					f.bitmap = nil
					f.metadata = nil
					f.loading = false
					if f.page.Revision == "" {
						a.loadDirectory(m, chat, f.path, false)
					}
				}
				ui.Spacer(c)
				if ui.Button(c, "下载").Disabled(f.metadata == nil || f.downloading || a.demo || m.session == nil).Clicked() {
					a.downloadFile(m, chat)
				}
			})
			ui.Text(c, path.Base(f.previewPath)).Bold().Selectable()
			if f.metadata != nil {
				ui.Text(c, fileSize(f.metadata.Size)+" · 只读").FontSize(11).TextColor(t.TextMuted)
			}
			if f.pdf != nil {
				if f.pdfLoading {
					ui.Spinner(c).Size(16, 16)
				}
				ui.WebView(c, f.pdf.Web).Grow(1).FillWidth()
			} else {
				ui.Scroll(c.Key("preview:" + f.previewPath)).Grow(1).Gap(10).Children(func() {
					if f.loading {
						ui.Spinner(c).Size(20, 20)
						ui.Text(c, "正在打开…").TextColor(t.TextMuted)
					}
					if f.previewMessage != "" {
						ui.Text(c, f.previewMessage).TextColor(t.TextMuted)
					}
					if f.document != nil && f.document.Kind == "text" {
						ext := strings.ToLower(path.Ext(f.document.Path))
						if ext == ".md" || ext == ".markdown" {
							a.markdownView(c, citationMarkdown(string(f.document.Data)), f.md)
						} else {
							ui.Text(c, string(f.document.Data)).Font("monospace").FontSize(12).Selectable()
						}
					}
					if f.bitmap != nil {
						w, h := f.bitmap.Size()
						if ui.Image(c, f.bitmap).FillWidth().AspectRatio(float32(w) / float32(h)).Clicked() {
							a.showImage(&conversationImage{label: path.Base(f.previewPath), bitmap: f.bitmap, data: f.document.Data})
						}
					}
				})
			}
		} else {
			directory := path.Join(chat.agent.Cwd, f.path)
			if m.filesystemRead {
				directory = "/" + f.path
			}
			ui.Text(c, directory).FontSize(11).TextColor(t.TextMuted).Selectable()
			ui.Row(c).Gap(6).Children(func() {
				if m.filesystemRead && ui.Button(c, "工作区").Clicked() {
					a.loadDirectory(m, chat, fileHome(m, chat.agent), false)
				}
				if ui.Button(c, "根目录").Disabled(f.path == "").Clicked() {
					a.loadDirectory(m, chat, "", false)
				}
				if ui.Button(c, "上一级").Disabled(f.path == "").Clicked() {
					a.loadDirectory(m, chat, relay.ParentDirectory(f.path), false)
				}
				ui.Spacer(c)
				if ui.Button(c, "刷新文件").Disabled(f.loading).Clicked() {
					a.loadDirectory(m, chat, f.path, false)
				}
			})
			if f.path != "" {
				ui.Text(c, f.path).FontSize(12).Selectable()
			}
			ui.TextInput(c, &f.query).Placeholder("筛选当前目录").Label("筛选文件")
			ui.Checkbox(c, &f.showHidden, "显示隐藏文件")
			ui.Row(c).Gap(6).Children(func() {
				ui.TextInput(c, &f.jumpPath).Label("文件路径").Placeholder("文件路径；目录以 / 结尾").Grow(1)
				if ui.Button(c, "打开路径").Disabled(f.loading || strings.TrimSpace(f.jumpPath) == "").Clicked() {
					p, _, err := fileReference(chat.agent.Cwd, "", strings.TrimSpace(f.jumpPath), m.filesystemRead)
					if err != nil {
						f.message = err.Error()
					} else if strings.HasSuffix(f.jumpPath, "/") {
						a.loadDirectory(m, chat, p, false)
					} else {
						a.openFile(m, chat, p)
					}
				}
			})
			if f.message != "" {
				ui.Text(c, f.message).TextColor(t.Warning).FontSize(12)
			}
			if f.loading {
				ui.Spinner(c).Size(16, 16)
				ui.Text(c, "正在读取…").TextColor(t.TextMuted).FontSize(12)
			}
			entries := make([]relay.FileEntry, 0, len(f.page.Entries))
			for _, e := range f.page.Entries {
				if (f.showHidden || !strings.HasPrefix(e.Name, ".")) && strings.Contains(strings.ToLower(e.Name), strings.ToLower(f.query)) {
					entries = append(entries, e)
				}
			}
			f.list.Key = func(i int) any { return entries[i].Path }
			var clicked *relay.FileEntry
			ui.List(c.Key("directory:"+m.config.ID+chat.agent.Identity()+f.path), &f.list, len(entries), func(i int) {
				e := entries[i]
				ec := c.Key(e.Path)
				button := ui.Button(ec, "").Label(e.Name).FillWidth().Padding(9, 4).BorderWidth(0).Background(t.Background).Justify(ui.Start)
				button.Children(func() {
					ui.Row(ec).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						mark := "·"
						if e.Kind == "directory" {
							mark = "▸"
						}
						ui.Text(ec, mark).TextColor(t.Accent)
						ui.Text(ec, e.Name).FontSize(12).Grow(1)
						if e.Kind == "file" {
							ui.Text(ec, fileSize(e.Size)).FontSize(10).TextColor(t.TextMuted)
						}
					})
				})
				if button.Clicked() {
					clicked = &e
				}
			}).Grow(1).Gap(2)
			if clicked != nil {
				if clicked.Kind == "directory" {
					a.loadDirectory(m, chat, clicked.Path, false)
				} else {
					a.openFile(m, chat, clicked.Path)
				}
			}
			if len(entries) == 0 && !f.loading && f.message == "" {
				ui.Text(c, "没有匹配的文件").TextColor(t.TextMuted).FontSize(12)
			}
			if f.page.NextCursor != "" && ui.Button(c, "加载更多文件").Disabled(f.loading).Clicked() {
				a.loadDirectory(m, chat, f.path, true)
			}
			ui.Text(c, fmt.Sprintf("%d 项", len(f.page.Entries))).FontSize(11).TextColor(t.TextMuted)
		}
		if f.downloading {
			if f.metadata != nil && f.metadata.Size > 0 {
				ui.Progress(c, float64(f.downloadBytes)/float64(f.metadata.Size)).FillWidth()
			}
			ui.Text(c, f.downloadMessage+" · "+fileSize(f.downloadBytes)).FontSize(11).TextColor(t.TextMuted)
			if ui.Button(c, "取消下载").Clicked() {
				if f.cancelDownload != nil {
					f.cancelDownload()
				}
			}
		} else if f.downloadMessage != "" {
			ui.Text(c, f.downloadMessage).FontSize(11).TextColor(t.TextMuted).Selectable()
		}
	})
}

func (a *application) preparePDF(f *fileBrowser) {
	if a.win == nil {
		return
	}
	f.pdfLoading = true
	operation := f.operation
	v, err := pdfview.New(a.win, f.document.Data, func(err error) {
		a.win.Update(func() {
			if f.operation != operation {
				return
			}
			f.pdfLoading = false
			if err != nil {
				f.closePDF()
				f.previewMessage = "PDF 暂时无法预览，可下载查看"
			}
		})
	})
	if err != nil {
		f.pdfLoading = false
		f.previewMessage = "PDF 暂时无法预览，可下载查看"
		return
	}
	f.pdf = v
	f.expanded = true
}

func demoDirectory(p string) relay.Directory {
	entries := []relay.FileEntry{{Path: "reports", Name: "reports", Kind: "directory"}, {Path: "src", Name: "src", Kind: "directory"}, {Path: "README.md", Name: "README.md", Kind: "file", Size: 256}, {Path: "eval-summary.json", Name: "eval-summary.json", Kind: "file", Size: 74}}
	if p == "reports" {
		entries = []relay.FileEntry{{Path: "reports/evaluation.md", Name: "evaluation.md", Kind: "file", Size: 256}}
	}
	if p == "src" {
		entries = []relay.FileEntry{{Path: "src/main.py", Name: "main.py", Kind: "file", Size: 120}}
	}
	return relay.Directory{Root: "/work/robotics", Path: p, Entries: entries, Total: len(entries), Revision: "demo"}
}
func demoDocument(p string) *relay.Document {
	text := "# 机器人研究\n\n评测结果与运行记录。\n\n- 成功率：96.0%\n- 验证集：500 条\n\n下一步复查失败样例。"
	if strings.HasSuffix(p, ".json") {
		text = "{\n  \"success_rate\": 0.96,\n  \"samples\": 500\n}"
	}
	if strings.HasSuffix(p, ".py") {
		text = "def evaluate(results):\n    return sum(results) / len(results)\n"
	}
	return &relay.Document{Path: p, Kind: "text", MediaType: "text/plain", Data: []byte(text)}
}
