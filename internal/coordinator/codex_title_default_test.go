package coordinator

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/profiles"
)

func TestCodexTitleDefaultPrecedesUserArgsAndIsStableOnRestart(t *testing.T) {
	for name, user := range map[string][]string{
		"empty":                 nil,
		"standalone":            {"--no-daemon"},
		"other config":          {"-c", `model="custom"`},
		"explicit title":        {"-c", `tui.terminal_title=["project-name"]`},
		"explicit long":         {"--config", `tui.terminal_title=[]`},
		"explicit joined":       {`--config=tui.terminal_title=["thread-name"]`},
		"explicit short joined": {`-ctui.terminal_title=[]`},
		"resume":                {"resume", "synthetic-thread", "-c", `tui.terminal_title=[]`},
		"config-like prompt":    {"--", "-c", codexTerminalTitleDefault},
		"quoted argument":       {"two words", "", `$(id); echo test`},
	} {
		t.Run(name, func(t *testing.T) {
			original := slices.Clone(user)
			request := StartRequest{AgentArgs: user, Prompt: "do not change the prompt"}
			got, err := request.launchArgs("codex")
			want := append(AgentLaunchDefaults()["codex"], user...)
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("argv = %q, %v", got, err)
			}
			if !slices.Equal(request.AgentArgs, original) || request.Prompt != "do not change the prompt" {
				t.Fatal("mutated request")
			}
			for range 3 {
				got, err = (StartRequest{AgentArgs: got}).launchArgs("codex")
				if err != nil || !slices.Equal(got, want) {
					t.Fatal("restarts accumulated title defaults")
				}
			}
			if len(original) > 0 {
				got[2] = "changed"
				if !slices.Equal(user, original) {
					t.Fatal("argv shares caller storage")
				}
			}
		})
	}
}

func TestTitleDefaultsOnlyApplyToResolvedCodexKind(t *testing.T) {
	for _, kind := range []string{"", "claude", "hermes", "pi", "opencode", "test-wrapper", "Codex"} {
		request := StartRequest{ProfileID: "codex", AgentArgs: []string{"--custom", "two words"}}
		got, err := request.launchArgs(kind)
		if err != nil || !slices.Equal(got, request.AgentArgs) {
			t.Fatalf("changed %q argv: %q, %v", kind, got, err)
		}
	}
}

func TestCodexTitleDefaultCountsTowardBoundsBeforePaneCreation(t *testing.T) {
	f := newLaunchOwnershipFixture(t, "codex", nil)
	for _, args := range [][]string{
		make([]string, profiles.MaxLaunchArgs-1),
		{strings.Repeat("x", profiles.MaxLaunchArgBytes)},
	} {
		request := f.request
		request.AgentArgs = args
		if _, _, err := f.lifecycle.ValidateStart(request); err == nil {
			t.Fatal("effective argv exceeds the limit")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := f.lifecycle.Start(ctx, f.profile, request)
		cancel()
		if err == nil {
			t.Fatal("oversized launch was accepted")
		}
		if _, err := os.Stat(f.calls); !os.IsNotExist(err) {
			t.Fatal("invalid arguments reached Herdr")
		}
	}
	request := f.request
	request.AgentArgs = make([]string, profiles.MaxLaunchArgs-2)
	if _, _, err := f.lifecycle.ValidateStart(request); err != nil {
		t.Fatal(err)
	}
}

func TestAdvertisedLaunchDefaultsMatchEffectiveArgsAndAreIndependent(t *testing.T) {
	defaults := AgentLaunchDefaults()
	got, err := (StartRequest{}).launchArgs("codex")
	if err != nil || !slices.Equal(defaults["codex"], got) {
		t.Fatalf("defaults %q != argv %q: %v", defaults["codex"], got, err)
	}
	defaults["codex"][0] = "changed"
	if AgentLaunchDefaults()["codex"][0] != "-c" {
		t.Fatal("caller changed shared defaults")
	}
	if len(defaults) != 1 {
		t.Fatal("other providers inherited Codex defaults")
	}
}

func TestCodexTitleArgvAcrossPlatforms(t *testing.T) {
	for _, platform := range []string{"windows", "linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			title := `tui.terminal_title=["thread-id","thread-name"]`
			if platform == "windows" {
				title = `tui.terminal_title=['thread-id','thread-name']`
			}
			user := []string{"-c", `model="user choice"`, "--config=tui.terminal_title=[]", "--", codexTerminalTitleDefault}
			want := append([]string{"-c", title}, user...)
			for _, saved := range [][]string{user, want, append([]string{"-c", codexTerminalTitleDefault}, user...)} {
				before := slices.Clone(saved)
				if got := codexLaunchArgs(saved, platform); !slices.Equal(got, want) || !slices.Equal(saved, before) {
					t.Fatalf("got %q; want %q; input %q", got, want, saved)
				}
			}
		})
	}
}
