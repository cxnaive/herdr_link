//go:build !linux && !windows

package app

import (
	"github.com/0cv/herdr-mobile-relay/internal/herdr"
	"path/filepath"
)

func codexForegroundGroup(*herdr.PaneProcessInfo, string) (herdr.PaneProcess, error) {
	return herdr.PaneProcess{}, errCodexProcessStart
}

func codexRuntimeForegroundProcess(info *herdr.PaneProcessInfo, pane string) (herdr.PaneProcess, error) {
	return codexForegroundProcess(info, pane)
}
func codexProcessHome(_ int, home string) (string, error) { return filepath.Join(home, ".codex"), nil }

func codexProcessProofSupported() bool { return false }
