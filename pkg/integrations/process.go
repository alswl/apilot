package integrations

type Spec struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
	Cwd        string   `json:"cwd"`
	Env        []string `json:"env"`
	TimeoutMS  int64    `json:"timeout_ms"`
	LogLimit   int64    `json:"log_limit"`
}
type Result struct {
	Status    string `json:"status"`
	ExitCode  *int   `json:"exit_code"`
	ErrorCode string `json:"error_code,omitempty"`
	Error     string `json:"error,omitempty"`
	Truncated bool   `json:"truncated"`
}
type Event struct {
	Type   string  `json:"type"`
	PID    int     `json:"pid,omitempty"`
	Result *Result `json:"result,omitempty"`
}
type Process struct {
	Done <-chan Result
	PID  int
	stop func()
}

func (p *Process) Stop() { p.stop() }
