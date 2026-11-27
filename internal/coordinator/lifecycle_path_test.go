package coordinator

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/0cv/herdr-mobile-relay/internal/herdr"
	"github.com/0cv/herdr-mobile-relay/internal/pathidentity"
)

func TestResolveCwdReturnsCanonicalSymlinkTarget(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "projects", "app")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "app-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveProjectCwd(link)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != target {
		t.Fatalf("resolved cwd = %q, want %q", resolved, target)
	}
}

func TestProjectCwdOnRelayFilesystem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := t.TempDir()
	file := filepath.Join(project, "not-a-directory")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{home, project} {
		got, err := resolveProjectCwd(path)
		if err != nil || !pathidentity.SameDirectory(got, path) {
			t.Errorf("resolve %q: %q, %v", path, got, err)
		}
	}
	for _, path := range []string{"", "relative", `D:platform`, `\\server\share`, "//server/share", filepath.Join(project, "missing"), file, project + "\n"} {
		if _, err := resolveProjectCwd(path); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
}

func TestProjectCwdMatchesOnlySelectedWorkspace(t *testing.T) {
	base := t.TempDir()
	project, other := filepath.Join(base, "platform"), filepath.Join(base, "platform-old")
	for _, p := range []string{project, other} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	workspaces := []herdr.Workspace{{ID: "one", Cwd: project}, {ID: "two", Cwd: project, Worktree: &herdr.WorkspaceWorktree{CheckoutPath: other}}}
	l := &Lifecycle{}
	for _, tc := range []struct {
		id, cwd string
		ok      bool
	}{{"one", project, true}, {"two", other, true}, {"one", other, false}, {"missing", project, false}, {"two", project, false}} {
		cwd, err := resolveProjectCwd(tc.cwd)
		if err != nil {
			t.Fatal(err)
		}
		if err = l.checkLaunchWorkspace(t.Context(), workspaces, tc.id, cwd); (err == nil) != tc.ok {
			t.Errorf("%s %q: %v", tc.id, cwd, err)
		}
	}
	link := filepath.Join(project, "redirect")
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd.exe", "/c", "mklink", "/J", link, other).CombinedOutput(); err != nil {
			t.Fatalf("create junction: %v %s", err, out)
		}
	} else if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	cwd, err := resolveProjectCwd(link)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.checkLaunchWorkspace(t.Context(), workspaces, "one", cwd); err == nil {
		t.Fatal("followed a link to the wrong workspace")
	}
}
