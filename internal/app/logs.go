package app

import (
	"github.com/alswl/apilot/internal/model"
	"io"
	"os"
	"path/filepath"
)

type LogChunk struct {
	Stream     string `json:"stream"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	Data       []byte `json:"data_base64"`
	EOF        bool   `json:"eof"`
	Truncated  bool   `json:"truncated"`
}

func (s *Service) Logs(id, stream string, offset int64, limit int) (LogChunk, error) {
	out := LogChunk{Stream: stream, Offset: offset, NextOffset: offset, Data: []byte{}}
	if (stream != "stdout" && stream != "stderr") || offset < 0 || limit < 1 || limit > 65536 {
		return out, model.Err("INVALID_ARGUMENT", "Invalid stream, offset or limit")
	}
	r, e := s.Run(id)
	if e != nil {
		return out, e
	}
	out.Truncated = r.Truncated
	f, e := os.Open(filepath.Join(r.LogPath, stream+".log"))
	if os.IsNotExist(e) {
		if r.StartedAt == nil {
			out.EOF = !r.Active()
			return out, nil
		}
		return out, model.Err("LOGS_EXPIRED", "Log file is no longer available")
	}
	if e != nil {
		return out, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return out, e
	}
	if offset > st.Size() {
		return out, model.Err("INVALID_ARGUMENT", "Offset is beyond the end of the log")
	}
	b := make([]byte, limit)
	n, e := f.ReadAt(b, offset)
	if e != nil && e != io.EOF {
		return out, e
	}
	out.Data = b[:n]
	out.NextOffset = offset + int64(n)
	out.EOF = !r.Active() && out.NextOffset >= st.Size()
	return out, nil
}
