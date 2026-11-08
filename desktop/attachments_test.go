package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDroppedVideoAndUnknownFilesKeepOriginalBytes(t *testing.T) {
	data := bytes.Repeat([]byte{0, 0xff, 42, 1}, 3*1024*1024/4)
	for _, name := range []string{"训练录像.mp4", "weights.safetensors", "no-extension"} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		files, err := readUploadFiles([]string{path})
		if err != nil || len(files) != 1 || files[0].Name != name || !bytes.Equal(files[0].Data, data) || files[0].MediaType == "" {
			t.Fatal("opaque file was rejected or altered", name, err)
		}
	}
}
