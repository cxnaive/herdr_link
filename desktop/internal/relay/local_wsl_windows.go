package relay

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
)

//go:embed wsl_bridge.py
var wslBridge string

type wslTarget struct {
	Distro string `json:"distro"`
	User   string `json:"user"`
	UID    int    `json:"uid"`
	Socket string `json:"socket"`
}

func (target wslTarget) route() string {
	data, _ := json.Marshal(target)
	return "wsl:" + base64.RawURLEncoding.EncodeToString(data)
}

func parseWSLRoute(path string) (wslTarget, error) {
	var target wslTarget
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(path, "wsl:"))
	if err != nil || len(data) > 8192 || json.Unmarshal(data, &target) != nil ||
		target.Distro == "" || target.User == "" || target.UID < 0 || !strings.HasPrefix(target.Socket, "/") ||
		strings.ContainsAny(target.Distro+target.User+target.Socket, "\x00\r\n") {
		return target, ErrLocalUnsafe
	}
	return target, nil
}

func wslCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "wsl.exe"), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

func runningWSLDistros(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := wslCommand("--list", "--running", "--quiet")
	var output limitedOutput
	cmd.Stdout = &output
	if err := cmd.Start(); err != nil {
		return nil, nil
	}
	stop := context.AfterFunc(ctx, func() { cmd.Process.Kill() })
	defer stop()
	if cmd.Wait() != nil {
		return nil, nil
	}
	return parseWSLDistros(output.Bytes()), nil
}

// wsl.exe's list output is UTF-16LE on older Windows and UTF-8 on newer builds.
func parseWSLDistros(data []byte) []string {
	text := string(data)
	if bytes.Contains(data, []byte{0}) || bytes.HasPrefix(data, []byte{0xff, 0xfe}) {
		if len(data)%2 != 0 {
			return nil
		}
		words := make([]uint16, len(data)/2)
		for i := range words {
			words[i] = binary.LittleEndian.Uint16(data[i*2:])
		}
		text = string(utf16.Decode(words))
	}
	text = strings.TrimPrefix(text, "\ufeff")
	var distros []string
	for _, line := range strings.Split(text, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.ContainsRune(name, 0) {
			continue
		}
		distros = append(distros, name)
		if len(distros) == 32 {
			break
		}
	}
	return distros
}

type limitedOutput struct{ data bytes.Buffer }

func (b *limitedOutput) Bytes() []byte { return b.data.Bytes() }

func (b *limitedOutput) Write(data []byte) (int, error) {
	if b.data.Len()+len(data) > 1024*1024 {
		return 0, errors.New("WSL response too large")
	}
	return b.data.Write(data)
}

type wslBridgeReply struct {
	Link    string `json:"link"`
	Address string `json:"address"`
	Relays  []struct {
		Socket  string `json:"socket"`
		Address string `json:"address"`
		User    string `json:"user"`
		UID     int    `json:"uid"`
	} `json:"relays"`
}

func bridgeCommand(target wslTarget) *exec.Cmd {
	args := []string{"--distribution", target.Distro}
	if target.User != "" {
		args = append(args, "--user", target.User)
	}
	return wslCommand(append(args, "--exec", "python3", "-I", "-c", wslBridge)...)
}

func bridgeRequest(target wslTarget, action, name string) []byte {
	data, _ := json.Marshal(Message{"action": action, "socket": target.Socket, "user": target.User, "uid": target.UID, "name": name})
	return append(data, '\n')
}

func queryWSLBridge(ctx context.Context, target wslTarget, action, name string) (wslBridgeReply, error) {
	var reply wslBridgeReply
	cmd := bridgeCommand(target)
	cmd.Stdin = bytes.NewReader(bridgeRequest(target, action, name))
	var output limitedOutput
	cmd.Stdout = &output
	if err := cmd.Start(); err != nil {
		return reply, ErrLocalUnavailable
	}
	stop := context.AfterFunc(ctx, func() { cmd.Process.Kill() })
	defer stop()
	if cmd.Wait() != nil || json.Unmarshal(output.Bytes(), &reply) != nil {
		return reply, ErrLocalUnavailable
	}
	return reply, nil
}

func discoverWSLRelays(ctx context.Context) ([]LocalRelay, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	distros, err := runningWSLDistros(ctx)
	if err != nil {
		return nil, err
	}
	type result struct {
		relays []LocalRelay
		failed bool
	}
	results := make(chan result, len(distros))
	for _, distro := range distros {
		go func(distro string) {
			probe, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			reply, err := queryWSLBridge(probe, wslTarget{Distro: distro}, "discover", "")
			r := result{failed: err != nil}
			for _, candidate := range reply.Relays {
				target := wslTarget{distro, candidate.User, candidate.UID, candidate.Socket}
				if _, err := parseWSLRoute(target.route()); err != nil {
					continue
				}
				r.relays = append(r.relays, LocalRelay{Socket: target.route(), Label: "WSL · " + distro + " · " + candidate.Address})
			}
			results <- r
		}(distro)
	}
	var found []LocalRelay
	failed := false
	for range distros {
		r := <-results
		found = append(found, r.relays...)
		failed = failed || r.failed
	}
	if failed {
		return found, errors.New("部分 WSL 无法检查，请确认发行版可访问且已安装 Python 3.9 或更新版本，然后重试")
	}
	return found, nil
}

func activeWSLTarget(ctx context.Context, path string) (wslTarget, error) {
	target, err := parseWSLRoute(path)
	if err != nil {
		return target, err
	}
	distros, err := runningWSLDistros(ctx)
	if err != nil {
		return target, err
	}
	for _, distro := range distros {
		if distro == target.Distro {
			return target, nil
		}
	}
	return target, errors.New("此 WSL 发行版未运行，请先启动其中的 Relay")
}

func pairWSLRelay(ctx context.Context, path, name string) (string, error) {
	target, err := activeWSLTarget(ctx, path)
	if err != nil {
		return "", err
	}
	reply, err := queryWSLBridge(ctx, target, "pair", name)
	return reply.Link, err
}

func dialWSLRelay(ctx context.Context, path string) (logicalWire, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	target, err := activeWSLTarget(ctx, path)
	if err != nil {
		return nil, err
	}
	cmd := bridgeCommand(target)
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, ErrLocalUnavailable
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, ErrLocalUnavailable
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, ErrLocalUnavailable
	}
	var once sync.Once
	closeBridge := func() { once.Do(func() { input.Close(); output.Close(); cmd.Process.Kill() }) }
	go func() { cmd.Wait(); closeBridge() }()
	stop := context.AfterFunc(ctx, closeBridge)
	defer stop()
	if _, err = input.Write(bridgeRequest(target, "connect", "")); err != nil {
		closeBridge()
		return nil, ErrLocalUnavailable
	}
	reader := bufio.NewReaderSize(output, 16384)
	line, err := reader.ReadSlice('\n')
	var reply wslBridgeReply
	if err != nil || json.Unmarshal(line, &reply) != nil {
		closeBridge()
		return nil, ErrLocalUnavailable
	}
	if _, err = localLoopbackAddress(reply.Address); err != nil {
		closeBridge()
		return nil, err
	}
	client, bridge := net.Pipe()
	go func() { defer bridge.Close(); defer closeBridge(); io.Copy(input, bridge) }()
	go func() { defer bridge.Close(); defer closeBridge(); io.Copy(bridge, reader) }()
	w, err := dialLocalWebSocket(ctx, reply.Address, func(context.Context, string, string) (net.Conn, error) { return client, nil })
	if err != nil {
		client.Close()
		closeBridge()
		return nil, err
	}
	return w, nil
}
