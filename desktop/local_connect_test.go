package main

import (
	"image/png"
	"os"
	"testing"

	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo/ui"
)

func TestLocalConnectionReusesIdentityDraftNamespaceAndRole(t *testing.T) {
	saved := relay.Computer{ID: "saved", Name: "我的电脑", RelayID: "same", Address: "gateway:8377", Auth: relay.Auth{Kind: "credential", Role: "reader", ID: "device", Secret: "existing"}}
	next := relay.Computer{ID: "new", Name: "新名称", RelayID: "same", LocalSocket: "/private/tcp-pair/admin.sock", Auth: relay.Auth{Kind: "invitation", Role: "controller"}}
	got := localComputerForCatalog(next, []relay.Computer{saved})
	if got.ID != saved.ID || got.Name != saved.Name || got.Auth != saved.Auth || got.LocalSocket != next.LocalSocket {
		t.Fatal("local route changed identity or escalated saved role")
	}
	next.RelayID = "other"
	if got := localComputerForCatalog(next, []relay.Computer{saved}); got.ID != "new" {
		t.Fatal("merged different computers")
	}
}
func TestLocalPairViewOffersLocalRouteWithoutAutomaticallyPairing(t *testing.T) {
	if !localRelaySupported() {
		t.Skip("local connection entry point")
	}
	a := newApplication(true)
	defer a.cancel()
	a.pairing = true
	a.local.relays = []relay.LocalRelay{{Socket: "/private/one", Label: "Windows · 127.0.0.1:8375"}, {Socket: "/private/two", Label: "WSL · Ubuntu · 127.0.0.1:8375"}}
	tt := ui.NewTester(a.view, 1180, 840)
	if !tt.HasText("查找本机 Relay") || !tt.HasText("远程电脑邀请") || !tt.HasText(a.local.relays[0].Label) || !tt.HasText(a.local.relays[1].Label) {
		t.Fatal("missing local discovery or remote invitation")
	}
	if path := os.Getenv("HERDR_LOCAL_UI_SCREENSHOT"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	tt.SetSize(800, 560)
	tt.Frame()
	a.findLocalRelays()
	a.connectLocalRelay(a.local.relays[0])
	if a.local.connecting || a.local.searching || len(a.machines) != 0 {
		t.Fatal("demo/no-vault state touched live local relay")
	}
}
