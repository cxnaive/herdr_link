package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/0cv/herdr-mobile-relay/internal/gatewayaccess"
)

func TestAdminInitializationRequiresExplicitCommandAndNeverOverwrites(t *testing.T) {
	var out bytes.Buffer
	if gatewayCommand([]string{"unknown"}, &out) == nil {
		t.Fatal("unknown command accepted")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	if err := gatewayCommand([]string{"admin-init", "--state-dir", dir}, &out); err != nil {
		t.Fatal(err)
	}
	fields := strings.Split(strings.TrimSpace(out.String()), "\n")
	key := fields[len(fields)-1]
	store, err := gatewayaccess.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if !store.Login(key) {
		t.Fatal("printed key cannot authenticate")
	}
	out.Reset()
	if gatewayCommand([]string{"admin-init", "--state-dir", dir}, &out) == nil || out.Len() != 0 {
		t.Fatal("overwrote working management state")
	}
}
