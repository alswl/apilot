package integrations

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubAppInstallationChecksAllPagesAndRepositoryScope(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch r.URL.Path {
		case "/user/installations":
			if page == "" {
				w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?per_page=100&page=2>; rel="next"`, r.Host, r.URL.Path))
				fmt.Fprint(w, `{"installations":[{"id":1,"app_slug":"other"}]}`)
			} else {
				fmt.Fprint(w, `{"installations":[{"id":2,"app_slug":"apilot"}]}`)
			}
		case "/user/installations/2/repositories":
			if page == "" {
				w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?per_page=100&page=2>; rel="next"`, r.Host, r.URL.Path))
				fmt.Fprint(w, `{"repositories":[{"id":11}]}`)
			} else {
				fmt.Fprint(w, `{"repositories":[{"id":42}]}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewWorkbenchGitHubClient(server.URL, "secret", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.VerifyInstallationRepository(context.Background(), "apilot", 42); err != nil {
		t.Fatal(err)
	}
	if err := client.VerifyInstallationRepository(context.Background(), "apilot", 11); err != nil {
		t.Fatal("missed first page:", err)
	}
	if err := client.VerifyInstallationRepository(context.Background(), "apilot", 99); err == nil {
		t.Fatal("uninstalled repository accepted")
	}
}
