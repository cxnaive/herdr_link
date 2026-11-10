package relay

import (
	"errors"
	"slices"
	"strings"
)

type QuestionOption struct {
	Index       int    `json:"index"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Selected    bool   `json:"selected"`
	Summary     []struct {
		Question string `json:"q"`
		Answer   string `json:"a"`
	} `json:"summary"`
}
type QuestionOther struct {
	Selected    bool   `json:"selected"`
	Text        string `json:"text"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder"`
	AllowEmpty  bool   `json:"allow_empty"`
	Hidden      bool   `json:"hidden"`
}
type Interaction struct {
	Stepwise      bool             `json:"stepwise"`
	ID            string           `json:"id"`
	Kind          string           `json:"kind"`
	Question      string           `json:"question"`
	Options       []QuestionOption `json:"options"`
	Other         *QuestionOther   `json:"other"`
	SubmitLabel   string           `json:"submit_label"`
	CanChat       bool             `json:"can_chat"`
	CanGoBack     bool             `json:"can_go_back"`
	QuestionIndex int              `json:"question_index"`
	QuestionTotal int              `json:"question_total"`
}

func ApprovalPayload(live, captured Agent, index int) (Message, error) {
	if live.Target != captured.Target || live.Status != "blocked" || live.ApprovalFingerprint == "" || live.ApprovalFingerprint != captured.ApprovalFingerprint || live.EventID != captured.EventID || !slices.Equal(live.Options, captured.Options) || index < 0 || index >= len(captured.Options) {
		return nil, errors.New("审批已变化，请处理最新请求")
	}
	return Message{"index": index, "total": len(captured.Options), "choice": captured.Options[index], "event_id": captured.EventID, "approval_fingerprint": captured.ApprovalFingerprint}, nil
}
func (q Interaction) Answer(indices []int, other bool, text string) (Message, error) {
	if q.ID == "" || len(indices) == 0 && !other && !(q.Stepwise && q.Kind == "multi_select") {
		return nil, errors.New("请选择答案")
	}
	seen := map[int]bool{}
	for _, index := range indices {
		if seen[index] || !slices.ContainsFunc(q.Options, func(o QuestionOption) bool { return o.Index == index }) {
			return nil, errors.New("答案选项已变化")
		}
		seen[index] = true
	}
	if q.Kind == "single_select" && (len(indices) > 1 || other && len(indices) > 0) {
		return nil, errors.New("此问题只能选择一个答案")
	}
	if other && (q.Other == nil || q.Other.Hidden || !q.Other.AllowEmpty && strings.TrimSpace(text) == "") {
		return nil, errors.New("请填写自定义答案")
	}
	if !other {
		text = ""
	}
	slices.Sort(indices)
	return Message{"interaction_id": q.ID, "selected_indices": indices, "other_selected": other, "other_text": text}, nil
}
