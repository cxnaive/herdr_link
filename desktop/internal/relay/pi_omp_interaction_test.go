package relay

import "testing"

func TestCurrentOMPEmptyMultiAndExplicitFormAction(t *testing.T) {
	q := Interaction{ID: "current", Kind: "multi_select", Stepwise: true, Options: []QuestionOption{{Index: 0, Label: "A"}, {Index: 1, Label: "B"}}}
	if _, err := q.Answer([]int{}, false, ""); err != nil {
		t.Fatal(err)
	}
	q.Stepwise = false
	if _, err := q.Answer([]int{}, false, ""); err == nil {
		t.Fatal("legacy empty answer admitted")
	}
	q.Stepwise = true
	q.Kind = "single_select"
	if _, err := q.Answer([]int{}, false, ""); err == nil {
		t.Fatal("form action not explicitly selected")
	}
	if _, err := q.Answer([]int{1}, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Answer([]int{4}, false, ""); err == nil {
		t.Fatal("stale form action admitted")
	}
}
