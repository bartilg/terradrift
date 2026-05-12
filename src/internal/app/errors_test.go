package app

import (
	"errors"
	"testing"
)

func TestExitError(t *testing.T) {
	cause := errors.New("failed")
	err := ExitError{Code: 3, Err: cause}

	if got, want := err.Error(), "failed"; got != want {
		t.Fatalf("Error()=%q want=%q", got, want)
	}
	if got := err.Unwrap(); got != cause {
		t.Fatalf("Unwrap() returned %v want %v", got, cause)
	}
	if got, want := err.ExitCode(), 3; got != want {
		t.Fatalf("ExitCode()=%d want=%d", got, want)
	}
}

func TestExitErrorDefaultsToOne(t *testing.T) {
	err := ExitError{}
	if got, want := err.Error(), ""; got != want {
		t.Fatalf("nil error string mismatch: got=%q want=%q", got, want)
	}
	if got, want := err.ExitCode(), 1; got != want {
		t.Fatalf("zero code should default to 1: got=%d want=%d", got, want)
	}
}

func TestRuntimeError(t *testing.T) {
	err := RuntimeError("scan %s", "failed")
	exitErr, ok := err.(ExitError)
	if !ok {
		t.Fatalf("RuntimeError should return ExitError, got %T", err)
	}
	if got, want := exitErr.ExitCode(), 1; got != want {
		t.Fatalf("ExitCode()=%d want=%d", got, want)
	}
	if got, want := exitErr.Error(), "scan failed"; got != want {
		t.Fatalf("Error()=%q want=%q", got, want)
	}
}
