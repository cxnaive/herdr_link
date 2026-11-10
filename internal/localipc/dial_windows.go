package localipc

import (
	"context"
	"errors"
	"github.com/0cv/herdr-mobile-relay/internal/privatepath"
	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"net"
)

func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "unix" {
		return nil, errors.New("only local Herdr endpoints are supported")
	}
	name, err := WindowsPipeName(address)
	if err != nil {
		return nil, err
	}
	conn, err := winio.DialPipeContext(ctx, name)
	if err != nil {
		return nil, err
	}
	fd, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		conn.Close()
		return nil, errors.New("pipe has no verifiable server handle")
	}
	var pid uint32
	err = windows.GetNamedPipeServerProcessId(windows.Handle(fd.Fd()), &pid)
	if err == nil {
		err = checkServerUser(pid)
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func checkServerUser(pid uint32) error {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err = windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	self, err := privatepath.CurrentSID()
	if err != nil {
		return err
	}
	if !self.Equals(user.User.Sid) {
		return errors.New("Herdr pipe belongs to another Windows user")
	}
	return nil
}
