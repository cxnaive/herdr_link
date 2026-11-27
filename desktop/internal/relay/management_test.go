package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLaunchArgsPreserveLiteralDataAndServerDefaults(t *testing.T) {
	args, err := ParseLaunchArgs(`--model x -c 'tui.terminal_title=["thread-id","thread-name"]' "" '$HOME' '$(echo nope)' "C:\path"`)
	want := []string{"--model", "x", "-c", `tui.terminal_title=["thread-id","thread-name"]`, "", "$HOME", "$(echo nope)", `C:\path`}
	if err != nil || !reflect.DeepEqual(args, want) {
		t.Fatal(args, err)
	}
	for _, bad := range []string{"'open", "end\\", "a\nb", strings.Repeat("a ", 65), strings.Repeat("中", 3000), "'\t'"} {
		if _, err := ParseLaunchArgs(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	w := Workspace{ID: "workspace", ServerSessionID: "primary", Cwd: "/work"}
	p, err := LaunchPayload(w, "codex", "review", "检查代码", "--model demo", true)
	if err != nil || p["workspace_id"] != w.ID || !reflect.DeepEqual(p["agent_args"], []string{"--model", "demo"}) {
		t.Fatal(p, err)
	}
	if _, err = LaunchPayload(w, "codex", "review", "", "--model demo", false); err == nil {
		t.Fatal("silently discarded unsupported args")
	}
	p, err = LaunchPayload(w, "codex", "review", "", "", false)
	if err != nil || p["agent_args"] != nil {
		t.Fatal("defaults must be owned by server", p, err)
	}
	for _, bad := range []Workspace{{ID: "cwd:/work", Cwd: "/work", ServerSessionID: "primary"}, {ID: "w", Cwd: "/work", ServerSessionID: "secondary"}, {ID: "w", Cwd: "relative", ServerSessionID: "primary"}} {
		if _, err = LaunchPayload(bad, "codex", "review", "", "", true); err == nil {
			t.Fatal("bad workspace accepted", bad)
		}
	}
}
func TestLaunchUsesRemotePathSyntax(t *testing.T) {
	for _, path := range []string{`D:\platform`, `d:/项目 A`, "/mnt/projects", "/Volumes/Projects"} {
		w := Workspace{ID: "project", ServerSessionID: "primary", Cwd: path}
		p, err := LaunchPayload(w, "codex", "test", "", "", false)
		if err != nil || p["cwd"] != path || p["workspace_id"] != w.ID {
			t.Fatalf("%q: %v, %v", path, p, err)
		}
	}
	for _, path := range []string{`D:platform`, `\platform`, `\\server\share`, "//server/share", "D:/a\nb"} {
		if RemoteDirectoryPath(path) {
			t.Errorf("accepted %q", path)
		}
	}
}

func TestManagementPermissionsSettingsAndNames(t *testing.T) {
	a := Agent{Target: Target{PaneID: "p", TerminalID: "t"}, Agent: "codex", BindingState: "conflict"}
	if !a.OperationWritable("agent_restart") || a.OperationWritable("set_agent_settings") || a.OperationWritable("respond") {
		t.Fatal("terminal authority leaked to chat")
	}
	for _, name := range []string{"", "a\nb", strings.Repeat("名", 129)} {
		if _, err := RenamePayload(name); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
	s := AgentSettings{Available: true, Revision: "rev", Models: []ModelOption{{Model: "m", Efforts: []EffortOption{{Value: "high"}}}}}
	p, err := s.Payload("m", "high")
	if err != nil || p["settings_revision"] != "rev" {
		t.Fatal(p, err)
	}
	if _, err = s.Payload("m", "invalid"); err == nil {
		t.Fatal("unadvertised effort accepted")
	}
}
func TestOutcomeUnknownTakesPriorityOverNegativeOK(t *testing.T) {
	for _, phase := range []string{"dispatched_unknown", "awaiting_evidence", "unconfirmed"} {
		_, err := commandReply(Message{"type": "command_result", "ok": false, "phase": phase, "error": "timeout"})
		if !UnknownOutcome(err) {
			t.Fatal("uncertain rejection lost", phase)
		}
	}
	for _, reply := range []Message{{"type": "error", "error": "denied"}, {"type": "upload_chunk_result", "error": Message{"code": "bad_hash"}}, {"type": "command_result", "ok": false, "phase": "failed"}} {
		_, err := commandReply(reply)
		if err == nil || UnknownOutcome(err) {
			t.Fatal("definite failure locked", reply, err)
		}
	}
	if !UnknownOutcome(context.DeadlineExceeded) {
		t.Fatal("transport timeout treated as rejection")
	}
	if data, err := commandReply(Message{"type": "upload_finish_result", "result": Message{"done": true}}); err != nil || data["done"] != true {
		t.Fatal(data, err)
	}
}
func TestApprovalAndQuestionRejectStaleOrInvalidAnswers(t *testing.T) {
	a := Agent{Target: Target{PaneID: "p", TerminalID: "t"}, Status: "blocked", EventID: "event", ApprovalFingerprint: "fp", Options: []string{"允许", "拒绝"}}
	p, err := ApprovalPayload(a, a, 1)
	if err != nil || p["choice"] != "拒绝" || p["event_id"] != "event" {
		t.Fatal(p, err)
	}
	for _, change := range []func(*Agent){func(v *Agent) { v.EventID = "next" }, func(v *Agent) { v.Generation++ }, func(v *Agent) { v.ApprovalFingerprint = "next" }, func(v *Agent) { v.Status = "working" }} {
		b := a
		change(&b)
		if _, err = ApprovalPayload(b, a, 0); err == nil {
			t.Fatal("stale approval accepted")
		}
	}
	q := Interaction{ID: "q", Kind: "single_select", Options: []QuestionOption{{Index: 2}, {Index: 4}}, Other: &QuestionOther{}}
	for _, indices := range [][]int{{}, {2, 4}, {8}, {2, 2}} {
		if _, err = q.Answer(indices, false, ""); err == nil {
			t.Fatal("invalid answer", indices)
		}
	}
	if _, err = q.Answer([]int{2}, true, "other"); err == nil {
		t.Fatal("single select allowed two answers")
	}
	if _, err = q.Answer(nil, true, ""); err == nil {
		t.Fatal("empty other accepted")
	}
	if p, err = q.Answer([]int{4}, false, "ignored"); err != nil || p["other_text"] != "" {
		t.Fatal(p, err)
	}
}
func TestUploadsValidateChunksHashesAndCancel(t *testing.T) {
	agent := Agent{Target: Target{ServerSessionID: "primary", PaneID: "p", TerminalID: "t", Generation: 2, AgentSessionID: "session", BindingEpoch: 7}, Cwd: "/work"}
	file := UploadFile{Name: "report.txt", MediaType: "text/plain", Data: []byte("hello 世界")}
	for _, variant := range []string{"success", "bad_ack", "bad_final", "cancel"} {
		t.Run(variant, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var received []byte
			cancelled, finished := false, false
			progress := int64(0)
			req := fileRequester(func(ctx context.Context, m Message) (Message, error) {
				if m["target"] != agent.Target {
					t.Fatal("target changed")
				}
				switch Text(m, "type") {
				case "upload_begin":
					return Message{"upload_id": "upload", "chunk_bytes": 4}, nil
				case "upload_chunk":
					part, err := base64.StdEncoding.DecodeString(Text(m, "data"))
					if err != nil || Text(m, "sha256") != digest(part) {
						t.Fatal("wrong chunk hash")
					}
					received = append(received, part...)
					if variant == "cancel" {
						cancel()
						return nil, context.Canceled
					}
					seq := Number(m, "sequence") + 1
					if variant == "bad_ack" {
						seq++
					}
					return Message{"next_sequence": seq}, nil
				case "upload_finish":
					finished = true
					if !bytes.Equal(received, file.Data) {
						t.Fatal("file corrupted")
					}
					hashes := m["files"].([]Message)
					if Text(hashes[0], "sha256") != digest(file.Data) {
						t.Fatal("wrong final hash")
					}
					hash := digest(file.Data)
					if variant == "bad_final" {
						hash = "bad"
					}
					return Message{"attachments": []Attachment{{Ref: strings.Repeat("a", 32), Name: file.Name, Bytes: int64(len(file.Data)), SHA256: hash, ExpiresAt: time.Now().Add(time.Hour)}}}, nil
				case "upload_cancel":
					cancelled = true
					if ctx.Err() != nil {
						t.Fatal("cleanup used cancelled context")
					}
					return Message{}, nil
				}
				return nil, errors.New("unexpected request")
			})
			atts, err := Upload(ctx, req, agent, []UploadFile{file}, func(_ string, n, total int64) {
				if n <= progress || n > total {
					t.Fatal("invalid progress")
				}
				progress = n
			})
			if variant == "success" {
				if err != nil || len(atts) != 1 || cancelled || !finished || progress != int64(len(file.Data)) {
					t.Fatal(atts, err, cancelled)
				}
			} else if err == nil || !cancelled {
				t.Fatal("failed upload not cancelled", variant, err)
			}
		})
	}
}
func TestExpiredAttachmentsAndInvitationValidation(t *testing.T) {
	now := time.Now()
	att := Attachment{Ref: strings.Repeat("a", 32), ExpiresAt: now.Add(time.Minute)}
	prompt, err := PromptWithAttachments("task", []Attachment{att}, now)
	if err != nil || !strings.Contains(prompt, "Attachment: "+att.Ref) {
		t.Fatal(prompt, err)
	}
	if _, err = PromptWithAttachments("task", []Attachment{att}, now.Add(time.Hour)); err == nil {
		t.Fatal("expired attachment sent")
	}
	c := Computer{Address: "localhost:8377", RelayID: encode(make([]byte, 16)), Rendezvous: encode(make([]byte, 32)), Name: "电脑"}
	data := Message{"invitation": Message{"invitation_id": "abcdefghijklmnop", "version": 1, "secret": encode(make([]byte, 32)), "expires_at": now.Add(time.Minute).Format(time.RFC3339)}}
	link, err := InvitationLink(c, data, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ParseInvitation(link, "", now); err != nil {
		t.Fatal(err)
	}
	Object(data["invitation"])["secret"] = "bad"
	if _, err = InvitationLink(c, data, now); err == nil {
		t.Fatal("invalid invitation exposed")
	}
}
