//go:build !windows

package localipc

import (
	"context"
	"net"
)

func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, address)
}
