package profiles

import (
	"github.com/0cv/herdr-mobile-relay/internal/privatepath"
	"os"
)

func ownershipFileSafe(path string, info os.FileInfo) bool {
	return info.Mode().IsRegular() && privatepath.Private(path, info)
}
func syncOwnershipDirectory(*os.File) error { return nil }
