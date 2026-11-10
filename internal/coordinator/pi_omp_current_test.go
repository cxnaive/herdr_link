package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/herdr"
	"github.com/0cv/herdr-mobile-relay/internal/question"
)

func TestCurrentPiOMPQuestionDispatchRevalidatesDialog(t *testing.T) {
	for _, name := range []string{"pi-confirm", "omp-approval", "omp-ask-multi", "omp-html-form"} {
		for _, changed := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/current", true: "/changed"}[changed], func(t *testing.T) {
				frame, e := os.ReadFile(filepath.Join("..", "question", "testdata", "pi-omp-current", name+".ansi"))
				if e != nil {
					t.Fatal(e)
				}
				provider := strings.Split(name, "-")[0]
				interaction := question.Parse(string(frame), provider)
				if interaction == nil {
					t.Fatal("missing interaction")
				}
				live := string(frame)
				if changed {
					live = strings.ReplaceAll(live, "synthetic", "replaced")
					live = strings.ReplaceAll(live, "North", "Replaced")
					live = strings.ReplaceAll(live, "Logging", "Changed")
				}
				dir := t.TempDir()
				panePath := filepath.Join(dir, "pane.ansi")
				record := filepath.Join(dir, "calls.log")
				if err := os.WriteFile(panePath, []byte(live), 0600); err != nil {
					t.Fatal(err)
				}
				bin := writeScript(t, dir, "herdr", "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \""+record+"\"\nif [ \"$1 $2\" = \"pane read\" ]; then cat \""+panePath+"\"; else printf '{\"ok\":true}\\n'; fi\n")
				state := NewState(testLogger())
				state.CommitInventory([]*AgentState{{PaneID: "pane-1", Agent: provider, Status: "blocked", Interaction: interaction, AttentionKind: question.AttentionQuestion}}, state.RevisionCounter())
				d := NewDispatcher(herdr.NewClient(bin, filepath.Join(t.TempDir(), "socket")), state, nil, testLogger())
				defer d.Close(context.Background())
				indices := []int{0}
				if name == "omp-ask-multi" {
					indices = []int{0, 2}
				}
				started := time.Now()
				result := d.Handle(context.Background(), map[string]any{"action": "answer_question", "request_id": "test", "pane_id": "pane-1", "interaction_id": interaction.ID, "selected_indices": indices, "other_selected": false})
				data, e := os.ReadFile(record)
				if e != nil {
					t.Fatal(e)
				}
				wrote := strings.Contains(string(data), "send-keys") || strings.Contains(string(data), "send-text")
				if changed {
					if result.OK || wrote {
						t.Fatalf("changed dialog received input: %+v\n%s", result, data)
					}
				} else {
					if !result.OK || !wrote {
						t.Fatalf("current dialog was not dispatched: %+v\n%s", result, data)
					}
					if name == "omp-ask-multi" && strings.Count(string(data), "Space") != 2 {
						t.Fatalf("wrong checkbox input: %s", data)
					}
					if name == "omp-approval" && time.Since(started) < 550*time.Millisecond {
						t.Fatal("approval skipped its anti-accidental-input delay")
					}
				}
			})
		}
	}
}

func TestCurrentPiOMPAnswersRejectControlInjectionAndInvalidActions(t *testing.T) {
	frame, e := os.ReadFile(filepath.Join("..", "question", "testdata", "pi-omp-current", "pi-input.ansi"))
	if e != nil {
		t.Fatal(e)
	}
	i := question.Parse(string(frame), "pi")
	for _, payload := range []questionPayload{
		{OtherSelected: true, OtherText: "\x1b[H"}, {OtherSelected: true, OtherText: "\x00"},
		{Selected: []int{88}}, {Selected: []int{0}, OtherSelected: true, OtherText: "a"}, {Navigation: "next"}, {},
	} {
		if validateQuestionPayload(payload, i) == nil {
			t.Fatalf("unsafe answer accepted: %+v", payload)
		}
	}
	if err := validateQuestionPayload(questionPayload{OtherSelected: true, OtherText: ""}, i); err != nil {
		t.Fatal("explicit empty text refused", err)
	}
}

func TestCurrentPiOMPFieldWatcherWaitsAndReturnsNextControl(t *testing.T) {
	dir := t.TempDir()
	frame, e := os.ReadFile(filepath.Join("..", "question", "testdata", "pi-omp-current", "pi-select.ansi"))
	if e != nil {
		t.Fatal(e)
	}
	initial := question.Parse(string(frame), "pi")
	next := strings.ReplaceAll(string(frame), "Choose a region", "Next field")
	firstPath, nextPath, countPath := filepath.Join(dir, "first"), filepath.Join(dir, "next"), filepath.Join(dir, "count")
	for path, data := range map[string][]byte{firstPath: frame, nextPath: []byte(next), countPath: []byte("0")} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	bin := writeScript(t, dir, "herdr", "#!/bin/sh\n"+
		"if [ \"$1 $2\" = \"pane read\" ]; then\n"+
		" count=$(cat \""+countPath+"\"); count=$((count+1)); echo $count > \""+countPath+"\"\n"+
		" if [ $count -eq 1 ]; then cat \""+firstPath+"\"; elif [ $count -eq 2 ]; then echo redrawing; else cat \""+nextPath+"\"; fi\n"+
		"else printf '{\"ok\":true}\\n'; fi\n")
	state := NewState(testLogger())
	state.CommitInventory([]*AgentState{{PaneID: "pane-1", Agent: "pi", Status: "blocked", Interaction: initial, AttentionKind: question.AttentionQuestion}}, state.RevisionCounter())
	d := NewDispatcher(herdr.NewClient(bin, filepath.Join(dir, "socket")), state, nil, testLogger())
	defer d.Close(context.Background())
	updates := make(chan map[string]any, 8)
	d.SetBroadcast(func(m any) {
		if v, ok := m.(map[string]any); ok {
			updates <- v
		}
	})
	result := d.Handle(context.Background(), map[string]any{"action": "answer_question", "pane_id": "pane-1", "request_id": "step", "interaction_id": initial.ID, "selected_indices": []int{1}})
	if !result.OK {
		t.Fatal(result)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case update := <-updates:
			if update["request_id"] != "step" {
				continue
			}
			if update["phase"] != "advanced" {
				t.Fatalf("field transition falsely finalized/failed: %+v", update)
			}
			data := update["data"].(map[string]any)
			i := data["interaction"].(*question.Interaction)
			if !i.Stepwise || i.ID == initial.ID || !strings.Contains(i.Question, "Next field") {
				t.Fatal(i)
			}
			return
		case <-deadline:
			t.Fatal("missing field transition")
		}
	}
}
