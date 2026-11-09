package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func procStat(birth string) []byte {
	return []byte("42 (relay (test)) S " + strings.Repeat("0 ", 18) + birth + " 0\n")
}
func TestLocalProcessSettingsAreScopedAndLoopbackOnly(t *testing.T) {
	for _, tc := range []struct {
		name            string
		env             map[string]string
		socket, address string
	}{
		{"default", map[string]string{"HOME": "/home/example"}, "/home/example/.config/herdr-mobile-relay/tcp-pair/admin.sock", "127.0.0.1:8375"},
		{"config", map[string]string{"XDG_CONFIG_HOME": "/private/config", "HERDR_RELAY_PORT": "4321"}, "/private/config/herdr-mobile-relay/tcp-pair/admin.sock", "127.0.0.1:4321"},
		{"plugin", map[string]string{"HOME": "/home/example", "HERDR_PLUGIN_CONFIG_DIR": "/private/plugin", "HERDR_RELAY_HOST": "0.0.0.0"}, "/private/plugin/tcp-pair/admin.sock", "127.0.0.1:8375"},
		{"env wins", map[string]string{"HOME": "/home/example", "HERDR_PLUGIN_CONFIG_DIR": "/private/plugin", "HERDR_RELAY_ENV": "/private/instance/relay.env", "HERDR_RELAY_HOST": "::"}, "/private/instance/tcp-pair/admin.sock", "[::1]:8375"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := processFromEnvironment(42, procStat("123"), tc.env)
			if err != nil || p.Socket != tc.socket || p.Address != tc.address || p.Birth != "123" {
				t.Fatalf("%+v %v", p, err)
			}
		})
	}
	for _, env := range []map[string]string{
		{}, {"HOME": "relative"}, {"HOME": "/home/x", "HERDR_RELAY_HOST": "example.com"},
		{"HOME": "/home/x", "HERDR_RELAY_HOST": "192.168.1.1"}, {"HOME": "/home/x", "HERDR_RELAY_PORT": "0"},
		{"HOME": "/home/x", "HERDR_RELAY_ENV": "relative/relay.env"}, {"HOME": "/home/x", "HERDR_PLUGIN_CONFIG_DIR": "relative"},
	} {
		if _, err := processFromEnvironment(42, procStat("123"), env); err == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
	for _, bad := range []string{"", "42 (relay)", string(procStat("0")), string(procStat("oops"))} {
		if processBirth([]byte(bad)) != "" {
			t.Fatal("invalid process start accepted")
		}
	}
}
func privateAdminFixture(t *testing.T) (string, *net.UnixListener, localInspector) {
	t.Helper()
	dir, err := os.MkdirTemp("", "herdr-local-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "tcp-pair", "admin.sock")
	if err = os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	inspect := func(pid int) (localProcess, error) {
		return localProcess{PID: pid, Birth: "123", Socket: path, Address: "127.0.0.1:8375"}, nil
	}
	return path, l, inspect
}
func TestLocalAdminChecksPermissionsPeerAndIdentityBeforeCreatingAnything(t *testing.T) {
	for _, change := range []string{"safe", "public socket", "public parent", "symlink", "wrong process", "changed pid", "wrong runtime", "not socket"} {
		t.Run(change, func(t *testing.T) {
			path, l, inspect := privateAdminFixture(t)
			switch change {
			case "public socket":
				os.Chmod(path, 0666)
			case "public parent":
				os.Chmod(filepath.Dir(path), 0755)
			case "symlink":
				link := filepath.Join(filepath.Dir(path), "link")
				os.Symlink(path, link)
				path = link
			case "wrong process":
				inspect = func(int) (localProcess, error) { return localProcess{}, ErrLocalUnsafe }
			case "changed pid":
				base := inspect
				inspect = func(pid int) (localProcess, error) { p, _ := base(pid); p.PID++; return p, nil }
			case "wrong runtime":
				base := inspect
				inspect = func(pid int) (localProcess, error) { p, _ := base(pid); p.Socket += "other"; return p, nil }
			case "not socket":
				l.Close()
				os.WriteFile(path, []byte("not socket"), 0600)
			}
			c, _, err := connectLocalAdmin(t.Context(), path, inspect)
			if change != "safe" {
				if err == nil {
					c.Close()
					t.Fatal("unsafe peer accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			c.Close()
			accepted, err := l.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer accepted.Close()
			var request map[string]any
			if json.NewDecoder(accepted).Decode(&request) == nil {
				t.Fatal("discovery created an invitation")
			}
		})
	}
}
func TestLocalPairingUsesOnlyPrivateInvitationAndNeverStoresAdminSecrets(t *testing.T) {
	path, l, inspect := privateAdminFixture(t)
	done := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		var request Message
		if err = json.NewDecoder(c).Decode(&request); err != nil {
			done <- err
			return
		}
		if Text(request, "action") != "create" || Text(request, "name") != "本机测试" {
			done <- fmt.Errorf("invalid private request")
			return
		}
		link := "herdr-tcp://unreachable.invalid:8377/#invite=abcdefghijklmnop&invite_version=1&invite_expires=9999999999999&setup=" + encode(make([]byte, 32)) + "&rendezvous=" + encode(make([]byte, 32)) + "&relay_id=" + encode(make([]byte, 16))
		done <- json.NewEncoder(c).Encode(Message{"link": link})
	}()
	computer, err := pairLocal(t.Context(), LocalRelay{Socket: path}, "本机测试", inspect)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if computer.LocalSocket != path || computer.Auth.Kind != "invitation" || computer.Address != "unreachable.invalid:8377" || computer.Name != "本机测试" {
		t.Fatal("incorrect local enrollment")
	}
}
func TestLocalPairCancellationIsBounded(t *testing.T) {
	path, l, inspect := privateAdminFixture(t)
	go func() {
		c, err := l.Accept()
		if err == nil {
			defer c.Close()
			time.Sleep(100 * time.Millisecond)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := pairLocal(ctx, LocalRelay{Socket: path}, "", inspect); err == nil {
		t.Fatal("cancelled pairing succeeded")
	}
}
func TestLocalProcessInspectorRejectsNonRelay(t *testing.T) {
	if _, err := inspectRelayProcess(os.Getpid()); err == nil {
		t.Fatal("test binary impersonates relay")
	}
}

func TestPrivateSocketAllowsPluginDirectoriesOnlyBehindPrivateBoundary(t *testing.T) {
	root, err := os.MkdirTemp("", "herdr-permissions-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	// Normal installer: private ~/.config, group-writable plugins, private runtime.
	parent := filepath.Join(root, "plugins")
	os.Mkdir(parent, 0775)
	os.Chmod(parent, 0775)
	private := filepath.Join(parent, "runtime", "tcp-pair")
	os.MkdirAll(private, 0700)
	path := filepath.Join(private, "admin.sock")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	os.Chmod(path, 0600)
	if _, err = privateSocket(path); err != nil {
		t.Fatal("private config boundary rejected")
	}
	os.Chmod(root, 0777)
	if _, err = privateSocket(path); err == nil {
		t.Fatal("publicly replaceable root accepted")
	}
	os.Chmod(root, 0700)
	link := filepath.Join(root, "alias")
	os.Symlink(parent, link)
	if _, err = privateSocket(filepath.Join(link, "runtime", "tcp-pair", "admin.sock")); err == nil {
		t.Fatal("symlinked ancestor accepted")
	}
}
