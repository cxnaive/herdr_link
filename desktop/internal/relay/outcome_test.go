package relay

import (
	"strings"
	"testing"
)

func TestUploadErrorsHaveReadableMessagesAndDefiniteOutcomes(t *testing.T) {
	for _, value := range []any{
		Message{"code": "attachment_unknown_mime"},
		map[string]any{"code": "attachment_file_too_large", "args": map[string]any{"max_bytes": 20971520}},
		"attachment_upload_expired",
	} {
		_, err := commandReply(Message{"type": "upload_begin_result", "error": value})
		if err == nil || UnknownOutcome(err) || strings.Contains(err.Error(), "map[") || strings.Contains(err.Error(), "attachment_") {
			t.Fatal("raw upload error or unknown outcome", err)
		}
	}
	_, err := commandReply(Message{"error": Message{"code": "attachment_unknown_mime"}})
	if !strings.Contains(err.Error(), "更新电脑端 Relay") {
		t.Fatal("old Relay rejection must explain how to enable generic uploads", err)
	}
}
