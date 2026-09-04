package main

import (
	"os"
	"testing"
)

func TestRunStateRoundtrip(t *testing.T) {
	home := t.TempDir()
	if loadState(home) != nil {
		t.Fatal("no file must mean no state")
	}
	e := &engine{f: &facts{homeDir: home}, p: &plan{}}
	e.markStepDone("legacy")
	e.backupDir = "/tmp/backup-x"
	e.markStepDone("backup")
	e.markStepDone("backup") // idempotent

	s := loadState(home)
	if s == nil || len(s.Completed) != 2 || !s.has("legacy") || !s.has("backup") || s.has("verify") {
		t.Fatalf("state wrong: %+v", s)
	}
	if s.BackupDir != "/tmp/backup-x" {
		t.Fatalf("backup dir not recorded: %+v", s)
	}

	e.clearState()
	if loadState(home) != nil {
		t.Fatal("clearState must remove the resume file")
	}
}

func TestRunStateIgnoresGarbage(t *testing.T) {
	home := t.TempDir()
	p := statePath(home)
	os.MkdirAll(home+"/.local/state/ryoku", 0o755)
	os.WriteFile(p, []byte("not json"), 0o644)
	if loadState(home) != nil {
		t.Fatal("garbage state must be ignored")
	}
	os.WriteFile(p, []byte(`{"completed":[]}`), 0o644)
	if loadState(home) != nil {
		t.Fatal("empty completed list is not a resumable run")
	}
}

func TestDryRunWritesNoState(t *testing.T) {
	home := t.TempDir()
	e := &engine{f: &facts{homeDir: home}, p: &plan{}, dry: true}
	e.markStepDone("legacy")
	if loadState(home) != nil {
		t.Fatal("dry runs must not write state")
	}
}
