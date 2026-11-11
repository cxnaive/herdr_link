package localpair

import (
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/0cv/herdr-mobile-relay/internal/localipc"
	"github.com/0cv/herdr-mobile-relay/internal/privatepath"
)

func TestInfoDoesNotCreateInvitationOrDiscloseSecrets(t *testing.T) {
	runtime := t.TempDir()
	if err := privatepath.Protect(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	stop, err := Listen(runtime, "127.0.0.1:38475", func(string) (Response, error) {
		calls.Add(1)
		return Response{Link: "private invitation"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	c, err := localipc.DialContext(t.Context(), "unix", socketPath(runtime))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = json.NewEncoder(c).Encode(request{Action: "info"}); err != nil {
		t.Fatal(err)
	}
	var reply map[string]any
	if err = json.NewDecoder(c).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	if len(reply) != 2 || reply["protocol"] != "herdr-local-v1" || reply["address"] != "127.0.0.1:38475" || calls.Load() != 0 {
		t.Fatal("info must only return the protocol and bound address")
	}
}
