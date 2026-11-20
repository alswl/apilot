package common

type LogChunk struct {
	Stream     string `json:"stream"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	Data       []byte `json:"data_base64"`
	EOF        bool   `json:"eof"`
	Truncated  bool   `json:"truncated"`
}
