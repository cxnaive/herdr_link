package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type localProcess struct {
	PID                    int
	Birth, Socket, Address string
}
type localInspector func(int) (localProcess, error)

func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, ErrLocalUnsafe
	}
	return b, nil
}

func inspectRelayProcess(pid int) (localProcess, error) {
	base := fmt.Sprintf("/proc/%d", pid)
	info, err := os.Stat(base)
	if err != nil || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return localProcess{}, ErrLocalUnsafe
	}
	exe, err := os.Readlink(base + "/exe")
	if err != nil || filepath.Base(strings.TrimSuffix(exe, " (deleted)")) != "herdr-mobile-relay" {
		return localProcess{}, ErrLocalUnsafe
	}
	before, err := readBounded(base+"/stat", 8192)
	if err != nil {
		return localProcess{}, ErrLocalUnsafe
	}
	raw, err := readBounded(base+"/environ", 1024*1024)
	if err != nil {
		return localProcess{}, ErrLocalUnsafe
	}
	defer clear(raw)
	env := map[string]string{}
	// Do not retain or log token/registration/device-store material.
	for _, entry := range bytes.Split(raw, []byte{0}) {
		k, v, ok := bytes.Cut(entry, []byte{'='})
		if !ok {
			continue
		}
		switch string(k) {
		case "HERDR_RELAY_ENV", "HERDR_PLUGIN_CONFIG_DIR", "XDG_CONFIG_HOME", "HOME", "HERDR_RELAY_PORT", "HERDR_RELAY_HOST":
			env[string(k)] = string(v)
		}
	}
	process, err := processFromEnvironment(pid, before, env)
	if err != nil {
		return localProcess{}, err
	}
	after, err := readBounded(base+"/stat", 8192)
	afterExe, exeErr := os.Readlink(base + "/exe")
	if err != nil || exeErr != nil || afterExe != exe || processBirth(after) != process.Birth {
		return localProcess{}, ErrLocalUnsafe
	}
	return process, nil
}
func processBirth(stat []byte) string {
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return ""
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 {
		return ""
	}
	if n, err := strconv.ParseUint(fields[19], 10, 64); err != nil || n == 0 {
		return ""
	}
	return fields[19]
}
func processFromEnvironment(pid int, stat []byte, env map[string]string) (localProcess, error) {
	birth := processBirth(stat)
	if birth == "" {
		return localProcess{}, ErrLocalUnsafe
	}
	config := env["XDG_CONFIG_HOME"]
	if config == "" {
		if !filepath.IsAbs(env["HOME"]) {
			return localProcess{}, ErrLocalUnsafe
		}
		config = filepath.Join(env["HOME"], ".config")
	}
	runtime := filepath.Join(config, "herdr-mobile-relay")
	if v := env["HERDR_PLUGIN_CONFIG_DIR"]; v != "" {
		runtime = v
	}
	if v := env["HERDR_RELAY_ENV"]; v != "" {
		if !filepath.IsAbs(v) {
			return localProcess{}, ErrLocalUnsafe
		}
		runtime = filepath.Dir(v)
	}
	if !filepath.IsAbs(runtime) {
		return localProcess{}, ErrLocalUnsafe
	}
	host := env["HERDR_RELAY_HOST"]
	switch host {
	case "", "127.0.0.1", "0.0.0.0", "localhost":
		host = "127.0.0.1"
	case "::1", "::":
		host = "::1"
	default:
		return localProcess{}, ErrLocalUnsafe
	}
	port := 8375
	if v := env["HERDR_RELAY_PORT"]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			return localProcess{}, ErrLocalUnsafe
		}
		port = n
	}
	return localProcess{PID: pid, Birth: birth, Socket: filepath.Join(runtime, "tcp-pair", "admin.sock"), Address: net.JoinHostPort(host, strconv.Itoa(port))}, nil
}

func privateSocket(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "admin.sock" || filepath.Base(filepath.Dir(path)) != "tcp-pair" {
		return nil, ErrLocalUnsafe
	}
	uid := uint32(os.Geteuid())
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uid {
		return nil, ErrLocalUnsafe
	}
	var ancestors []string
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		ancestors = append(ancestors, dir)
		if dir == "/" {
			break
		}
	}
	privateBoundary := false
	for i := len(ancestors) - 1; i >= 0; i-- {
		dir := ancestors[i]
		d, err := os.Lstat(dir)
		if err != nil || !d.IsDir() {
			return nil, ErrLocalUnsafe
		}
		owner := d.Sys().(*syscall.Stat_t).Uid
		if owner != uid && owner != 0 {
			return nil, ErrLocalUnsafe
		}
		if dir == filepath.Dir(path) && (owner != uid || d.Mode().Perm()&0077 != 0) {
			return nil, ErrLocalUnsafe
		}
		// Group-writable plugin directories inside a private ~/.config are not
		// reachable by another user. Before that boundary, refuse unsafe parents.
		if !privateBoundary && d.Mode().Perm()&0022 != 0 && !(owner == 0 && d.Mode()&os.ModeSticky != 0) {
			return nil, ErrLocalUnsafe
		}
		if owner == uid && d.Mode().Perm()&0077 == 0 {
			privateBoundary = true
		}
	}

	return info, nil
}

func connectLocalAdmin(ctx context.Context, path string, inspect localInspector) (*net.UnixConn, localProcess, error) {
	before, err := privateSocket(path)
	if err != nil {
		return nil, localProcess{}, err
	}
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, localProcess{}, ErrLocalUnavailable
	}
	c := conn.(*net.UnixConn)
	fail := func() (*net.UnixConn, localProcess, error) { c.Close(); return nil, localProcess{}, ErrLocalUnsafe }
	raw, err := c.SyscallConn()
	if err != nil {
		return fail()
	}
	var cred *syscall.Ucred
	var peerErr error
	if err = raw.Control(func(fd uintptr) {
		cred, peerErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil || peerErr != nil || cred == nil || cred.Uid != uint32(os.Geteuid()) {
		return fail()
	}
	p, err := inspect(int(cred.Pid))
	if err != nil || p.Socket != path || p.PID != int(cred.Pid) {
		return fail()
	}
	after, err := privateSocket(path)
	if err != nil || !os.SameFile(before, after) {
		return fail()
	}
	return c, p, nil
}

func DiscoverLocal(ctx context.Context) ([]LocalRelay, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, ErrLocalUnavailable
	}
	found := map[string]LocalRelay{}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		p, err := inspectRelayProcess(pid)
		if err != nil {
			continue
		}
		if _, ok := found[p.Socket]; ok {
			continue
		}
		conn, current, err := connectLocalAdmin(ctx, p.Socket, inspectRelayProcess)
		if err != nil {
			continue
		}
		conn.Close()
		if current != p {
			continue
		}
		found[p.Socket] = LocalRelay{Socket: p.Socket, Label: "本机 Relay · " + p.Address}
		if len(found) >= 32 {
			break
		}
	}
	result := make([]LocalRelay, 0, len(found))
	for _, r := range found {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Socket < result[j].Socket })
	if len(result) == 0 {
		return nil, ErrLocalUnavailable
	}
	return result, nil
}

func PairLocal(ctx context.Context, local LocalRelay, name string) (Computer, error) {
	return pairLocal(ctx, local, name, inspectRelayProcess)
}
func pairLocal(ctx context.Context, local LocalRelay, name string, inspect localInspector) (Computer, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "本机 Relay"
	}
	if len([]rune(name)) > 48 || len(name) > 80 || strings.ContainsAny(name, "\r\n\x00") {
		return Computer{}, errors.New("电脑名称无效")
	}
	initial, err := privateSocket(local.Socket)
	if err != nil {
		return Computer{}, err
	}
	c, p, err := connectLocalAdmin(ctx, local.Socket, inspect)
	if err != nil {
		return Computer{}, err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if err = json.NewEncoder(c).Encode(Message{"action": "create", "name": name}); err != nil {
		return Computer{}, ErrLocalUnavailable
	}
	var reply struct {
		Link  string `json:"link"`
		Error string `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(c, 16384)).Decode(&reply) != nil || reply.Error != "" {
		return Computer{}, ErrLocalUnavailable
	}
	after, err := inspect(p.PID)
	if err != nil || after != p {
		return Computer{}, ErrLocalUnsafe
	}
	final, err := privateSocket(local.Socket)
	if err != nil || !os.SameFile(initial, final) {
		return Computer{}, ErrLocalUnsafe
	}
	computer, err := ParseInvitation(reply.Link, name, time.Now())
	if err != nil {
		return Computer{}, errors.New("本机 Relay 返回了无效邀请，请升级 Relay")
	}
	computer.LocalSocket = local.Socket
	return computer, nil
}

func dialLocal(ctx context.Context, path string) (logicalWire, error) {
	c, p, err := connectLocalAdmin(ctx, path, inspectRelayProcess)
	if err != nil {
		return nil, err
	}
	c.Close()
	w, err := dialLoopbackWebSocket(ctx, p.Address)
	if err != nil {
		return nil, err
	}
	// No credential handshake may be sent for a process that changed during dial.
	c, after, err := connectLocalAdmin(ctx, path, inspectRelayProcess)
	if err != nil {
		w.close()
		return nil, err
	}
	c.Close()
	if after != p {
		w.close()
		return nil, ErrLocalUnsafe
	}
	return w, nil
}
