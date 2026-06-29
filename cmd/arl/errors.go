package main

import (
	"errors"
	"fmt"
	"strings"
)

const (
	exitGeneric     = 1
	exitUsage       = 2
	exitNotFound    = 3
	exitAuth        = 4
	exitConflict    = 5
	exitCancelled   = 6
	exitEnvironment = 7
)

type exitCodeError interface {
	error
	ExitCode() int
}

type cliError struct {
	code int
	err  error
}

func (e *cliError) Error() string {
	return e.err.Error()
}

func (e *cliError) Unwrap() error {
	return e.err
}

func (e *cliError) ExitCode() int {
	return e.code
}

func usageError(format string, args ...any) error {
	return &cliError{code: exitUsage, err: fmt.Errorf(format, args...)}
}

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

func (e *HTTPError) ExitCode() int {
	switch e.StatusCode {
	case 401, 403:
		return exitAuth
	case 404:
		return exitNotFound
	case 409:
		return exitConflict
	default:
		return exitGeneric
	}
}

func exitCodeForError(err error) int {
	var coded exitCodeError
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unknown flag"),
		strings.Contains(msg, "requires"),
		strings.Contains(msg, "accepts "),
		strings.Contains(msg, "required"),
		strings.Contains(msg, "invalid"):
		return exitUsage
	case strings.Contains(msg, "not found"):
		return exitNotFound
	case strings.Contains(msg, "unauthorized"),
		strings.Contains(msg, "forbidden"),
		strings.Contains(msg, "permission"):
		return exitAuth
	case strings.Contains(msg, "already exists"),
		strings.Contains(msg, "conflict"):
		return exitConflict
	case strings.Contains(msg, "aborted"),
		strings.Contains(msg, "cancelled"),
		strings.Contains(msg, "interrupted"):
		return exitCancelled
	case strings.Contains(msg, "missing dependency"),
		strings.Contains(msg, "not installed"):
		return exitEnvironment
	default:
		return exitGeneric
	}
}
