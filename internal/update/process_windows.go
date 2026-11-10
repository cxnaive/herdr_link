package update

import (
	"github.com/0cv/herdr-mobile-relay/internal/processjob"
	"os/exec"
)

func startCommand(cmd *exec.Cmd) (func(), error)            { return processjob.Start(cmd) }
func terminateProcessGroup(pid int, wait <-chan error) bool { return processjob.Terminate(pid, wait) }
func processGroupAlive(pid int) bool                        { return processjob.Alive(pid) }
