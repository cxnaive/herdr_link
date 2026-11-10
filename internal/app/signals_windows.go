package app

import "os"

// Windows has no Unix reload/bootstrap signals; pairing uses the private IPC API.
func notifyProfileReload(chan os.Signal) {}
func notifyBootstrap(chan os.Signal)     {}
