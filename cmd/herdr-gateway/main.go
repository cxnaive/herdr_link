// Command herdr-gateway runs the blind WebSocket rendezvous gateway that pairs
// Herdr computer relays with their phones. It sees only ciphertext and may hold
// a separate registration secret. It is configured entirely through the
// environment so a container needs no command line.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/0cv/herdr-mobile-relay/internal/gateway"
	"github.com/0cv/herdr-mobile-relay/internal/gatewayaccess"
	"github.com/0cv/herdr-mobile-relay/internal/gatewaywire"
)

// drainTimeout bounds how long the HTTP server is given to finish in-flight
// requests after the relay links have been dropped.
const drainTimeout = 10 * time.Second

var (
	version  = "dev"
	revision = "unknown"
)

func main() {
	if err := gatewayCommand(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func gatewayCommand(args []string, out io.Writer) error {
	if len(args) == 0 {
		return run()
	}
	if args[0] != "admin-init" && args[0] != "admin-reset" {
		return errors.New("usage: herdr-gateway [admin-init|admin-reset --state-dir PRIVATE_DIRECTORY]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("state-dir", "", "private management directory")
	if flags.Parse(args[1:]) != nil || *dir == "" || flags.NArg() != 0 {
		return errors.New("usage: herdr-gateway admin-init --state-dir PRIVATE_DIRECTORY")
	}
	var password string
	var err error
	if args[0] == "admin-reset" {
		password, err = gatewayaccess.ResetAdmin(*dir)
	} else {
		password, err = gatewayaccess.Init(*dir)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Gateway management initialized. Store this administrator key securely; it is shown only once:\n%s\n", password)
	return err
}

func run() error {
	addr := envString("HERDR_GATEWAY_ADDR", ":8443")

	logger, err := newLogger(envString("HERDR_GATEWAY_LOG_FORMAT", "text"))
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	opts, err := loadOptions()
	if err != nil {
		return err
	}
	opts.Logger = logger
	opts.TCPAddr = os.Getenv("HERDR_GATEWAY_TCP_ADDR")

	if state := os.Getenv("HERDR_GATEWAY_ADMIN_STATE"); state != "" {
		store, e := gatewayaccess.Open(state)
		if e != nil {
			return e
		}
		defer store.Close()
		opts.Access = store
		if opts.StatePath == "" {
			opts.StatePath = filepath.Join(state, "traffic.json")
		}
	} else if os.Getenv("HERDR_GATEWAY_ADMIN_ADDR") != "" {
		return errors.New("initialize and set HERDR_GATEWAY_ADMIN_STATE before enabling the admin listener")
	}
	srv, err := gateway.New(opts)
	if err != nil {
		return err
	}

	if srv.TCPAddress() != "" {
		logger.Info("native TCP gateway listening", "addr", srv.TCPAddress())
	}

	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv.Handler(),
		// Read and write deadlines are deliberately absent: both WebSocket
		// routes are long lived. Only the header read is bounded.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	var adminServer *http.Server
	var adminListener net.Listener
	if opts.Access != nil {
		adminAddr := envString("HERDR_GATEWAY_ADMIN_ADDR", "127.0.0.1:18378")
		host, _, e := net.SplitHostPort(adminAddr)
		if e != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			srv.Close()
			return errors.New("admin listener must bind a literal loopback address; use an HTTPS reverse proxy for remote access")
		}
		origin := envString("HERDR_GATEWAY_ADMIN_ORIGIN", "http://"+adminAddr)
		handler, e := gateway.NewAdminHandler(srv, origin, os.Getenv("HERDR_GATEWAY_PUBLIC_ENDPOINT"))
		if e != nil {
			srv.Close()
			return e
		}
		adminListener, e = net.Listen("tcp", adminAddr)
		if e != nil {
			srv.Close()
			return e
		}
		adminServer = &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 2)
	if adminServer != nil {
		go func() {
			logger.Info("gateway admin listening", "addr", adminListener.Addr().String())
			serveErr <- adminServer.Serve(adminListener)
		}()
	}
	go func() {
		logger.Info("gateway listening", "addr", addr)
		serveErr <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = srv.Close()
			_ = httpServer.Close()
			if adminServer != nil {
				_ = adminServer.Close()
			}
			return fmt.Errorf("gateway: listen on %s: %w", addr, err)
		}
	case <-ctx.Done():
		logger.Info("gateway shutting down")
	}

	// Drop the relay links first so their handlers return, then let the HTTP
	// server drain whatever plain requests are still in flight.
	closeErr := srv.Close()

	drainCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	if adminServer != nil {
		if err := adminServer.Shutdown(drainCtx); err != nil {
			adminServer.Close()
			return err
		}
	}
	if err := httpServer.Shutdown(drainCtx); err != nil {
		return fmt.Errorf("gateway: shutdown: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("gateway: close: %w", closeErr)
	}
	logger.Info("gateway stopped")
	return nil
}

func loadOptions() (gateway.Options, error) {
	var opts gateway.Options
	var err error

	opts.RegistrationToken = os.Getenv("HERDR_GATEWAY_REGISTRATION_TOKEN")
	allowed := strings.FieldsFunc(os.Getenv("HERDR_GATEWAY_ALLOWED_RELAY_IDS"), func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	if len(allowed) > 0 {
		opts.AllowedRelayIDs = make(map[string]bool, len(allowed))
		for _, id := range allowed {
			if !gatewaywire.ValidRelayID(id) {
				return opts, fmt.Errorf("gateway: HERDR_GATEWAY_ALLOWED_RELAY_IDS contains invalid relay id %q; expected a full 22-character base64url id", id)
			}
			opts.AllowedRelayIDs[id] = true
		}
	}

	if opts.MaxRelays, err = envInt("HERDR_GATEWAY_MAX_RELAYS"); err != nil {
		return opts, err
	}
	if opts.MaxClients, err = envInt("HERDR_GATEWAY_MAX_CLIENTS"); err != nil {
		return opts, err
	}
	if opts.MaxClientsPerRelay, err = envInt("HERDR_GATEWAY_MAX_CLIENTS_PER_RELAY"); err != nil {
		return opts, err
	}
	if opts.ConnectRatePerMinute, err = envInt("HERDR_GATEWAY_CONNECT_RATE_PER_MINUTE"); err != nil {
		return opts, err
	}
	if opts.QuotaWarnPercent, err = envInt("HERDR_GATEWAY_QUOTA_WARN_PERCENT"); err != nil {
		return opts, err
	}

	// HERDR_GATEWAY_MONTHLY_BYTES=0 means unlimited, which Options spells as a
	// negative limit because zero selects the default there.
	monthly, set, err := envInt64("HERDR_GATEWAY_MONTHLY_BYTES")
	if err != nil {
		return opts, err
	}
	if set {
		opts.MonthlyBytes = monthly
		if monthly == 0 {
			opts.MonthlyBytes = -1
		}
	}

	idleSeconds, err := envInt("HERDR_GATEWAY_IDLE_TIMEOUT")
	if err != nil {
		return opts, err
	}
	if idleSeconds < 0 {
		opts.IdleTimeout = -1
	} else {
		opts.IdleTimeout = time.Duration(idleSeconds) * time.Second
	}

	opts.StatePath = envString("HERDR_GATEWAY_STATE", "")
	// Address discovery listens on its own UDP port. gateway.Options treats an
	// empty address as disabled, so the ":3478" default lives here — and unlike
	// every other string variable an explicitly empty value is meaningful, since
	// setting HERDR_GATEWAY_STUN_ADDR= is how an operator who cannot open UDP
	// switches discovery off. envString folds empty into its fallback, so this
	// reads the variable directly.
	opts.STUNAddr = ":3478"
	if raw, ok := os.LookupEnv("HERDR_GATEWAY_STUN_ADDR"); ok {
		opts.STUNAddr = strings.TrimSpace(raw)
	}
	if opts.TrustProxyHeaders, err = envBool("HERDR_GATEWAY_TRUSTED_PROXY"); err != nil {
		return opts, err
	}
	opts.Version = version
	opts.Revision = revision
	return opts, nil
}

func newLogger(format string) (*slog.Logger, error) {
	options := &slog.HandlerOptions{Level: slog.LevelInfo}
	switch format {
	case "text":
		return slog.New(slog.NewTextHandler(os.Stderr, options)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stderr, options)), nil
	default:
		return nil, fmt.Errorf("gateway: HERDR_GATEWAY_LOG_FORMAT must be text or json, got %q", format)
	}
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// envInt returns zero when the variable is unset, letting gateway.Options apply
// its own default.
func envInt(key string) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("gateway: %s must be an integer, got %q", key, raw)
	}
	return value, nil
}

func envInt64(key string) (int64, bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("gateway: %s must be an integer, got %q", key, raw)
	}
	return value, true, nil
}

func envBool(key string) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("gateway: %s must be a boolean, got %q", key, raw)
	}
	return value, nil
}
