package coordinator

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/0cv/herdr-mobile-relay/internal/question"
)

// Used by tests/pi-omp-upstream.mjs with synthetic frames from the published UI
// components. No Herdr connection, process discovery, or tool execution occurs.
func TestPiOMPUpstreamContract(t *testing.T) {
	input, output := os.Getenv("HERDR_INTERACTION_INPUT"), os.Getenv("HERDR_INTERACTION_OUTPUT")
	if input == "" || output == "" {
		t.Skip("explicit upstream component fixture required")
	}
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var in struct {
		Agent         string
		Frame         string
		Intent        question.InputIntent
		ApprovalIndex *int
	}
	if err := json.Unmarshal(data, &in); err != nil {
		t.Fatal(err)
	}
	c := question.Classify(in.Frame, in.Agent)
	var steps []question.InputStep
	var validationError string
	if c.Interaction != nil {
		err := validateQuestionPayload(questionPayload{InteractionID: c.Interaction.ID, Selected: in.Intent.Selected, OtherSelected: in.Intent.OtherSelected, OtherText: in.Intent.OtherText, Navigation: in.Intent.Navigation, Clarify: in.Intent.Clarify}, c.Interaction)
		if err != nil {
			validationError = err.Error()
		} else {
			steps = question.PlanInput(c.Interaction, in.Intent)
		}
	}
	if in.ApprovalIndex != nil && c.Kind == question.AttentionApproval {
		if *in.ApprovalIndex < 0 || *in.ApprovalIndex >= len(c.Options) {
			t.Fatal("invalid synthetic approval index")
		}
		steps = []question.InputStep{{Keys: approvalKeys(*in.ApprovalIndex, c.ApprovalFocus)}}
	}
	data, err = json.Marshal(map[string]any{"validation_error": validationError, "classification": c, "steps": steps, "fingerprint": question.ApprovalFingerprint(c)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, data, 0600); err != nil {
		t.Fatal(err)
	}
}
