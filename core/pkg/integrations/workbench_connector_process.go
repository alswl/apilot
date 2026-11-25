package integrations

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
)

var (
	ErrWorkbenchConnectorProtocol = errors.New("Connector protocol failure")
	ErrWorkbenchConnectorProcess  = errors.New("Connector process failure")
)

type WorkbenchConnectorProcess struct {
	pkg       WorkbenchConnectorPackage
	namespace string
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	active    sync.WaitGroup
}

func NewWorkbenchConnectorProcess(pkg WorkbenchConnectorPackage, namespace string) *WorkbenchConnectorProcess {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkbenchConnectorProcess{pkg: pkg, namespace: namespace, ctx: ctx, cancel: cancel}
}

func (p *WorkbenchConnectorProcess) Fresh() *WorkbenchConnectorProcess {
	return NewWorkbenchConnectorProcess(p.pkg, p.namespace)
}

func (p *WorkbenchConnectorProcess) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancel()
}

func (p *WorkbenchConnectorProcess) Close() {
	p.Stop()
	p.active.Wait()
}

func (p *WorkbenchConnectorProcess) Describe(ctx context.Context) (protocol.Description, error) {
	var description protocol.Description
	result, err := p.Call(ctx, protocol.Request{ID: "describe", Method: "describe"}, nil)
	if err != nil {
		return description, err
	}
	m := p.pkg.Manifest
	if json.Unmarshal(result, &description) != nil || description.ConnectorID != m.ConnectorID ||
		description.ReleaseVersion != m.ReleaseVersion || !slices.Equal(description.ProtocolVersions, m.ProtocolVersions) ||
		!validConnectorSchema(description.EndpointConfig) || !validConnectorSchema(description.ScopeConfig) || !validScopeTypes(description.ScopeConfig.Types) || len(description.EndpointConfig.Types) != 0 {
		return protocol.Description{}, ErrWorkbenchConnectorProtocol
	}
	return description, nil
}

func validScopeTypes(types []string) bool {
	seen := make(map[string]bool, len(types))
	for _, kind := range types {
		if (kind != protocol.ScopeProject && kind != protocol.ScopeSpace) || seen[kind] {
			return false
		}
		seen[kind] = true
	}
	return true
}

func validConnectorSchema(schema protocol.ConfigSchema) bool {
	var root struct {
		Type string `json:"type"`
	}
	return schema.Version > 0 && json.Unmarshal(schema.Schema, &root) == nil && root.Type == "object"
}

// Call delivers notifications synchronously. Callers should enqueue any work
// from the notification callback without blocking the protocol reader.
func (p *WorkbenchConnectorProcess) Call(ctx context.Context, request protocol.Request, notify func(protocol.Message) error) (json.RawMessage, error) {
	timeout := 30 * time.Second
	switch request.Method {
	case "auth.login":
		timeout = 15 * time.Minute
	case "tasks.sync", "tasks.resolve":
		timeout = 2 * time.Minute
	}
	p.mu.Lock()
	if p.ctx.Err() != nil {
		p.mu.Unlock()
		return nil, context.Canceled
	}
	p.active.Add(1)
	p.mu.Unlock()
	defer p.active.Done()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()

	if request.ID == "" || request.Method == "" {
		return nil, ErrWorkbenchConnectorProtocol
	}
	request.JSONRPC = "2.0"
	var input bytes.Buffer
	if err := protocol.WriteFrame(&input, protocol.MaxFrame, request); err != nil {
		return nil, ErrWorkbenchConnectorProtocol
	}
	if err := p.pkg.verifyEntrypoint(); err != nil {
		return nil, ErrWorkbenchConnectorProcess
	}
	stdout, childOutput, err := os.Pipe()
	if err != nil {
		return nil, ErrWorkbenchConnectorProcess
	}
	defer stdout.Close()

	cmd := exec.CommandContext(ctx, p.pkg.entrypoint, "serve", "--protocol", "1")
	cmd.Dir = filepath.Dir(p.pkg.entrypoint)
	cmd.Env = []string{"APILOT_CONNECTOR_DATA_DIR=" + p.namespace}
	deadline, _ := ctx.Deadline()
	cmd.Env = append(cmd.Env, "APILOT_CONNECTOR_DEADLINE="+deadline.UTC().Format(time.RFC3339Nano))
	cmd.Stdin = &input
	cmd.Stdout = childOutput
	// Discard diagnostics at the host boundary to avoid persisting secrets.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	kill := func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Cancel = kill
	cmd.WaitDelay = time.Second
	startErr := cmd.Start()
	childOutput.Close()
	if startErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrWorkbenchConnectorProcess
	}
	unblock := context.AfterFunc(ctx, func() { stdout.Close() })
	defer unblock()
	result, readErr := readWorkbenchConnectorResponse(stdout, request, notify)
	if readErr != nil {
		_ = kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, ErrWorkbenchConnectorProcess
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return result.Result, nil
}

func readWorkbenchConnectorResponse(stdout io.Reader, request protocol.Request, notify func(protocol.Message) error) (protocol.Message, error) {
	reader := bufio.NewReader(stdout)
	var terminal protocol.Message
	for {
		var raw json.RawMessage
		err := protocol.ReadFrame(reader, protocol.MaxFrame, &raw)
		if err == io.EOF && terminal.ID != nil {
			return terminal, nil
		}
		if err != nil || terminal.ID != nil {
			return protocol.Message{}, ErrWorkbenchConnectorProtocol
		}

		var fields map[string]json.RawMessage
		var msg protocol.Message
		if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(raw, &msg) != nil || msg.JSONRPC != "2.0" {
			return protocol.Message{}, ErrWorkbenchConnectorProtocol
		}
		_, hasID := fields["id"]
		_, hasResult := fields["result"]
		_, hasError := fields["error"]
		if hasID {
			if msg.ID == nil || *msg.ID != request.ID || fields["method"] != nil || fields["params"] != nil ||
				hasResult == hasError || (hasError && (msg.Error == nil || !validConnectorError(msg.Error))) {
				return protocol.Message{}, ErrWorkbenchConnectorProtocol
			}
			terminal = msg
			continue
		}
		if msg.Method == "" || hasResult || hasError ||
			((request.Method == "auth.login" || msg.Method == "open_browser") && len(raw) > protocol.MaxLoginNotification) {
			return protocol.Message{}, ErrWorkbenchConnectorProtocol
		}
		if msg.Method == "open_browser" && !validWorkbenchBrowserNotification(request, msg.Params) {
			return protocol.Message{}, ErrWorkbenchConnectorProtocol
		}
		if notify != nil {
			if err := notify(msg); err != nil {
				return protocol.Message{}, err
			}
		}
	}
}

func validConnectorError(rpc *protocol.RPCError) bool {
	return rpc.Code != 0 && rpc.Message != "" && rpc.Data.Kind != ""
}

func validWorkbenchBrowserNotification(request protocol.Request, params json.RawMessage) bool {
	var browser protocol.BrowserNotification
	if request.Method != "auth.login" || json.Unmarshal(params, &browser) != nil || browser.RequestID != request.ID {
		return false
	}
	target, err := url.Parse(browser.URL)
	return err == nil && target.Scheme == "https" && target.Hostname() != "" && target.User == nil
}
