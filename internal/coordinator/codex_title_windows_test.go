package coordinator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/childenv"
)

// Exercise the npm shim's script -> native argv boundary with Windows PowerShell
// (powershell.exe, not pwsh). The native receiver only reports argv; no Codex login.
func TestWindowsPowerShellCodexTitleArgv(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "codex.ps1")
	if err := os.WriteFile(shim, []byte(`& node.exe "$PSScriptRoot/probe.cjs" @args
exit $LASTEXITCODE
`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.cjs"), []byte(`console.log(JSON.stringify(process.argv.slice(2)))`), 0600); err != nil {
		t.Fatal(err)
	}
	args, err := (StartRequest{AgentArgs: []string{"--model", "demo model"}}).launchArgs("codex")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_TEST_CODEX_SHIM", shim)
	t.Setenv("HERDR_TEST_LAUNCH_ARGS", string(data))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	out, err := childenv.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "RemoteSigned", "-Command", `
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion.Major -ne 5 -or $PSVersionTable.PSVersion.Minor -ne 1) { throw 'expected Windows PowerShell 5.1' }
[string[]]$launch = ConvertFrom-Json -InputObject $env:HERDR_TEST_LAUNCH_ARGS
& $env:HERDR_TEST_CODEX_SHIM @launch
exit $LASTEXITCODE`).CombinedOutput()
	if err != nil {
		t.Fatalf("PowerShell shim: %v: %s", err, out)
	}
	want := []string{"-c", `tui.terminal_title=['thread-id','thread-name']`, "--model", "demo model"}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil || !slices.Equal(got, want) {
		t.Fatalf("native argv = %s, want %q (%v)", out, want, err)
	}
}
