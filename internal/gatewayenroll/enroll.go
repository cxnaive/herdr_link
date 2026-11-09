// Package gatewayenroll exchanges a short-lived computer invitation over TLS on
// the existing native TCP port. The out-of-band code pins the server certificate.
package gatewayenroll

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/gatewaywire"
	"github.com/0cv/herdr-mobile-relay/internal/nativeconn"
)

var ErrEnrollment = errors.New("gateway enrollment failed; check invitation, expiry and gateway availability")

type Code struct {
	Endpoint, ID, Secret, Pin string
	ExpiresAt                 int64
}
type Request struct {
	ID        string `json:"id"`
	Secret    string `json:"secret"`
	RelayID   string `json:"relay_id"`
	RequestID string `json:"request_id"`
}
type Response struct {
	Token string `json:"token,omitempty"`
	Error string `json:"error,omitempty"`
}

func RandomSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func Encode(c Code) string {
	u, _ := url.Parse(c.Endpoint)
	u.Scheme = "herdr-gateway"
	u.Fragment = url.Values{"id": {c.ID}, "secret": {c.Secret}, "pin": {c.Pin}, "expires": {strconv.FormatInt(c.ExpiresAt, 10)}}.Encode()
	return u.String()
}
func Parse(raw string, now time.Time) (Code, error) {
	if len(raw) > 4096 {
		return Code{}, ErrEnrollment
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "herdr-gateway" || u.User != nil || u.RawQuery != "" || u.ForceQuery {
		return Code{}, ErrEnrollment
	}
	p, err := url.ParseQuery(u.Fragment)
	if err != nil || len(p) != 4 {
		return Code{}, ErrEnrollment
	}
	for _, v := range p {
		if len(v) != 1 {
			return Code{}, ErrEnrollment
		}
	}
	expires, err := strconv.ParseInt(p.Get("expires"), 10, 64)
	if err != nil || expires <= now.UnixMilli() {
		return Code{}, ErrEnrollment
	}
	id, e := base64.RawURLEncoding.DecodeString(p.Get("id"))
	secret, e2 := base64.RawURLEncoding.DecodeString(p.Get("secret"))
	pin, e3 := hex.DecodeString(p.Get("pin"))
	if e != nil || len(id) != 18 || e2 != nil || len(secret) != 32 || e3 != nil || len(pin) != 32 {
		return Code{}, ErrEnrollment
	}
	u.Scheme = "tcp"
	u.Fragment = ""
	if _, err := nativeconn.Endpoint(u.String()); err != nil {
		return Code{}, ErrEnrollment
	}
	return Code{Endpoint: u.String(), ID: p.Get("id"), Secret: p.Get("secret"), Pin: p.Get("pin"), ExpiresAt: expires}, nil
}
func Redeem(ctx context.Context, code Code, relayID, requestID string) (string, error) {
	if !gatewaywire.ValidRelayID(relayID) {
		return "", ErrEnrollment
	}
	address, err := nativeconn.Endpoint(code.Endpoint)
	if err != nil {
		return "", ErrEnrollment
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return "", ErrEnrollment
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err = conn.Write(append([]byte(nativeconn.Magic), nativeconn.RoleEnroll)); err != nil {
		return "", ErrEnrollment
	}
	pin, err := hex.DecodeString(code.Pin)
	if err != nil || len(pin) != 32 {
		return "", ErrEnrollment
	}
	secure := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true, // Certificate identity is authenticated by the invitation fingerprint, not DNS PKI.
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) != 1 {
				return ErrEnrollment
			}
			actual := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if subtle.ConstantTimeCompare(actual[:], pin) != 1 {
				return ErrEnrollment
			}
			return nil
		},
	})
	if secure.HandshakeContext(ctx) != nil {
		return "", ErrEnrollment
	}
	defer secure.Close()
	if json.NewEncoder(secure).Encode(Request{code.ID, code.Secret, relayID, requestID}) != nil {
		return "", ErrEnrollment
	}
	var response Response
	if json.NewDecoder(io.LimitReader(secure, 8192)).Decode(&response) != nil || response.Error != "" {
		return "", ErrEnrollment
	}
	token, e := base64.RawURLEncoding.DecodeString(response.Token)
	if e != nil || len(token) != 32 {
		return "", ErrEnrollment
	}
	return response.Token, nil
}
