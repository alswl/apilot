package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
)

type client struct {
	path string
	json bool
	out  io.Writer
}

func (c *client) call(method string, p any) (json.RawMessage, error) {
	return services.NewRemote(c.path).Call(method, p)
}
func (c *client) show(data any) error {
	if c.json {
		return json.NewEncoder(c.out).Encode(common.Response{Version: 1, Data: data})
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) == nil {
		if items, ok := object["items"].([]any); ok {
			w := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATE\tID")
			for _, item := range items {
				row, ok := item.(map[string]any)
				if !ok {
					continue
				}
				name, _ := row["name"].(string)
				if name == "" {
					name, _ = row["task_name"].(string)
				}
				if name == "" {
					name, _ = row["summary"].(string)
				}
				state, _ := row["status"].(string)
				if state == "" {
					state, _ = row["health"].(string)
				}
				if state == "" {
					if run, ok := row["last_run"].(map[string]any); ok {
						state, _ = run["status"].(string)
					}
				}
				if enabled, ok := row["enabled"].(bool); ok && !enabled {
					state += " (auto off)"
				}
				fmt.Fprintf(w, "%s\t%s\t%v\n", name, state, row["id"])
			}
			return w.Flush()
		}
		if state, ok := object["state"].(string); ok {
			_, e := fmt.Fprintf(c.out, "apilot %s\nRunning tasks: %v\nNeeds attention: %v\n", state, object["running_count"], object["attention_count"])
			return e
		}
		if v, ok := object["app_version"].(string); ok && len(object) == 2 {
			_, e := fmt.Fprintln(c.out, "apilot "+v)
			return e
		}
	}
	b, e := json.MarshalIndent(data, "", "  ")
	if e == nil {
		_, e = fmt.Fprintln(c.out, string(b))
	}
	return e
}
func (c *client) invoke(method string, p any) error {
	b, e := c.call(method, p)
	if e != nil {
		return e
	}
	return c.show(b)
}

type reported struct{ err error }

func (e *reported) Error() string { return e.err.Error() }
func exitCode(e *common.Error) int {
	switch e.Code {
	case "INVALID_ARGUMENT":
		return 2
	case "APP_NOT_RUNNING":
		return 3
	case "NOT_FOUND":
		return 4
	case "CONFLICT", "STALE_REVISION":
		return 5
	case "IPC_TIMEOUT", "IPC_UNAVAILABLE", "PROTOCOL_MISMATCH":
		return 6
	default:
		return 1
	}
}
func readEnv(path string) (map[string]string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, common.Err("INVALID_ARGUMENT", "Cannot read environment file")
	}
	out := map[string]string{}
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || k == "" {
			return nil, common.Err("INVALID_ARGUMENT", fmt.Sprintf("Invalid environment file line %d", i+1))
		}
		out[k] = v
	}
	return out, nil
}
