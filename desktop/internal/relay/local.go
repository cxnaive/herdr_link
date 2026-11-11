package relay

import "errors"

// LocalRelay contains only discovery metadata, never invitations or secrets.
// Selecting one does not enroll a device until PairLocal is explicitly called.
type LocalRelay struct {
	Socket string
	Label  string
}

var ErrLocalUnavailable = errors.New("未找到可连接的本机 Relay，请确认 Relay 已在当前用户下启动")
var ErrLocalUnsafe = errors.New("本机 Relay 身份或配对接口已变化，请重新查找")
