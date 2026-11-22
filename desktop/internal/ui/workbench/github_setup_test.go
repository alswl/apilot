package workbench

import (
	"net"
	"net/http"
	"testing"
	"time"
)

func TestGitHubSetupRedirect(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	_ = probe.Close()
	completed := make(chan struct{}, 2)
	cancel, err := listenGitHubSetup(address, func() { completed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	for _, test := range []struct {
		query  string
		status int
	}{
		{"installation_id=bogus&setup_action=install", http.StatusBadRequest},
		{"installation_id=123&setup_action=install", http.StatusOK},
		{"installation_id=123&setup_action=update", http.StatusOK},
	} {
		response, err := http.Get("http://" + address + "/github/setup?" + test.query)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != test.status {
			t.Fatalf("%s: got %d", test.query, response.StatusCode)
		}
	}
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("missing setup signal")
	}
	select {
	case <-completed:
		t.Fatal("setup signaled twice")
	default:
	}
}
