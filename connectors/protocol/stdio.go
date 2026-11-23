package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

var ErrFrame = errors.New("invalid Connector protocol frame")

// ReadFrame reads a complete newline-terminated JSON frame with bounded memory.
func ReadFrame(reader *bufio.Reader, limit int, target any) error {
	var frame []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(frame)+len(part) > limit+1 || (err != nil && len(frame)+len(part) > limit) {
			return ErrFrame
		}
		frame = append(frame, part...)
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(frame) == 0:
			return io.EOF
		case err != nil:
			return ErrFrame
		}
		if !utf8.Valid(frame) || json.Unmarshal(frame, target) != nil {
			return ErrFrame
		}
		return nil
	}
}

func WriteFrame(writer io.Writer, limit int, value any) error {
	frame, err := json.Marshal(value)
	if err != nil || len(frame) > limit {
		return ErrFrame
	}
	n, err := writer.Write(append(frame, '\n'))
	if err == nil && n != len(frame)+1 {
		return io.ErrShortWrite
	}
	return err
}
