//go:build !windows

package app

import (
	"os"
	"os/signal"
	"syscall"
)

func notifyProfileReload(c chan os.Signal) { signal.Notify(c, syscall.SIGHUP) }
func notifyBootstrap(c chan os.Signal)     { signal.Notify(c, syscall.SIGUSR1) }
