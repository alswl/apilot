package protocol

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sync"
)

type Notify func(method string, params any) error
type Handler func(context.Context, Request, Notify) (any, *RPCError)

func Serve(ctx context.Context, args []string, input io.Reader, output io.Writer, description Description, handle Handler) error {
	if !slices.Equal(args, []string{"serve", "--protocol", "1"}) {
		return errors.New("usage: serve --protocol 1")
	}
	var request Request
	reader := bufio.NewReader(input)
	if err := ReadFrame(reader, MaxFrame, &request); err != nil {
		return err
	}
	if _, err := reader.ReadByte(); err != io.EOF || request.JSONRPC != "2.0" || request.ID == "" || request.Method == "" {
		return ErrFrame
	}
	var outputMu sync.Mutex
	finished := false
	notify := func(method string, params any) error {
		outputMu.Lock()
		defer outputMu.Unlock()
		if finished || method == "" {
			return ErrFrame
		}
		body, err := json.Marshal(params)
		if err != nil {
			return ErrFrame
		}
		limit := MaxFrame
		if request.Method == "auth.login" || method == "open_browser" {
			limit = MaxLoginNotification
		}
		return WriteFrame(output, limit, Message{JSONRPC: "2.0", Method: method, Params: body})
	}
	response := Message{JSONRPC: "2.0", ID: &request.ID}
	var result any
	switch {
	case request.Method == "describe":
		result = description
	case handle != nil:
		result, response.Error = handle(ctx, request, notify)
	default:
		response.Error = &RPCError{Code: -32601, Message: "Method not supported", Data: ErrorData{Kind: Unsupported}}
	}
	outputMu.Lock()
	defer outputMu.Unlock()
	finished = true
	if response.Error == nil {
		body, err := json.Marshal(result)
		if err != nil {
			return ErrFrame
		}
		response.Result = body
	}
	return WriteFrame(output, MaxFrame, response)
}
