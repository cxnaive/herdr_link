package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestApplicationVersionMatchesProject(t *testing.T) {
	data, err := os.ReadFile("../VERSION.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Project struct {
			Version string `json:"version"`
		} `json:"project"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if got := applicationVersion(); got != manifest.Project.Version {
		t.Fatalf("desktop version %q differs from project %q", got, manifest.Project.Version)
	}
}
