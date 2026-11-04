package tv

import "errors"

// ErrBusy means another app-launch sequence is already walking the grid.
var ErrBusy = errors.New("another launch is in progress")

// BadRequestError is returned for malformed or unsupported actions. The HTTP
// layer maps it to 400 instead of 503.
type BadRequestError struct {
	Message string
}

func (e *BadRequestError) Error() string {
	return e.Message
}

func badRequest(message string) error {
	return &BadRequestError{Message: message}
}
