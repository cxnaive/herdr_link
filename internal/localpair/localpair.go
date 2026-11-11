// Package localpair creates one-use invitations through the running relay's
// private Unix socket. It never opens the device store in a second process.
package localpair

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0cv/herdr-mobile-relay/internal/localipc"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/deviceauth"
	"github.com/0cv/herdr-mobile-relay/internal/gatewaywire"
	"github.com/0cv/herdr-mobile-relay/internal/nativeconn"
)

type request struct {
	Action string `json:"action"`
	Name   string `json:"name"`
}
type Response struct {
	Link      string    `json:"link,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Error     string    `json:"error,omitempty"`
}

func socketPath(runtime string) string { return filepath.Join(runtime, "tcp-pair", "admin.sock") }

func InvitationLink(endpoint, token string, invitation deviceauth.Invitation) (string, error) {
	address, err := nativeconn.Endpoint(endpoint)
	if err != nil {
		return "", err
	}
	id, err := gatewaywire.DeriveRelayID(token)
	if err != nil {
		return "", err
	}
	key, err := gatewaywire.DeriveRendezvousKey(token)
	if err != nil {
		return "", err
	}
	values := url.Values{"setup": {invitation.Secret}, "invite": {invitation.InvitationID}, "invite_version": {strconv.FormatUint(invitation.Version, 10)}, "invite_expires": {strconv.FormatInt(invitation.ExpiresAt.UnixMilli(), 10)}, "relay_id": {id}, "rendezvous": {base64.RawURLEncoding.EncodeToString(key)}, "label": {invitation.Name}}
	return "herdr-tcp://" + address + "/#" + values.Encode(), nil
}

func Listen(runtime, address string, create func(string) (Response, error)) (func(), error) {
	listener, err := listenPairing(runtime)
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				var req request
				dec := json.NewDecoder(io.LimitReader(conn, 4096))
				dec.DisallowUnknownFields()
				if dec.Decode(&req) != nil {
					return
				}
				if req.Action == "info" {
					_ = json.NewEncoder(conn).Encode(map[string]string{"protocol": "herdr-local-v1", "address": address})
					return
				}
				if req.Action != "create" {
					return
				}
				response, err := create(req.Name)
				if err != nil {
					response = Response{Error: "Unable to create invitation"}
				}
				_ = json.NewEncoder(conn).Encode(response)
			}()
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { _ = listener.Close(); <-done }) }, nil
}

func Create(ctx context.Context, runtime, name string) (Response, error) {
	conn, err := localipc.DialContext(ctx, "unix", socketPath(runtime))
	if err != nil {
		return Response{}, fmt.Errorf("connect to running TCP relay: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(request{Action: "create", Name: name}); err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.NewDecoder(io.LimitReader(conn, 16384)).Decode(&response); err != nil {
		return Response{}, err
	}
	if response.Error != "" {
		return Response{}, errors.New(response.Error)
	}
	if response.Link == "" {
		return Response{}, errors.New("empty invitation")
	}
	return response, nil
}
