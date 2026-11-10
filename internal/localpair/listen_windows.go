package localpair

import (
	"errors"
	"github.com/0cv/herdr-mobile-relay/internal/localipc"
	"github.com/0cv/herdr-mobile-relay/internal/privatepath"
	"github.com/Microsoft/go-winio"
	"net"
	"os"
	"path/filepath"
)

func listenPairing(runtime string) (net.Listener, error) {
	dir := filepath.Dir(socketPath(runtime))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || !privatepath.Private(dir, info) {
		return nil, errors.New("pairing directory must have a private Windows ACL")
	}
	sid, err := privatepath.CurrentSID()
	if err != nil {
		return nil, err
	}
	name, err := localipc.WindowsPipeName(socketPath(runtime))
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(name, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + sid.String() + ")(A;;GA;;;SY)", InputBufferSize: 4096, OutputBufferSize: 16384})
}
