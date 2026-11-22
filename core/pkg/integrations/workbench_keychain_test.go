package integrations

import (
	"context"
	"path/filepath"
	"testing"
)

func TestWorkbenchCredentialNamespaceSeparatesDataDirectories(t *testing.T) {
	root := t.TempDir()
	first, err := WorkbenchCredentialNamespace(filepath.Join(root, "first"))
	if err != nil {
		t.Fatal(err)
	}
	same, err := WorkbenchCredentialNamespace(filepath.Join(root, "first", "."))
	if err != nil {
		t.Fatal(err)
	}
	second, err := WorkbenchCredentialNamespace(filepath.Join(root, "second"))
	if err != nil {
		t.Fatal(err)
	}
	if first != same || first == second {
		t.Fatalf("credential namespace did not isolate data directories")
	}
}

func TestWorkbenchKeychainHonorsCanceledContext(t *testing.T) {
	store := NewWorkbenchKeychain("test.apilot.cancelled")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Put(ctx, "ref", "secret"); err != context.Canceled {
		t.Fatalf("Put returned %v", err)
	}
	if _, err := store.Get(ctx, "ref"); err != context.Canceled {
		t.Fatalf("Get returned %v", err)
	}
	if err := store.Delete(ctx, "ref"); err != context.Canceled {
		t.Fatalf("Delete returned %v", err)
	}
	if _, err := store.List(ctx); err != context.Canceled {
		t.Fatalf("List returned %v", err)
	}
}
