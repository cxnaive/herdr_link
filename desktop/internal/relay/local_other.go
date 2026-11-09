//go:build !linux

package relay

import "context"

func DiscoverLocal(context.Context) ([]LocalRelay, error) { return nil, ErrLocalUnavailable }
func PairLocal(context.Context, LocalRelay, string) (Computer, error) {
	return Computer{}, ErrLocalUnavailable
}
func dialLocal(context.Context, string) (logicalWire, error) { return nil, ErrLocalUnavailable }
