package integrations

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestGitHubRateSerializesAccountsAndCancelsWait(t *testing.T) {
	rate := NewWorkbenchGitHubRate(2)
	release, err := rate.acquire(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := rate.acquire(ctx, 1, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("same account was not serialized: %v", err)
	}
	other, err := rate.acquire(context.Background(), 2, false)
	if err != nil {
		t.Fatal(err)
	}
	other()
}

func TestGitHubRateBackoffSharedAndOtherAccountsProceed(t *testing.T) {
	rate := NewWorkbenchGitHubRate(2)
	release, err := rate.acquire(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	rate.observe(1, &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"60"}}})
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := rate.acquire(ctx, 1, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("backoff not applied: %v", err)
	}
	other, err := rate.acquire(context.Background(), 2, false)
	if err != nil {
		t.Fatal(err)
	}
	other()
}

func TestGitHubRateSpacesWrites(t *testing.T) {
	rate := NewWorkbenchGitHubRate(1)
	release, err := rate.acquire(context.Background(), 1, true)
	if err != nil {
		t.Fatal(err)
	}
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := rate.acquire(ctx, 1, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("write interval not enforced: %v", err)
	}
}
