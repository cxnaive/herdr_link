package upload

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGenericAttachmentsKeepBytesSuffixAndScopeAfterRestart(t *testing.T) {
	for _, row := range []struct {
		name, media, suffix string
	}{
		{"训练录像.MP4", "video/mp4", ".mp4"},
		{"weights.safetensors", "", ".safetensors"},
		{"archive.zip", "application/zip", ".zip"},
		{"notes.dat", "application/x-custom", ".dat"},
		{"LICENSE", "application/octet-stream", ".bin"},
		{"data.未知", "application/x-custom", ".bin"},
		{"script.exe", "image/png", ".exe"},
		{"image.png", "image/png", ".png"},
		{"plain.txt", "text/plain", ".txt"},
	} {
		t.Run(row.name, func(t *testing.T) {
			config := Config{Root: t.TempDir()}
			manager, err := NewManager(config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { manager.Close() })
			target := uploadTarget(2)
			// Opaque bytes are intentional: the uploader need not decode a video
			// or a model to transfer it. Include NULs and invalid UTF-8.
			content := bytes.Repeat([]byte{0, 0xff, 0x81, 42}, 3*1024*1024/4)
			begin := beginOne(t, manager, target, row.name, row.media, int64(len(content)))
			for offset, seq := 0, 0; offset < len(content); seq++ {
				end := min(offset+begin.ChunkBytes, len(content))
				sendChunk(t, manager, target, begin.UploadID, 0, seq, content[offset:end])
				offset = end
			}
			finished, err := manager.Finish(FinishRequest{Target: target, UploadID: begin.UploadID, Files: []FileDigest{{FileIndex: 0, SHA256: digestFor(content)}}})
			if err != nil || len(finished.Attachments) != 1 {
				t.Fatal("generic upload failed", finished, err)
			}
			att := finished.Attachments[0]
			if att.MediaType != genericMediaType || att.Name != row.name || filepath.Base(att.Path) != att.Ref+row.suffix || att.Bytes != int64(len(content)) {
				t.Fatal("generic attachment metadata changed", att)
			}
			stored, err := os.ReadFile(att.Path)
			if err != nil || !bytes.Equal(stored, content) || att.SHA256 != digestFor(stored) {
				t.Fatal("stored bytes differ", err)
			}
			if info, err := os.Stat(att.Path); err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("attachment must remain private and non-executable", info, err)
			}
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}
			restarted, err := NewManager(config)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			resolved, err := restarted.Resolve(uploadTarget(3), att.Ref)
			if err != nil || resolved.Path != att.Path || resolved.MediaType != genericMediaType {
				t.Fatal("generic file lost on restart", resolved, err)
			}
			other := uploadTarget(3)
			other.TerminalID = "other"
			if _, err := restarted.Resolve(other, att.Ref); errorCode(t, err) != "attachment_scope_mismatch" {
				t.Fatal("file crossed Agent boundaries")
			}
		})
	}
}
