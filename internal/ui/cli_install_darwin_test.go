package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCLIInstallPreservesExistingTool(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "App", "apilot")
	target := filepath.Join(dir, "bin", "apilot")
	if err := installCLI(source, target); err != nil {
		t.Fatal(err)
	}
	if err := installCLI(source, target); err != nil {
		t.Fatal("same install must be idempotent:", err)
	}
	if err := installCLI("/another/app/apilot", target); err == nil {
		t.Fatal("replaced another link")
	}
	os.Remove(target)
	os.WriteFile(target, []byte("existing"), 0755)
	if err := installCLI(source, target); err == nil {
		t.Fatal("overwrote existing CLI")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "existing" {
		t.Fatal("existing file changed")
	}
}
