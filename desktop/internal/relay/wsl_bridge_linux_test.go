package relay

import (
	"os/exec"
	"testing"
)

func TestWSLBridgePrivateDiscoveryGuards(t *testing.T) {
	output, err := exec.Command("python3", "-I", "wsl_bridge_test.py").CombinedOutput()
	if err != nil {
		t.Fatalf("WSL helper: %v\n%s", err, output)
	}
}
