package mixedmodellint

import (
	"errors"
	"strings"
)

// Error codes (summary_rules §3). Only the codes this program produces exist.
const (
	EINTERNAL = "internal" // a bug or an I/O failure
	EINVALID  = "invalid"  // an input the user supplied is malformed
	ENOTFOUND = "not_found"
)

// Error is the one error type of the application. A leaf error carries Code
// and Message; a wrapping error carries Op ("package.Func") and Err. Never set
// both on one value.
type Error struct {
	Code    string
	Message string
	Op      string
	Err     error
}

// Error renders the chain as "op: op: message", a one-line logical trace.
func (e *Error) Error() string {
	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(": ")
	}
	switch {
	case e.Err != nil:
		b.WriteString(e.Err.Error())
	case e.Message != "":
		b.WriteString(e.Message)
	default:
		b.WriteString(e.Code)
	}
	return b.String()
}

// Unwrap returns the wrapped error, so errors.Is and errors.As see through.
func (e *Error) Unwrap() error { return e.Err }

// ErrorCode returns the code of the innermost *Error with one, EINTERNAL for
// any other non-nil error, and "" for nil.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	for e := range errorChain(err) {
		if e.Code != "" {
			return e.Code
		}
	}
	return EINTERNAL
}

// ErrorMessage returns the message of the innermost *Error with one, or a
// generic message for any other non-nil error.
func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	for e := range errorChain(err) {
		if e.Message != "" {
			return e.Message
		}
	}
	return "internal error"
}

// errorChain yields every *Error in err's chain, outermost first.
func errorChain(err error) func(yield func(*Error) bool) {
	return func(yield func(*Error) bool) {
		for err != nil {
			var e *Error
			if !errors.As(err, &e) {
				return
			}
			if !yield(e) {
				return
			}
			err = e.Err
		}
	}
}
