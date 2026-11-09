package main

import (
	"context"
	"runtime"
	"time"

	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo/ui"
)

type localConnections struct {
	searching, connecting bool
	sequence              uint64
	relays                []relay.LocalRelay
	message               string
}

func (a *application) localRelayView(c *ui.Context) {
	if runtime.GOOS != "linux" {
		return
	}
	busy := a.pairBusy || a.local.searching || a.local.connecting
	ui.Column(c).Gap(8).MaxWidth(620).Children(func() {
		ui.Text(c, "本机 Relay").Bold()
		ui.Text(c, "直接连接当前 Linux 用户的 Relay，无需复制邀请，不经过远程 Gateway。").FontSize(12).TextColor(c.Theme().TextMuted).Wrap()
		label := "查找本机 Relay"
		if a.local.searching {
			label = "正在查找…"
		}
		if a.local.connecting {
			label = "正在连接本机…"
		}
		if ui.Button(c, label).Disabled(busy || a.store == nil || a.demo).Clicked() {
			a.findLocalRelays()
		}
		if a.local.message != "" {
			ui.Text(c, a.local.message).FontSize(12).TextColor(c.Theme().TextMuted).Wrap()
		}
		ui.Scroll(c).MaxHeight(180).Gap(6).Children(func() {
			for _, candidate := range a.local.relays {
				ui.Row(c.Key(candidate.Socket)).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, candidate.Label).Grow(1)
					if ui.Button(c, "连接本机").Disabled(busy || a.store == nil || a.demo).Clicked() {
						a.connectLocalRelay(candidate)
					}
				})
			}
		})
	})
}
func (a *application) findLocalRelays() {
	if a.store == nil || a.demo || a.local.searching || a.local.connecting {
		return
	}
	a.local.searching = true
	a.local.sequence++
	seq := a.local.sequence
	a.local.message = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
		defer cancel()
		found, err := relay.DiscoverLocal(ctx)
		a.update(func() {
			if a.ctx.Err() != nil || a.local.sequence != seq {
				return
			}
			a.local.searching = false
			a.local.relays = found
			if err != nil {
				a.local.message = err.Error()
			} else {
				a.local.message = "仅列出当前用户的运行实例；点击连接后才创建独立设备凭据。"
			}
		})
	}()
}

// Reuse a saved device instead of duplicating the computer or its drafts.
// A transport switch never upgrades a saved reader credential to controller.
func localComputerForCatalog(next relay.Computer, existing []relay.Computer) relay.Computer {
	for _, saved := range existing {
		if saved.RelayID == next.RelayID && saved.RelayID != "" && saved.Auth.Kind == "credential" {
			saved.LocalSocket = next.LocalSocket
			return saved
		}
	}
	return next
}
func (a *application) connectLocalRelay(candidate relay.LocalRelay) {
	if a.store == nil || a.demo || a.local.connecting || a.local.searching || a.pairBusy {
		return
	}
	for _, m := range a.machines {
		if m.config.LocalSocket == candidate.Socket {
			a.pairing = false
			a.local.message = ""
			a.connect(m)
			return
		}
	}
	a.local.connecting = true
	a.local.message = ""
	name := a.pairName
	store := a.store
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		computer, err := relay.PairLocal(ctx, candidate, name)
		if err == nil {
			computer = localComputerForCatalog(computer, store.computers())
			if ctx.Err() != nil {
				err = ctx.Err()
			} else {
				err = store.saveComputer(computer)
			}
		}
		a.update(func() {
			a.local.connecting = false
			if a.ctx.Err() != nil {
				return
			}
			if err != nil {
				a.local.message = err.Error()
				return
			}
			var target *machine
			for _, m := range a.machines {
				if m.config.ID == computer.ID {
					m.config = computer
					target = m
					break
				}
			}
			if target == nil {
				target = &machine{config: computer, status: "未连接"}
				a.machines = append(a.machines, target)
			}
			a.pairing = false
			a.pairName = ""
			a.pairLink = ""
			a.status = ""
			a.connect(target)
		})
	}()
}
