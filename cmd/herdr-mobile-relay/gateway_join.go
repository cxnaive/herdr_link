package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/config"
	"github.com/0cv/herdr-mobile-relay/internal/gatewayenroll"
	"github.com/0cv/herdr-mobile-relay/internal/privatepath"
)

func runGatewayJoin(args []string, input io.Reader, output io.Writer) (int, error) {
	flags := flag.NewFlagSet("gateway-join", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("code-file", "", "private file containing the one-time access code; otherwise read one line from stdin")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return 2, errors.New("usage: herdr-mobile-relay gateway-join [--code-file PRIVATE_FILE]; never pass the code as a command-line argument")
	}
	if *file != "" {
		info, err := os.Lstat(*file)
		if err != nil || !info.Mode().IsRegular() || !privatepath.Private(*file, info) {
			return 1, errors.New("code file must be a private regular file (0600)")
		}
		f, err := os.Open(*file)
		if err != nil {
			return 1, errors.New("cannot open code file")
		}
		defer f.Close()
		opened, statErr := f.Stat()
		if statErr != nil || !os.SameFile(info, opened) {
			return 1, errors.New("code file changed while opening")
		}
		input = f
	} else {
		_, _ = fmt.Fprintln(output, "Paste the one-time Gateway access code, then press Enter (do not share it):")
	}
	line, err := bufio.NewReader(io.LimitReader(input, 4097)).ReadString('\n')
	if err != nil && err != io.EOF {
		return 1, gatewayenroll.ErrEnrollment
	}
	raw := strings.TrimSpace(line)
	if len(raw) > 4096 {
		return 1, gatewayenroll.ErrEnrollment
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	record, err := gatewayenroll.Join(ctx, raw, config.RuntimeDirectory(), os.Getenv("HERDR_RELAY_TOKEN"))
	if err != nil {
		return 1, err
	}
	_, err = fmt.Fprintf(output, "Gateway enrollment saved. Relay ID: %s\nStart or restart only the Relay service to connect. Existing phone pairing and Agents are unchanged.\n", record.RelayID)
	if err != nil {
		return 1, err
	}
	return 0, nil
}
