package common

// WorkbenchEvent tells clients to reread authoritative state. Sequence is scoped to Instance.
type WorkbenchEvent struct {
	Instance string `json:"instance"`
	Sequence uint64 `json:"sequence"`
	Type     string `json:"type" enum:"snapshot,changed"`
}
