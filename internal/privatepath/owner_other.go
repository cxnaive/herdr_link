//go:build !linux && !darwin

package privatepath

import "os"

// Gateway credential management is enabled only where private Unix ownership is
// verified. Other existing transports remain available on their original paths.
func Owned(os.FileInfo) bool { return false }
