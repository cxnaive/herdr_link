package localipc

import (
	"context"
	"github.com/Microsoft/go-winio"
	"io"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsNamedPipeReadWriteAndDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "herdr.sock")
	name, err := WindowsPipeName(path)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := winio.ListenPipe(name, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		c, e := listener.Accept()
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		b := make([]byte, 4)
		_, e = io.ReadFull(c, b)
		if e == nil {
			_, e = c.Write(b)
		}
		done <- e
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	c, err := DialContext(ctx, "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	if _, err = c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 4)
	if _, err = io.ReadFull(c, b); err != nil || string(b) != "ping" {
		t.Fatalf("echo failed: %q %v", b, err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	cancelled, stop := context.WithCancel(t.Context())
	stop()
	if _, err = DialContext(cancelled, "unix", path+"absent"); err == nil {
		t.Fatal("cancelled dial succeeded")
	}
}
