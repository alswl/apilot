package common

type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func (e *Error) Error() string { return e.Message }

func Err(code, message string) *Error { return &Error{code, message, map[string]any{}} }
