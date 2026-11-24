package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var approvalForm = template.Must(template.New("approval").Parse(`<!doctype html><meta charset="utf-8">
<title>Apilot fixture login</title><h1>Apilot fixture login</h1>
<form method="post"><button>Authorize fixture account</button></form>`))

func validApprovalID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func fixtureBrowser(args []string) error {
	flags := flag.NewFlagSet("browser", flag.ContinueOnError)
	address := flags.String("listen", "127.0.0.1:8421", "Loopback HTTPS address")
	cert := flags.String("cert", "", "Local TLS certificate")
	key := flags.String("key", "", "Local TLS certificate key")
	dir := flags.String("data-dir", "", "Fixture Connector data namespace")
	if err := flags.Parse(args); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil || !net.ParseIP(host).IsLoopback() || *cert == "" || *key == "" || !filepath.IsAbs(*dir) || flags.NArg() != 0 {
		return fmt.Errorf("fixture browser requires loopback address, TLS files, and absolute data directory")
	}
	server := &http.Server{Addr: *address, Handler: fixtureApprovalHandler(*dir), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, MaxHeaderBytes: 8192}
	return server.ListenAndServeTLS(*cert, *key)
}

func fixtureApprovalHandler(dir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/fixture-login", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if !validApprovalID(id) {
			http.NotFound(w, r)
			return
		}
		pending := filepath.Join(dir, "pending-"+id)
		if info, err := os.Stat(pending); err != nil || time.Since(info.ModTime()) > 10*time.Minute {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = approvalForm.Execute(w, nil)
		case http.MethodPost:
			if err := os.WriteFile(filepath.Join(dir, "approved-"+id), nil, 0600); err != nil {
				http.Error(w, "Fixture authorization unavailable", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Allow", "GET, POST")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return mux
}
