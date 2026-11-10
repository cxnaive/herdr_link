package localipc

import "testing"

func TestWindowsPipeNameMatchesHerdrInterprocessNamespace(t *testing.T) {
	for _, path := range []string{`C:\Users\Alice\AppData\Roaming\herdr\herdr.sock`, `C:/mixed path/中文/herdr-client.sock`} {
		got, err := WindowsPipeName(path)
		if err != nil || got != `\\.\pipe\`+path {
			t.Fatalf("pipe namespace mismatch: %s %v", got, err)
		}
	}
	for _, path := range []string{"", "nul\x00path", `\\remote\pipe\name`, `\\.\pipe\already-prefixed`} {
		if _, err := WindowsPipeName(path); err == nil {
			t.Fatal("accepted non-local marker path")
		}
	}
}
