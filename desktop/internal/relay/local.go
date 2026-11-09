package relay

import "errors"

// LocalRelay contains only discovery metadata, never invitations or secrets.
// Selecting one does not enroll a device until PairLocal is explicitly called.
type LocalRelay struct {
	Socket string
	Label  string
}

var ErrLocalUnavailable = errors.New("未找到当前用户可安全连接的本机 Relay；请确认原生 TCP Relay 已启动并提供私有配对 socket")
var ErrLocalUnsafe = errors.New("本机 Relay 身份或私有 socket 已变化，请重新查找；不会改走远程网关")
