//go:build darwin && cgo

package integrations

import (
	"context"
	"os"
	"slices"
	"testing"
)

func TestWorkbenchKeychainRoundTrip(t *testing.T) {
	if os.Getenv("APILOT_TEST_KEYCHAIN") != "1" {
		t.Skip("set APILOT_TEST_KEYCHAIN=1 to exercise the user's Keychain")
	}
	namespace, err := WorkbenchCredentialNamespace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := NewWorkbenchKeychain(namespace)
	ctx := context.Background()
	const ref, secret = "round-trip-test", "apilot-test-secret"
	if err := store.Put(ctx, ref, secret); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Delete(ctx, ref); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	value, err := store.Get(ctx, ref)
	if err != nil || value != secret {
		t.Fatalf("credential round trip failed: %v", err)
	}
	updater, ok := store.(WorkbenchCredentialUpdater)
	if !ok {
		t.Fatal("Keychain does not support atomic replacement")
	}
	if err := updater.Replace(ctx, ref, "rotated-test-secret"); err != nil {
		t.Fatal(err)
	}
	value, err = store.Get(ctx, ref)
	if err != nil || value != "rotated-test-secret" {
		t.Fatalf("credential replacement failed: %v", err)
	}
	refs, err := store.List(ctx)
	if err != nil || !slices.Contains(refs, ref) {
		t.Fatalf("credential reference missing: %v", err)
	}
	if err := store.Delete(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, ref); err == nil {
		t.Fatal("deleted credential remains available")
	}
}
