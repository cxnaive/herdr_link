package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayJoinRejectsCommandLineSecretsAndUnsafeCodeFiles(t *testing.T) {
	var out bytes.Buffer
	if status, err := runGatewayJoin([]string{"herdr-gateway://secret"}, strings.NewReader(""), &out); status != 2 || err == nil || strings.Contains(out.String(), "herdr-gateway://secret") {
		t.Fatal("secret argument accepted or echoed")
	}
	path := filepath.Join(t.TempDir(), "code")
	os.WriteFile(path, []byte("PRIVATE_TEST_CODE"), 0644)
	if status, err := runGatewayJoin([]string{"--code-file", path}, strings.NewReader(""), &out); status == 0 || err == nil {
		t.Fatal("public code file read")
	}
	os.Chmod(path, 0600)
	if _, err := runGatewayJoin([]string{"--code-file", path}, strings.NewReader(""), &out); err == nil || strings.Contains(err.Error(), "PRIVATE_TEST_CODE") {
		t.Fatal("invalid code accepted or leaked")
	}
	if _, err := runGatewayJoin(nil, strings.NewReader(strings.Repeat("a", 9000)), &out); err == nil {
		t.Fatal("unbounded input accepted")
	}
}
