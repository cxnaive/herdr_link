package question

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func currentFrame(t *testing.T, name string) string {
	t.Helper()
	b, e := os.ReadFile(filepath.Join("testdata", "pi-omp-current", name+".ansi"))
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

func TestCurrentPiOMPStructuredDialogs(t *testing.T) {
	for _, name := range []string{"pi-confirm", "pi-select", "pi-input", "omp-approval", "omp-session-approval", "omp-grant-approval", "omp-ask-single", "omp-ask-multi", "omp-html-form"} {
		t.Run(name, func(t *testing.T) {
			c := Classify(currentFrame(t, name), strings.Split(name, "-")[0])
			if c.Kind != AttentionQuestion || c.Interaction == nil || !c.Interaction.Stepwise {
				t.Fatalf("missing current interaction: %+v", c)
			}
			if c.Interaction.ID == "" {
				t.Fatal("unbound action")
			}
		})
	}
}
func TestCurrentPiOMPStaleAndTruncatedDialogsFailClosed(t *testing.T) {
	for _, name := range []string{"pi-confirm", "pi-select", "pi-input", "omp-approval", "omp-ask-single", "omp-ask-multi", "omp-html-form"} {
		frame := currentFrame(t, name)
		provider := strings.Split(name, "-")[0]
		for _, bad := range []string{frame + "\nnew unrelated output", strings.Join(strings.Split(frame, "\n")[:3], "\n")} {
			c := Classify(bad, provider)
			if c.Interaction != nil || c.Kind == AttentionApproval {
				t.Fatalf("%s stale/truncated actionable", name)
			}
		}
	}
}
func TestCurrentPiOMPRemappedAndDraftOwnedControlsFailClosed(t *testing.T) {
	for _, name := range []string{"pi-select", "omp-approval", "omp-ask-single", "omp-html-form"} {
		frame := strings.Join(currentLines(currentFrame(t, name)), "\n")
		frame = strings.ReplaceAll(frame, "enter select", "space select")
		frame = strings.ReplaceAll(frame, "⏎", "F8")
		c := Classify(frame, strings.Split(name, "-")[0])
		if c.Interaction != nil || c.Kind == AttentionApproval {
			t.Fatalf("%s remapped actionable", name)
		}
	}
	frame := currentFrame(t, "omp-approval")
	frame = strings.ReplaceAll(frame, "↑/↓ choose · ⏎ select", "⏎ deny and send")
	c := Classify(frame, "omp")
	if c.Interaction != nil || c.Kind == AttentionApproval {
		t.Fatal("draft-owned approval exposed")
	}
}
func TestCurrentPiSelectorDoesNotChoosePartialLists(t *testing.T) {
	frame := currentFrame(t, "pi-select")
	frame = strings.Replace(frame, "South", "(1/15)", 1)
	if c := Classify(frame, "pi"); c.Interaction != nil {
		t.Fatal("windowed selector exposed incorrect indices")
	}
}
func TestCurrentPiInputDoesNotExposeSecrets(t *testing.T) {
	frame := strings.ReplaceAll(currentFrame(t, "pi-input"), "Describe your choice", "Enter password")
	if c := Classify(frame, "pi"); c.Interaction != nil {
		t.Fatal("secret input became persisted answer")
	}
}
func TestCurrentOMPCheckboxUsesSpaceBeforeEnter(t *testing.T) {
	i := Classify(currentFrame(t, "omp-ask-multi"), "omp").Interaction
	got := PlanInput(i, InputIntent{Selected: []int{0, 2}})
	want := []InputStep{{Keys: []string{"Space"}}, {Keys: []string{"Down", "Down", "Space"}}, {Keys: []string{"Enter"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("checkbox contract: %+v", got)
	}
}
func TestCurrentOMPRememberIsAnExplicitNonApprovingAction(t *testing.T) {
	i := Classify(currentFrame(t, "omp-grant-approval"), "omp").Interaction
	if !strings.Contains(i.Options[0].Label, "不批准") {
		t.Fatal("remember toggle mislabeled")
	}
	if got := PlanInput(i, InputIntent{Selected: []int{0}}); !reflect.DeepEqual(got, []InputStep{{Keys: []string{"Up", "Up", "Enter"}}}) {
		t.Fatal(got)
	}
}
func TestCurrentOMPFieldStateChangesRevokeActions(t *testing.T) {
	frame := currentFrame(t, "omp-html-form")
	a := Classify(frame, "omp").Interaction
	b := Classify(strings.ReplaceAll(frame, "synthetic", "changed"), "omp").Interaction
	if a == nil || b == nil || a.ID == b.ID {
		t.Fatal("edited field reused an old interaction")
	}
}
func TestCurrentOMPPlanApprovalAndStaleFrame(t *testing.T) {
	frame := currentFrame(t, "omp-plan-0")
	c := Classify(frame, "omp")
	want := []string{"Approve and execute", "Approve and compact context", "Approve and keep context", "Refine plan"}
	if c.Kind != AttentionApproval || !reflect.DeepEqual(c.Options, want) || c.ApprovalFocus != 0 || ApprovalFingerprint(c) == "" {
		t.Fatalf("plan: %+v", c)
	}
	stale := Classify(frame+"\nNew unrelated output after the dialog\n", "omp")
	if stale.Kind == AttentionApproval || stale.Interaction != nil {
		t.Fatal("stale plan actionable")
	}
}

func TestCurrentPiOMPStockFooterDoesNotHideTheLiveDialog(t *testing.T) {
	for _, name := range []string{"pi-select", "omp-approval"} {
		frame := currentFrame(t, name)
		provider := strings.Split(name, "-")[0]
		footer := "\n/synthetic/work\n0.0%/0 (auto)                 unknown"
		if provider == "omp" {
			footer = "\n π > no-model > /synthetic/work > ◫ 0/? ─────────────────\n╰─                         ⇧⇥ to change thinking effort"
		}
		before := Classify(frame, provider).Interaction
		after := Classify(frame+footer, provider).Interaction
		if before == nil || after == nil || before.ID != after.ID {
			t.Fatalf("%s stock chrome changes the dialog: before=%+v after=%+v", name, before, after)
		}
		if c := Classify(frame+footer+"\nnew output", provider); c.Interaction != nil {
			t.Fatal("new output resurrected old dialog")
		}
	}
}
