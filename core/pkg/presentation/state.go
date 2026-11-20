package presentation

type Token uint64

// OperationState belongs to the UI thread; workers only carry tokens and values.
type OperationState struct {
	generation Token
	busy       bool
}

func (s *OperationState) Begin() (Token, bool) {
	if s.busy {
		return 0, false
	}
	s.generation++
	s.busy = true
	return s.generation, true
}
func (s *OperationState) Complete(t Token) bool {
	if !s.busy || t != s.generation {
		return false
	}
	s.busy = false
	return true
}
func (s *OperationState) Invalidate() { s.generation++; s.busy = false }
func (s *OperationState) Busy() bool  { return s.busy }

// Coalesce refreshes without invalidating the result of a slow in-flight query.
type RefreshState struct {
	OperationState
	pending bool
}

func (s *RefreshState) Begin() (Token, bool) {
	if s.Busy() {
		s.pending = true
		return 0, false
	}
	s.pending = false
	return s.OperationState.Begin()
}
func (s *RefreshState) Pending() bool { return s.pending }
func (s *RefreshState) Invalidate()   { s.OperationState.Invalidate(); s.pending = false }

type LogSelection struct {
	RunID, Stream string
	Offset        int64
	Generation    Token
}
