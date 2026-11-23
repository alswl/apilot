package integrations

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

type workbenchEventConn struct{ net.Conn }

func (c workbenchEventConn) Read(data []byte) (int, error) {
	if err := c.SetReadDeadline(time.Now().Add(25 * time.Second)); err != nil {
		return 0, err
	}
	return c.Conn.Read(data)
}

// WatchWorkbenchEvents consumes one connection. Every new connection starts with a snapshot notice.
func WatchWorkbenchEvents(ctx context.Context, socket string, receive func(common.WorkbenchEvent) error) error {
	dialer := net.Dialer{Timeout: 500 * time.Millisecond}
	transport := &http.Transport{
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 3 * time.Second,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, "unix", socket)
			if err != nil {
				return nil, err
			}
			return workbenchEventConn{conn}, nil
		},
	}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client, err := NewClient("http://apilot", WithHTTPClient(httpClient))
	if err != nil {
		return err
	}
	resp, err := client.WatchWorkbenchEvents(ctx, func(_ context.Context, req *http.Request) error {
		req.Header.Set(common.WorkbenchAPIVersionHeader, common.WorkbenchAPIVersion)
		req.Header.Set("Accept", "text/event-stream")
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return common.Err("APP_NOT_RUNNING", "Apilot event socket is unavailable")
		}
		return common.Err("IPC_UNAVAILABLE", err.Error())
	}
	defer resp.Body.Close()
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK || media != "text/event-stream" {
		if resp.StatusCode == http.StatusServiceUnavailable {
			return common.Err("IPC_UNAVAILABLE", "Apilot event stream is temporarily unavailable (HTTP 503)")
		}
		return common.Err("PROTOCOL_MISMATCH", fmt.Sprintf("Unexpected Apilot event response (HTTP %d)", resp.StatusCode))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 4096)
	var pending string
	var prior common.WorkbenchEvent
	for scanner.Scan() {
		line := scanner.Text()
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			if pending != "" {
				return common.Err("PROTOCOL_MISMATCH", "Unexpected event data")
			}
			pending = data
		}
		if line != "" || pending == "" {
			continue
		}
		var event common.WorkbenchEvent
		if json.Unmarshal([]byte(pending), &event) != nil || event.Instance == "" || event.Sequence == 0 ||
			(prior.Sequence == 0 && event.Type != "snapshot") ||
			(prior.Sequence != 0 && (event.Type != "changed" || event.Instance != prior.Instance || event.Sequence <= prior.Sequence)) {
			return common.Err("PROTOCOL_MISMATCH", "Invalid Apilot event")
		}
		if err := receive(event); err != nil {
			return err
		}
		prior, pending = event, ""
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var network net.Error
		if errors.As(err, &network) || errors.Is(err, io.ErrUnexpectedEOF) {
			return common.Err("IPC_UNAVAILABLE", err.Error())
		}
		return common.Err("PROTOCOL_MISMATCH", err.Error())
	}
	return io.EOF
}
