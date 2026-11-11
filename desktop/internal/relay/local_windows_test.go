package relay

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/Microsoft/go-winio"
)

func TestWindowsLocalRoutesRejectRemotePathsAndAddresses(t *testing.T) {
	for _, path := range []string{`relative\tcp-pair\admin.sock`, `\\remote\share\tcp-pair\admin.sock`, `\\.\pipe\anything`, `C:\runtime\wrong\admin.sock`, `C:\runtime\tcp-pair\..\admin.sock`} {
		if _, err := windowsPairPipe(path); err == nil {
			t.Fatalf("unsafe pipe path accepted: %q", path)
		}
	}
	for _, address := range []string{"example.com:8375", "192.168.1.1:8375", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:http", "::1:8375"} {
		if _, err := localLoopbackAddress(address); err == nil {
			t.Fatalf("unsafe address accepted: %q", address)
		}
	}
	for address, want := range map[string]string{"0.0.0.0:8375": "127.0.0.1:8375", "[::]:8375": "[::1]:8375", "127.0.0.1:41234": "127.0.0.1:41234"} {
		if got, err := localLoopbackAddress(address); err != nil || got != want {
			t.Fatal(address, got, err)
		}
	}
}

func TestWindowsAdminVerifiesProcessBeforeAnyRequestAndAfterReply(t *testing.T) {
	for _, kind := range []string{"real inspector rejects test process", "changed process", "info"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tcp-pair", "admin.sock")
			pipe, err := windowsPairPipe(path)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := winio.ListenPipe(pipe, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			requests := make(chan string, 1)
			go func() {
				c, err := listener.Accept()
				if err != nil {
					requests <- ""
					return
				}
				defer c.Close()
				var request Message
				if json.NewDecoder(c).Decode(&request) != nil {
					requests <- ""
					return
				}
				requests <- Text(request, "action")
				json.NewEncoder(c).Encode(Message{"protocol": "herdr-local-v1", "address": "127.0.0.1:8375"})
			}()
			calls := 0
			inspect := func(net.Conn) (windowsRelayProcess, error) {
				calls++
				birth := uint64(1)
				if kind == "changed process" {
					birth = uint64(calls)
				}
				return windowsRelayProcess{PID: 42, Birth: birth}, nil
			}
			if kind == "real inspector rejects test process" {
				inspect = inspectWindowsRelay
			}
			address, _, err := windowsAdmin(t.Context(), path, "info", "", inspect)
			if kind == "info" {
				if err != nil || address != "127.0.0.1:8375" {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unverified process accepted")
			}
			request := <-requests
			if kind == "real inspector rejects test process" && request != "" {
				t.Fatal("request sent before process proof")
			}
			if request == "create" {
				t.Fatal("discovery requested an invitation")
			}
		})
	}
}

func TestWSLDistroParsingAndPersistentRoute(t *testing.T) {
	want := []string{"Ubuntu-22.04", "开发 Linux"}
	text := "\ufeff" + strings.Join(want, "\r\n") + "\r\n"
	words := utf16.Encode([]rune(text))
	encoded := make([]byte, len(words)*2)
	for i, word := range words {
		binary.LittleEndian.PutUint16(encoded[i*2:], word)
	}
	for _, raw := range [][]byte{[]byte(text), encoded} {
		got := parseWSLDistros(raw)
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("incorrect distro list: %q", got)
		}
	}
	target := wslTarget{Distro: "开发 Linux", User: "alice", UID: 1000, Socket: "/home/alice/a 'quote'/tcp-pair/admin.sock"}
	got, err := parseWSLRoute(target.route())
	if err != nil || got != target {
		t.Fatal("WSL route lost distro/user/path identity")
	}
	cmd := bridgeCommand(target)
	if cmd.Args[1] != "--distribution" || cmd.Args[2] != target.Distro || cmd.Args[3] != "--user" || cmd.Args[4] != target.User || cmd.Args[5] != "--exec" || cmd.Args[6] != "python3" {
		t.Fatal("WSL route must use separate argv, without a shell")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("WSL helper must stay hidden")
	}
	for _, bad := range []wslTarget{{Distro: "x", User: "a", UID: -1, Socket: "/x"}, {Distro: "x", User: "a", Socket: "relative"}, {Distro: "x\n", User: "a", Socket: "/x"}} {
		if _, err := parseWSLRoute(bad.route()); err == nil {
			t.Fatal("unsafe route accepted")
		}
	}
}

func TestWindowsLocalCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := windowsAdmin(ctx, filepath.Join(t.TempDir(), "tcp-pair", "admin.sock"), "info", "", inspectWindowsRelay)
	if err == nil || time.Since(start) > time.Second {
		t.Fatal("cancelled local dial was not bounded")
	}
}

func TestWSLResponseLimitAlsoAppliesToStreamCopy(t *testing.T) {
	var output limitedOutput
	if _, err := io.Copy(&output, strings.NewReader(strings.Repeat("x", 1024*1024+1))); err == nil {
		t.Fatal("unbounded helper output accepted")
	}
}
