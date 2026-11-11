package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// The fixture owns only its temporary Relay. Closing stdin tears it down.
const wslRelayFixture = `
import json, os, pathlib, pwd, shutil, socket, subprocess, sys, tempfile, time
root = tempfile.mkdtemp(prefix="herdr-wsl-client-")
process = None
def port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return str(s.getsockname()[1])
def stop():
    global process
    if process is not None:
        process.terminate()
        try: process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
        process = None
try:
    path = root + "/c/herdr-mobile-relay/tcp-pair/admin.sock"
    while True:
        env = dict(PATH=os.environ["PATH"], HOME=root, XDG_CONFIG_HOME=root+"/c", XDG_DATA_HOME=root+"/data", XDG_CACHE_HOME=root+"/cache",
            HERDR_BIN="/bin/false", HERDR_SOCKET_PATH=root+"/absent.sock", HERDR_RELAY_TOKEN="W"*32, HERDR_GATEWAY_REGISTRATION_TOKEN="R"*32,
            HERDR_RELAY_HOST="127.0.0.1", HERDR_RELAY_PORT=port(), HERDR_RELAY_PLUGIN_PORT=port(), HERDR_GATEWAY_URL="tcp://127.0.0.1:"+port(),
            HERDR_WEB_ROOT=root+"/absent-web", HERDR_REACHABILITY_PORT_MAPPING="false")
        with open(root+"/relay.log", "ab") as log:
            process = subprocess.Popen([sys.argv[1], "serve"], env=env, stdout=log, stderr=log)
        for _ in range(200):
            if pathlib.Path(path).exists(): break
            if process.poll() is not None: raise RuntimeError("fixture Relay exited")
            time.sleep(.05)
        else: raise RuntimeError("fixture timeout")
        print(json.dumps(dict(socket=path, uid=os.geteuid(), user=pwd.getpwuid(os.geteuid()).pw_name)), flush=True)
        if sys.stdin.readline().strip() != "restart": break
        stop()
finally:
    stop()
    shutil.rmtree(root)
`

func TestWSLActualRelayDiscoveryEnrollmentAndRestart(t *testing.T) {
	distro, binary := os.Getenv("HERDR_TEST_WSL_DISTRO"), os.Getenv("HERDR_TEST_WSL_RELAY_BINARY")
	if distro == "" || binary == "" {
		t.Skip("set HERDR_TEST_WSL_DISTRO and HERDR_TEST_WSL_RELAY_BINARY for isolated WSL integration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	distros, _ := runningWSLDistros(ctx)
	running := false
	for _, name := range distros {
		running = running || name == distro
	}
	if !running {
		t.Fatal("integration requires an already running WSL distro")
	}
	cmd := wslCommand("--distribution", distro, "--exec", "python3", "-I", "-c", wslRelayFixture, binary)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { input.Close(); cmd.Wait() }()
	reader := bufio.NewReader(output)
	target := wslTarget{Distro: distro}
	readTarget := func() {
		line, err := reader.ReadBytes('\n')
		if err != nil || json.Unmarshal(line, &target) != nil {
			t.Fatal("WSL fixture failed to start")
		}
	}
	readTarget()
	found, _ := discoverWSLRelays(ctx)
	matched := false
	for _, local := range found {
		matched = matched || local.Socket == target.route()
	}
	if !matched {
		t.Fatal("WSL Relay not discovered")
	}
	computer, err := PairLocal(ctx, LocalRelay{Socket: target.route()}, "WSL 中文 'quoted' client")
	if err != nil {
		t.Fatal(err)
	}
	computer.Address = "192.0.2.1:1"
	saved := computer.Auth
	verify := func() {
		t.Helper()
		session, err := Connect(ctx, computer, func(auth Auth) error { saved = auth; return nil })
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		reply, err := session.Request(ctx, Message{"type": "device_list"})
		if err != nil {
			t.Fatal(err)
		}
		rows, ok := reply["devices"].([]any)
		if !ok || len(rows) != 1 || Text(Object(rows[0]), "device_id") != saved.DeviceID {
			t.Fatal("independent WSL device identity not preserved")
		}
	}
	verify()
	computer.Auth = saved
	input.Write([]byte("restart\n"))
	readTarget()
	verify()
	bad := computer
	bad.Auth.Secret = encode(make([]byte, 32))
	if session, err := Connect(ctx, bad, func(Auth) error { t.Fatal("bad credential was replaced"); return nil }); err == nil {
		session.Close()
		t.Fatal("wrong WSL credential accepted")
	}
	// A saved route cannot be rebound to the distro's changed default user.
	target.UID++
	if _, err := queryWSLBridge(ctx, target, "pair", strings.Repeat("x", 2)); err == nil {
		t.Fatal("different WSL UID accepted")
	}
}
