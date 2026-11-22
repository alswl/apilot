package workbench

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo"
)

const gitHubSetupAddress = "127.0.0.1:43781"

// listenGitHubSetup treats the redirect as a signal only. Repository access is
// checked independently using the user's GitHub credential before saving.
func listenGitHubSetup(address string, completed func()) (func(), error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	cancel := sync.OnceFunc(func() { _ = server.Close() })
	mux.HandleFunc("/github/setup", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseUint(r.URL.Query().Get("installation_id"), 10, 64)
		action := r.URL.Query().Get("setup_action")
		if r.Method != http.MethodGet || err != nil || id == 0 || (action != "install" && action != "update") {
			http.Error(w, "Invalid GitHub setup redirect", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		fmt.Fprint(w, "<!doctype html><meta charset=utf-8><title>Apilot</title><p>请返回 Apilot，继续 GitHub 授权。</p>")
		_ = http.NewResponseController(w).Flush()
		once.Do(func() { go completed() })
	})
	go func() { _ = server.Serve(listener) }()
	time.AfterFunc(10*time.Minute, cancel)
	return cancel, nil
}

func (m *mockup) startGitHubSetup(edit *connectorEdit) {
	if edit.githubSetupCancel != nil {
		edit.githubSetupCancel()
		edit.githubSetupCancel = nil
	}
	cancel, err := listenGitHubSetup(gitHubSetupAddress, func() {
		mygo.RunOnMain(func() {
			if m.connectorEditor != edit {
				return
			}
			if edit.githubSetupCancel != nil {
				edit.githubSetupCancel()
				edit.githubSetupCancel = nil
			}
			m.continueGitHubSetup(edit)
			m.focusGitHubConnection()
			m.invalidateLiveWindow()
		})
	})
	if err != nil {
		edit.problem = "无法启动 GitHub 授权回调"
		return
	}
	edit.githubSetupCancel = cancel
	if err := mygo.Shell.OpenExternal("https://github.com/apps/" + services.GitHubAppSlug + "/installations/new"); err != nil {
		cancel()
		edit.githubSetupCancel = nil
		edit.problem = "无法打开 GitHub"
	}
}
