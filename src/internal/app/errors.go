package app

import "fmt"

type ExitError struct {
	Code int
	Err  error
}

func (e ExitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e ExitError) Unwrap() error {
	return e.Err
}

func (e ExitError) ExitCode() int {
	if e.Code == 0 {
		return 1
	}
	return e.Code
}

func RuntimeError(format string, args ...any) error {
	return ExitError{Code: 1, Err: fmt.Errorf(format, args...)}
}
