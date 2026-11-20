package presentation

import "testing"

func TestOperationTokens(t *testing.T) {
	var s OperationState
	a, ok := s.Begin()
	if !ok {
		t.Fatal("begin")
	}
	if _, ok = s.Begin(); ok {
		t.Fatal("duplicate")
	}
	s.Invalidate()
	b, _ := s.Begin()
	if s.Complete(a) || !s.Busy() {
		t.Fatal("old completion cleared new operation")
	}
	if !s.Complete(b) || s.Busy() {
		t.Fatal("completion")
	}
}
func TestRefreshCoalescesWithoutStarvation(t *testing.T) {
	var s RefreshState
	a, ok := s.Begin()
	if !ok {
		t.Fatal("start")
	}
	for i := 0; i < 20; i++ {
		if _, ok = s.Begin(); ok {
			t.Fatal("overlapping refresh")
		}
	}
	if !s.Complete(a) || !s.Pending() {
		t.Fatal("slow current result must apply and request next refresh")
	}
	b, ok := s.Begin()
	if !ok {
		t.Fatal("pending refresh")
	}
	s.Invalidate()
	if s.Complete(b) {
		t.Fatal("closed generation applied")
	}
}
func TestLogSelectionRejectsOldStreamAndOffset(t *testing.T) {
	a := LogSelection{RunID: "r", Stream: "stdout", Offset: 0, Generation: 1}
	b := a
	b.Stream = "stderr"
	if a == b {
		t.Fatal("stream")
	}
	b = a
	b.Offset = 3
	if a == b {
		t.Fatal("offset")
	}
	b = a
	b.Generation++
	if a == b {
		t.Fatal("generation")
	}
}
