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

func TestAdminCustomPasswordReadsBoundedStdinWithoutEcho(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	old, err := gatewayaccess.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	const password = "test-custom-password"
	var out bytes.Buffer
	args := []string{"admin-reset", "--state-dir", dir, "--password-stdin"}
	if err := gatewayCommandInput(args, strings.NewReader(password+"\r\n"), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), password) {
		t.Fatal("echoed password")
	}
	for _, invalid := range []string{"short\n", strings.Repeat("a", 1024), password + "\nsecond-line\n"} {
		out.Reset()
		if err := gatewayCommandInput(args, strings.NewReader(invalid), &out); err == nil || out.Len() != 0 {
			t.Fatal("invalid password accepted or echoed")
		}
	}
	store, err := gatewayaccess.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if !store.Login(password) || store.Login(old) {
		t.Fatal("password update not persisted")
	}
	if err := gatewayCommandInput([]string{"admin-init", "--state-dir", dir, "--password-stdin"}, strings.NewReader(password), &out); err == nil {
		t.Fatal("custom reset flag accepted for initialization")
	}
}
