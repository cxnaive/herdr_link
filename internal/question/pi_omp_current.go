package question

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	ompEmptyComposerTop    = regexp.MustCompile(`^π > .*─{8,}$`)
	ompEmptyComposerBottom = regexp.MustCompile(`^╰─\s*(?:⇧⇥ to change thinking effort)?$`)
	controlString          = regexp.MustCompile("\\x1b(?:_[^\\x07]*(?:\\x07|$)|\\][^\\x07]*(?:\\x07|$))")
	privateInputTitle      = regexp.MustCompile(`(?i)password|passphrase|secret|api[ _-]?key|access[ _-]?token|密码|口令|密钥`)
	piFooterStats          = regexp.MustCompile(`^(?:[↑↓RWCH$0-9.,kKmM% /()+-]+ )?(?:\?|[0-9.]+%)/[0-9.kKmM]+(?: \(auto\))?(?:\s+.*)?$`)
	piFooterPath           = regexp.MustCompile(`^(?:/|~/|[A-Za-z]:[\\/])`)
	piScrollCount          = regexp.MustCompile(`^\(\d+/\d+\)$`)
)

func currentLines(text string) []string {
	text = controlString.ReplaceAllString(ansiPattern.ReplaceAllString(text, ""), "")
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

// A complete last panel and the exact default-key footer are required. Unknown
// bindings, clipped controls, draft-owned input and trailing output fail closed.
func currentPanel(text string, pi bool) (body []string, footer string, ok bool) {
	lines := currentLines(text)
	end := len(lines) - 1
	for end >= 0 && strings.TrimSpace(lines[end]) == "" {
		end--
	}
	if end < 0 {
		return
	}
	// Pi's stock footer is outside the extension panel: cwd, then token/context stats.
	if pi && end >= 2 && piFooterStats.MatchString(strings.TrimSpace(lines[end])) && piFooterPath.MatchString(strings.TrimSpace(lines[end-1])) {
		end -= 2
		for end >= 0 && strings.TrimSpace(lines[end]) == "" {
			end--
		}
	}
	if end < 0 {
		return
	}

	if !pi && end >= 1 && ompEmptyComposerBottom.MatchString(strings.TrimSpace(lines[end])) && ompEmptyComposerTop.MatchString(strings.TrimSpace(lines[end-1])) {
		end -= 2
		for end >= 0 && strings.TrimSpace(lines[end]) == "" {
			end--
		}
	}
	if end < 0 {
		return
	}
	isRule := func(s string) bool { return terminalRulePattern.MatchString(strings.TrimSpace(s)) }
	if pi {
		if !isRule(lines[end]) {
			return
		}
	} else if !strings.HasPrefix(strings.TrimSpace(lines[end]), "╰") {
		return
	}
	start := end - 1
	for start >= 0 {
		if pi && isRule(lines[start]) || !pi && strings.HasPrefix(strings.TrimSpace(lines[start]), "╭") {
			break
		}
		start--
	}
	if start < 0 || end-start < 3 || end-start > 160 {
		return
	}
	for _, raw := range lines[start+1 : end] {
		s := strings.TrimSpace(raw)
		if !pi {
			s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "│"), "│"))
		}
		body = append(body, s)
	}
	for len(body) > 0 && (body[len(body)-1] == "" || ompBorderLine(body[len(body)-1])) {
		body = body[:len(body)-1]
	}
	if len(body) < 2 {
		return nil, "", false
	}
	footer = body[len(body)-1]
	body = body[:len(body)-1]
	if !pi {
		body = append([]string{strings.TrimSpace(lines[start])}, body...)
	}
	return body, footer, true
}

func finishCurrent(i *Interaction, text string) *Interaction {
	var display []string
	for _, line := range strings.Split(i.Question, "\n") {
		if ompBorderLine(line) {
			continue
		}
		if strings.HasPrefix(line, "╭") {
			line = strings.Trim(line, "╭╮─━═ ")
		}
		display = append(display, line)
	}
	i.Question = strings.Join(display, "\n")
	i.Stepwise = true
	// Unlike old layouts, a focus/value/remember-scope change is a new request.
	// Never reuse a captured form action against another field or a new draft.
	body, footer, ok := currentPanel(text, i.Agent == "pi-current")
	if !ok {
		return nil
	}
	sum := sha256.Sum256([]byte(strings.Join(body, "\n") + "\n" + footer))
	i.ID = hex.EncodeToString(sum[:])[:24]
	return i
}

func action(i *Interaction, label string, keys ...string) {
	i.Options = append(i.Options, Option{Index: len(i.Options), Label: label})
	i.ActionKeys = append(i.ActionKeys, keys)
}

func currentInteraction(text, agent string) (*Interaction, bool) {
	normalized := strings.ToLower(strings.TrimSpace(agent))
	if normalized == "pi" || strings.HasPrefix(normalized, "pi-") {
		if i, recognized := parsePiCurrent(text); recognized {
			return i, true
		}
	}
	if !ompAskAgent(normalized) {
		return nil, false
	}
	body, footer, ok := currentPanel(text, false)
	if !ok {
		return nil, false
	}
	if strings.Contains(body[0], "Allow tool:") && strings.Contains(footer, "approve") {
		return parseOMPCurrentApproval(text, body, footer), true
	}
	if footer == "⏎ or Ctrl+Q submit  ⎋ cancel  Ctrl+G external editor" {
		if privateInputTitle.MatchString(strings.Join(body, "\n")) {
			return nil, true
		}
		i := &Interaction{Kind: "single_select", Agent: "omp-editor-current", Other: Other{Label: "在当前光标处插入答案（不自动提交）"}, InputMode: "insert", SubmitLabel: "执行所选操作", Question: strings.Join(body, "\n")}
		action(i, "提交当前答案", "Enter")
		action(i, "取消编辑", "Esc")
		return finishCurrent(i, text), true
	}
	if !ompAskHeaderPattern.MatchString(body[0]) {
		return nil, false
	}
	if strings.Contains(footer, "next field") || strings.Contains(footer, "highlight") {
		return parseOMPFormStep(text, body, footer), true
	}
	if strings.Contains(footer, "⏎") {
		return parseOMPCurrentAsk(text, body, footer), true
	}
	return nil, false
}

func parsePiCurrent(text string) (*Interaction, bool) {
	body, footer, ok := currentPanel(text, true)
	if !ok {
		return nil, false
	}
	selectFooter := footer == "↑↓ navigate  enter select  escape/ctrl+c cancel"
	inputFooter := footer == "enter submit  escape/ctrl+c cancel"
	if !selectFooter && !inputFooter {
		return nil, false
	}
	i := &Interaction{Kind: "single_select", Agent: "pi-current", Other: Other{Hidden: true}, SubmitLabel: "确认所选操作"}
	if inputFooter {
		prompt := -1
		for j, s := range body {
			if strings.HasPrefix(s, ">") {
				if prompt >= 0 {
					return nil, true
				}
				prompt = j
			}
		}
		if prompt < 1 {
			return nil, true
		}
		i.Question = strings.TrimSpace(strings.Join(body[:prompt], "\n"))
		if privateInputTitle.MatchString(i.Question) {
			return nil, true
		}
		i.Other = Other{Label: "输入答案（替换当前输入）", AllowEmpty: true}
		i.InputMode = "replace-line"
		action(i, "取消", "Esc")
		return finishCurrent(i, text), true
	}
	// The list is a separate blank-delimited block with exactly one live arrow.
	focus := -1
	start, stop := -1, -1
	for j, s := range body {
		if strings.HasPrefix(s, "→ ") {
			if focus >= 0 {
				return nil, true
			}
			focus = j
		}
	}
	if focus < 0 {
		return nil, true
	}
	start, stop = focus, focus
	for start > 0 && body[start-1] != "" {
		start--
	}
	for stop+1 < len(body) && body[stop+1] != "" {
		stop++
	}
	if start < 1 || stop-start > 9 {
		return nil, true
	}
	i.Question = strings.TrimSpace(strings.Join(body[:start], "\n"))
	if i.Question == "" {
		return nil, true
	}
	rawLines := currentLines(text)
	optionRows := 0
	for _, raw := range rawLines {
		trimmed := strings.TrimSpace(raw)
		for j := start; j <= stop; j++ {
			if trimmed == body[j] && (strings.HasPrefix(raw, " → ") || strings.HasPrefix(raw, "   ")) {
				optionRows++
				break
			}
		}
	}
	if optionRows != stop-start+1 {
		return nil, true
	}
	for j := start; j <= stop; j++ {
		label := strings.TrimPrefix(body[j], "→ ")
		if label == "" || piScrollCount.MatchString(label) {
			return nil, true
		}
		keys := moveKeys(j - focus)
		action(i, label, append(keys, "Enter")...)
	}
	for _, s := range body[stop+1:] {
		if s != "" {
			return nil, true
		}
	}
	action(i, "取消", "Esc")
	return finishCurrent(i, text), true
}

func moveKeys(distance int) []string {
	key := "Down"
	if distance < 0 {
		key = "Up"
		distance = -distance
	}
	keys := make([]string, distance)
	for j := range keys {
		keys[j] = key
	}
	return keys
}

func parseOMPCurrentApproval(text string, body []string, footer string) *Interaction {
	if strings.Contains(strings.Join(body, "\n"), "…") {
		return nil
	}
	if !strings.HasPrefix(footer, "↑/↓ choose · ⏎ select · ") || !strings.Contains(footer, "⎋ deny") || strings.Contains(footer, "deny and send") {
		return nil
	}
	divider := -1
	for j := len(body) - 1; j > 0; j-- {
		if ompBorderLine(body[j]) {
			divider = j
			break
		}
	}
	if divider < 2 {
		return nil
	}
	stop := divider - 1
	for stop > 0 && body[stop] == "" {
		stop--
	}
	if body[stop] != "Deny" && body[stop] != "❯ Deny" {
		return nil
	}
	start := stop
	valid := func(s string) bool {
		s = strings.TrimPrefix(s, "❯ ")
		return s == "Approve" || s == "Approve Once" || s == "Deny" || strings.HasPrefix(s, "Approve for ") || strings.HasPrefix(s, "Allow ") || strings.HasPrefix(s, "Always allow ") || strings.HasSuffix(s, " Remember across sessions")
	}
	for start > 1 && valid(body[start-1]) {
		start--
	}
	if start >= stop {
		return nil
	}
	// Wrapped or ambiguous option rows are not mapped to indices.
	focus := -1
	for j := start; j <= stop; j++ {
		if !valid(body[j]) {
			return nil
		}
		if strings.HasPrefix(body[j], "❯ ") {
			if focus >= 0 {
				return nil
			}
			focus = j
		}
	}
	if focus < 0 {
		return nil
	}
	i := &Interaction{Kind: "single_select", Agent: "omp-approval-current", Other: Other{Hidden: true}, SubmitLabel: "确认所选审批操作"}
	i.Question = strings.Join(body[:start], "\n")
	for j := start; j <= stop; j++ {
		label := strings.TrimPrefix(body[j], "❯ ")
		if strings.Contains(label, "Remember across sessions") {
			label += "（仅切换设置，不批准）"
		}
		action(i, label, append(moveKeys(j-focus), "Enter")...)
	}
	return finishCurrent(i, text)
}

func parseOMPCurrentAsk(text string, body []string, footer string) *Interaction {
	if (!strings.Contains(footer, "↑/↓ move") && !strings.Contains(footer, "↑/↓ scroll")) || !strings.Contains(footer, "⎋ cancel") {
		return nil
	}
	if !strings.HasPrefix(footer, "⏎ select") && !strings.HasPrefix(footer, "␣ toggle · ⏎ ") && !strings.HasPrefix(footer, "⏎ submit") {
		return nil
	}
	// HTML inputs also use an Ask border but have a distinct next-field contract.
	i := &Interaction{Kind: "single_select", Agent: "omp-ask-current", Other: Other{Hidden: true}, SubmitLabel: "选择并继续"}
	first, focus := -1, -1
	var rows []ompOptionRow
	for j, s := range body {
		m := ompOptionPattern.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		if first < 0 {
			first = j
		}
		if m[1] != "" {
			if focus >= 0 {
				return nil
			}
			focus = len(rows)
		}
		rows = append(rows, ompOptionRow{line: j, focus: m[1] != "", marker: m[2], label: m[3], selected: ompMarkerSelected(m[2])})
	}
	if len(rows) == 0 {
		if !strings.HasPrefix(footer, "⏎ submit") || !strings.Contains(strings.Join(body, "\n"), "Review answers") {
			return nil
		}
		i.Question = strings.Join(body, "\n")
		action(i, "提交以上答案", "Enter")
		action(i, "返回修改", "Left")
		action(i, "取消", "Esc")
		return finishCurrent(i, text)
	}
	if strings.Contains(strings.Join(body, "\n"), "…") {
		return nil
	}
	if focus < 0 || len(rows) > 30 {
		return nil
	}
	i.Question = strings.Join(body[:first], "\n")
	i.Focus = Focus{Kind: "option", Index: focus}
	for index, row := range rows {
		end := len(body)
		if index+1 < len(rows) {
			end = rows[index+1].line
		}
		var details []string
		for _, line := range body[row.line+1 : end] {
			if line != "" && !ompBorderLine(line) {
				details = append(details, line)
			}
		}
		i.Options = append(i.Options, Option{Index: index, Label: row.label, Description: strings.Join(details, "\n"), Selected: row.selected})
	}
	i.AllOptionCount = len(rows)
	if strings.HasPrefix(footer, "␣ toggle") {
		i.Kind = "multi_select"
		i.SubmitLabel = "保存选择并继续"
	}
	if i.Kind == "single_select" {
		for index := range i.Options {
			i.ActionKeys = append(i.ActionKeys, append(moveKeys(index-focus), "Enter"))
		}
		action(i, "为当前选项填写备注", "n")
		if strings.Contains(footer, "question") {
			action(i, "上一题", "Left")
			action(i, "下一题", "Right")
		}
		action(i, "取消", "Esc")
	}
	return finishCurrent(i, text)
}

// HTML stays native: expose one explicit operation against the visible focused
// field, not invented HTML names/values. Tab never submits; Submit is separate.
func parseOMPFormStep(text string, body []string, footer string) *Interaction {
	if !strings.Contains(footer, "⎋ cancel") && !strings.Contains(footer, "⎋ close") {
		return nil
	}
	if !strings.Contains(footer, "↑/↓ move") && !strings.Contains(footer, "↑/↓ highlight") {
		return nil
	}
	if !strings.Contains(footer, "⇥ next field") && !strings.Contains(footer, "highlight") {
		return nil
	}
	i := &Interaction{Kind: "single_select", Agent: "omp-form-current", Other: Other{Hidden: true}, SubmitLabel: "执行所选操作"}
	i.Question = "原生表单：每次只操作当前字段，最后单独提交。\n" + strings.Join(body, "\n")
	if len(i.Question) > 16000 {
		return nil
	}
	switch {
	case strings.HasPrefix(footer, "⏎ submit"):
		action(i, "提交整个表单", "Enter")
	case strings.HasPrefix(footer, "⏎ choose"):
		action(i, "选择当前项并到下一字段", "Enter")
	case strings.HasPrefix(footer, "␣ toggle"):
		action(i, "切换当前复选项", "Space")
	case strings.HasPrefix(footer, "space open") || strings.HasPrefix(footer, "␣ open"):
		action(i, "展开下拉选项", "Space")
		action(i, "前一个选项", "Left")
		action(i, "后一个选项", "Right")
	case strings.Contains(footer, "highlight"):
		action(i, "确认当前下拉选项", "Enter")
		action(i, "关闭下拉列表（不取消表单）", "Esc")
	case strings.HasPrefix(footer, "⏎ next") || strings.HasPrefix(footer, "type your own"):
		if privateInputTitle.MatchString(i.Question) || strings.Contains(i.Question, "••") {
			return nil
		}
		i.InputMode = "insert"
		i.Other = Other{Label: "在当前字段光标处插入文本（保留原值，不提交）"}
	default:
		return nil
	}
	action(i, "向上移动", "Up")
	action(i, "向下移动", "Down")
	if strings.Contains(footer, "next field") {
		action(i, "下一字段（不提交）", "Tab")
		action(i, "上一字段", "Shift+Tab")
		action(i, "取消表单", "Esc")
	}
	return finishCurrent(i, text)
}
