package relay

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Auth struct {
	Kind      string
	ID        string
	Version   int64
	Secret    string
	ExpiresAt int64
	DeviceID  string
	Role      string
}

type Computer struct {
	LocalSocket string
	ID          string
	Name        string
	Address     string
	RelayID     string
	Rendezvous  string
	Auth        Auth
}

func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func decode(s string, size int) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != size {
		return nil, errors.New("invalid encoded key")
	}
	return b, nil
}

func ParseInvitation(value, name string, now time.Time) (Computer, error) {
	fail := func() (Computer, error) {
		return Computer{}, errors.New("请粘贴有效的 herdr-tcp 一次性邀请链接")
	}
	if len(value) > 16384 {
		return fail()
	}
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Scheme != "herdr-tcp" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || (u.Path != "" && u.Path != "/") {
		return fail()
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return fail()
	}
	p, err := url.ParseQuery(u.Fragment)
	if err != nil {
		return fail()
	}
	for _, v := range p {
		if len(v) != 1 {
			return fail()
		}
	}
	for _, k := range []string{"gateway", "gateways", "relay"} {
		if _, ok := p[k]; ok {
			return fail()
		}
	}
	version, e1 := strconv.ParseInt(p.Get("invite_version"), 10, 64)
	expires, e2 := strconv.ParseInt(p.Get("invite_expires"), 10, 64)
	if e1 != nil || e2 != nil || version < 1 || expires <= now.UnixMilli() || !regexp.MustCompile("^[A-Za-z0-9_-]{16,128}$").MatchString(p.Get("invite")) {
		return fail()
	}
	for k, size := range map[string]int{"setup": 32, "rendezvous": 32, "relay_id": 16} {
		if _, err := decode(p.Get(k), size); err != nil {
			return fail()
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(p.Get("label"))
	}
	if name == "" {
		name = u.Hostname()
	}
	if len([]rune(name)) > 48 || strings.ContainsAny(name, "\r\n\x00") {
		return fail()
	}
	return Computer{ID: NewID(), Name: name, Address: net.JoinHostPort(u.Hostname(), u.Port()), RelayID: p.Get("relay_id"), Rendezvous: p.Get("rendezvous"),
		Auth: Auth{Kind: "invitation", ID: p.Get("invite"), Version: version, Secret: p.Get("setup"), ExpiresAt: expires}}, nil
}
