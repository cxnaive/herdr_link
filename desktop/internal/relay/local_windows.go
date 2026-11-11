package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

type windowsRelayProcess struct {
	PID        uint32
	Birth      uint64
	Executable string
}

type windowsRelayInspector func(net.Conn) (windowsRelayProcess, error)

func inspectWindowsRelay(conn net.Conn) (windowsRelayProcess, error) {
	fd, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return windowsRelayProcess{}, ErrLocalUnsafe
	}
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(fd.Fd()), &pid); err != nil {
		return windowsRelayProcess{}, ErrLocalUnsafe
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return windowsRelayProcess{}, ErrLocalUnsafe
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err = windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return windowsRelayProcess{}, ErrLocalUnsafe
	}
	defer token.Close()
	owner, err := token.GetTokenUser()
	if err != nil {
		return windowsRelayProcess{}, ErrLocalUnsafe
	}
	self, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || !owner.User.Sid.Equals(self.User.Sid) {
		return windowsRelayProcess{}, ErrLocalUnsafe
	}
	var birth, exit, kernel, user windows.Filetime
	if windows.GetProcessTimes(process, &birth, &exit, &kernel, &user) != nil {
		return windowsRelayProcess{}, ErrLocalUnsafe
	}
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if windows.QueryFullProcessImageName(process, 0, &buffer[0], &size) != nil {
		return windowsRelayProcess{}, ErrLocalUnsafe
	}
	executable := windows.UTF16ToString(buffer[:size])
	if !strings.EqualFold(filepath.Base(executable), "herdr-mobile-relay.exe") {
		return windowsRelayProcess{}, ErrLocalUnsafe
	}
	return windowsRelayProcess{pid, uint64(birth.HighDateTime)<<32 | uint64(birth.LowDateTime), executable}, nil
}

func windowsPairPipe(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) || filepath.Clean(path) != path ||
		!strings.EqualFold(filepath.Base(path), "admin.sock") || !strings.EqualFold(filepath.Base(filepath.Dir(path)), "tcp-pair") {
		return "", ErrLocalUnsafe
	}
	return `\\.\pipe\` + path, nil
}

func localLoopbackAddress(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", ErrLocalUnsafe
	}
	ip := net.ParseIP(host)
	n, err := strconv.Atoi(port)
	if ip == nil || err != nil || n < 1 || n > 65535 {
		return "", ErrLocalUnsafe
	}
	if ip.IsUnspecified() {
		if ip.To4() != nil {
			host = "127.0.0.1"
		} else {
			host = "::1"
		}
	} else if !ip.IsLoopback() {
		return "", ErrLocalUnsafe
	}
	return net.JoinHostPort(host, port), nil
}

func windowsAdmin(ctx context.Context, path, action, name string, inspect windowsRelayInspector) (string, windowsRelayProcess, error) {
	pipe, err := windowsPairPipe(path)
	if err != nil {
		return "", windowsRelayProcess{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c, err := winio.DialPipeContext(ctx, pipe)
	if err != nil {
		return "", windowsRelayProcess{}, ErrLocalUnavailable
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	before, err := inspect(c)
	if err != nil {
		return "", windowsRelayProcess{}, err
	}
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if err = json.NewEncoder(c).Encode(Message{"action": action, "name": name}); err != nil {
		return "", windowsRelayProcess{}, ErrLocalUnavailable
	}
	var reply struct{ Protocol, Address, Link, Error string }
	if json.NewDecoder(io.LimitReader(c, 16384)).Decode(&reply) != nil || reply.Error != "" {
		return "", windowsRelayProcess{}, ErrLocalUnavailable
	}
	after, err := inspect(c)
	if err != nil || before != after {
		return "", windowsRelayProcess{}, ErrLocalUnsafe
	}
	if action == "create" {
		return reply.Link, before, nil
	}
	if reply.Protocol != "herdr-local-v1" {
		return "", windowsRelayProcess{}, errors.New("请更新 Windows Relay 后再使用本机连接")
	}
	address, err := localLoopbackAddress(reply.Address)
	return address, before, err
}

func discoverWindowsRelays(ctx context.Context) []LocalRelay {
	pattern, _ := windows.UTF16PtrFromString(`\\.\pipe\*`)
	var entry windows.Win32finddata
	handle, err := windows.FindFirstFile(pattern, &entry)
	if err != nil {
		return nil
	}
	defer windows.FindClose(handle)
	var paths []string
	for {
		path := windows.UTF16ToString(entry.FileName[:])
		if _, err := windowsPairPipe(path); err == nil {
			paths = append(paths, path)
		}
		if len(paths) >= 32 || windows.FindNextFile(handle, &entry) != nil || ctx.Err() != nil {
			break
		}
	}
	var found []LocalRelay
	for _, path := range paths {
		if ctx.Err() != nil {
			break
		}
		address, _, err := windowsAdmin(ctx, path, "info", "", inspectWindowsRelay)
		if err == nil {
			found = append(found, LocalRelay{Socket: path, Label: "Windows · " + address})
		}
	}
	return found
}

func DiscoverLocal(ctx context.Context) ([]LocalRelay, error) {
	type result struct {
		relays []LocalRelay
		err    error
	}
	wsl := make(chan result, 1)
	go func() { relays, err := discoverWSLRelays(ctx); wsl <- result{relays, err} }()
	found := discoverWindowsRelays(ctx)
	r := <-wsl
	found = append(found, r.relays...)
	sort.Slice(found, func(i, j int) bool { return found[i].Label < found[j].Label })
	if len(found) == 0 {
		if r.err != nil {
			return nil, r.err
		}
		return nil, ErrLocalUnavailable
	}
	return found, r.err
}

func PairLocal(ctx context.Context, local LocalRelay, name string) (Computer, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "本机 Relay"
	}
	if len([]rune(name)) > 48 || len(name) > 80 || strings.ContainsAny(name, "\r\n\x00") {
		return Computer{}, errors.New("电脑名称无效")
	}
	var link string
	var err error
	if strings.HasPrefix(local.Socket, "wsl:") {
		link, err = pairWSLRelay(ctx, local.Socket, name)
	} else {
		link, _, err = windowsAdmin(ctx, local.Socket, "create", name, inspectWindowsRelay)
	}
	if err != nil {
		return Computer{}, err
	}
	computer, err := ParseInvitation(link, name, time.Now())
	if err != nil {
		return Computer{}, errors.New("本机 Relay 返回了无效邀请，请升级 Relay")
	}
	computer.LocalSocket = local.Socket
	return computer, nil
}

func dialLocal(ctx context.Context, path string) (logicalWire, error) {
	if strings.HasPrefix(path, "wsl:") {
		return dialWSLRelay(ctx, path)
	}
	address, before, err := windowsAdmin(ctx, path, "info", "", inspectWindowsRelay)
	if err != nil {
		return nil, err
	}
	w, err := dialLoopbackWebSocket(ctx, address)
	if err != nil {
		return nil, err
	}
	addressAfter, after, err := windowsAdmin(ctx, path, "info", "", inspectWindowsRelay)
	if err != nil || before != after || addressAfter != address {
		w.close()
		return nil, ErrLocalUnsafe
	}
	return w, nil
}
