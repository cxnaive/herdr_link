package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"log"
	"runtime"
	"time"

	"github.com/cxnaive/herdr-desktop/internal/pdfview"
	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/herdr-terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

//go:embed resources/icon.png
var appIcon []byte

//go:embed mygo.json
var appBuildConfig []byte

func applicationVersion() string {
	var config struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(appBuildConfig, &config); err != nil || config.Version == "" {
		panic("missing desktop build version")
	}
	return config.Version
}

var brandIcon, _ = ui.DecodeBitmap(appIcon)

type entry struct {
	ID     string           `json:"id"`
	Role   string           `json:"role"`
	Text   string           `json:"text"`
	Tools  []map[string]any `json:"tools"`
	Images []any            `json:"images"`
}
type conversation struct {
	agent                       relay.Agent
	entries                     []entry
	draft                       string
	next                        string
	revision                    string
	message                     string
	loading, pending, uncertain bool
	list                        ui.ListState
	md                          *markdown.Options
	mdTarget                    relay.Target
	mdEpoch                     uint64
	folds                       map[string]*bool
	files                       *fileBrowser
	run                         runPresentation
	preparing                   bool
	historyOlder                bool
	historyProgress             float64
	historyCancel               context.CancelFunc
	historyEpoch                uint64
	images                      map[string]*conversationImage
	markdownCache               map[string]renderedText
	question                    questionForm
	attachments                 []relay.Attachment
	uploading                   bool
	uploadCancel                context.CancelFunc
	uploadProgress              float64
	uploadLabel                 string
	subscription                string
	sequence                    int64
	updating                    bool
	updateCancel                context.CancelFunc
	lastCached                  time.Time
	historyBookmark             historyBookmark
	historyWork                 *historyRecovery
	historyPages, historyCount  int
	resumeOlder                 bool
	historySynchronized         bool
	provisionalIDs              map[string]bool
	searchOpen                  bool
	search                      string
	terminalSearch              string
	terminalSearchOpen          bool
}
type machine struct {
	notifiedTurns  map[string]string
	config         relay.Computer
	session        *relay.Session
	cancel         context.CancelFunc
	agents         []relay.Agent
	workspaces     []relay.Workspace
	status         string
	capabilities   map[string]bool
	profiles       []relay.Profile
	launchDefaults map[string][]string
	inventoryReady bool
	protocol       int64
	remote         *relay.TerminalConn
	term           *terminal.Terminal
	termIdentity   string
	termState      string
	opening        bool
	epoch          uint64
	filesystemRead bool
	fileEpoch      uint64
	fileContext    context.Context
	fileCancel     context.CancelFunc
}
type application struct {
	local                          localConnections
	win                            *mygo.Window
	ctx                            context.Context
	cancel                         context.CancelFunc
	store                          *storage
	machines                       []*machine
	chats                          map[string]*conversation
	selectedMachine, selectedAgent string
	sidebar                        float32
	mode                           int
	query                          string
	pairing                        bool
	pairName, pairLink             string
	pairBusy                       bool
	status                         string
	details                        bool
	files                          bool
	collapsed                      map[string]bool
	refreshCount                   int
	imagePreview                   *conversationImage
	imageZoom                      float64
	demo                           bool
	management                     *managementDialog
	toolsPanel                     *toolsPanel
	projectDialog                  *projectDialog
	deviceDialog                   *deviceDialog
	fileAccessDialog               *fileAccessDialog
	operations                     map[string]string
	busyOperations                 map[string]bool
	appearance                     appearanceSettings
	appearanceOpen                 bool
	appearanceMessage              string
	systemDark                     bool
}

func newApplication(demo bool) *application {
	ctx, cancel := context.WithCancel(context.Background())
	return &application{ctx: ctx, cancel: cancel, sidebar: 260, chats: map[string]*conversation{}, collapsed: map[string]bool{}, operations: map[string]string{}, busyOperations: map[string]bool{}, appearance: appearanceSettings{}.normalized(), demo: demo, status: "正在打开本机凭据…"}
}
func main() {
	log.Println("Herdr Desktop starting")
	demo := flag.Bool("demo", false, "show isolated example conversations and an explicitly local native terminal")
	flag.Parse()
	app := newApplication(*demo)
	if err := pdfview.Register(); err != nil {
		log.Fatal(err)
	}
	mygo.App.SetName("Herdr Desktop")
	mygo.App.SetVersion(applicationVersion())
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu},
		{Label: "视图", Submenu: []*mygo.MenuItem{
			{Label: "外观…", Click: func(_ *mygo.MenuItem, _ *mygo.Window) { app.appearanceOpen = true; app.win.Invalidate() }},
			{Label: "对话", Accelerator: "CmdOrCtrl+1", Click: func(_ *mygo.MenuItem, _ *mygo.Window) { app.setMode(0); app.win.Invalidate() }},
			{Label: "终端", Accelerator: "CmdOrCtrl+2", Click: func(_ *mygo.MenuItem, _ *mygo.Window) { app.setMode(1); app.win.Invalidate() }},
		}}, {Role: mygo.RoleWindowMenu},
	}))
	mygo.App.WhenReady(func() {
		app.systemDark = mygo.Theme.IsDark()
		mygo.Theme.OnUpdated(func() {
			if mygo.Theme.Source() == mygo.ThemeSystem {
				app.systemDark = mygo.Theme.IsDark()
			}
			app.applyTerminalAppearance()
			if app.win != nil {
				app.win.Invalidate()
			}
		})
		log.Println("Creating native window")
		app.win = mygo.NewWindow(mygo.WindowOptions{Title: "Herdr Desktop", Width: 1280, Height: 840, MinWidth: 800, MinHeight: 560, StateKey: "main", Content: ui.View(app.view)})
		if err := app.win.SetIcon(appIcon); err != nil {
			log.Printf("window icon: %v", err)
		}
		windowReady, preferencesReady := false, app.demo
		app.win.OnReadyToShow(func() {
			windowReady = true
			if preferencesReady {
				app.win.Show()
			}
		})
		app.win.OnClosed(app.shutdown)
		app.win.OnFileDrop(func(e *mygo.FileDropEvent) {
			if app.mode == 0 && !app.pairing && app.management == nil && app.projectDialog == nil && app.deviceDialog == nil && app.toolsPanel == nil && app.imagePreview == nil && app.fileAccessDialog == nil && !app.appearanceOpen {
				app.uploadPaths(e.Paths)
			}
		})
		if app.demo {
			app.loadDemo()
			app.win.Invalidate()
			return
		}
		go func() {
			s, err := openStorage()
			app.win.Update(func() {
				preferencesReady = true
				defer func() {
					if windowReady {
						app.win.Show()
					}
				}()
				if err != nil {
					app.status = "无法打开本机加密存储：" + err.Error()
					return
				}
				app.store = s
				app.collapsed = s.sidebarGroups()
				app.operations = s.operations()
				app.appearance = s.appearance()
				app.applyAppearance()
				app.status = ""
				for _, c := range s.computers() {
					m := &machine{config: c, status: "未连接"}
					app.machines = append(app.machines, m)
					app.connect(m)
				}
				if len(app.machines) == 0 {
					app.pairing = true
					if runtime.GOOS == "linux" {
						app.findLocalRelays()
					}
				}
			})
		}()
		go func() {
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for {
				select {
				case <-app.ctx.Done():
					return
				case <-tick.C:
					app.win.Update(func() {
						app.refreshCount++
						app.refreshRun()
						if app.mode == 0 && !app.pairing {
							app.syncConversation()
						}
					})
				}
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
func (a *application) shutdown() {
	a.cancel()
	for _, chat := range a.chats {
		if chat.files != nil {
			chat.files.closePDF()
		}
	}
	for _, m := range a.machines {
		if m.cancel != nil {
			m.cancel()
		}
		if m.remote != nil {
			m.remote.Close()
		}
		if m.term != nil {
			m.term.Close()
		}
		if m.session != nil {
			m.session.Close()
		}
	}
	a.saveCurrentDraft()
}
