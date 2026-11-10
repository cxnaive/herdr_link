package herdr

import (
	"github.com/0cv/herdr-mobile-relay/internal/processjob"
	"os/exec"
)

func startCommand(cmd *exec.Cmd) (func(), error)       { return processjob.Start(cmd) }
func terminateProcessGroup(pid int, wait <-chan error) { processjob.Terminate(pid, wait) }
func GroupAlive(pid int) bool                          { return processjob.Alive(pid) }
