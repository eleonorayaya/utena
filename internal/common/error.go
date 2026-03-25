package common

import (
	"errors"
	"net/http"

	"github.com/go-chi/render"
)

type ErrorCategory int

const (
	CategoryNotFound ErrorCategory = iota
	CategoryInvalidRequest
	CategoryConflict
)

type AppError struct {
	Category ErrorCategory
	Message  string
	Err      error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func NewNotFound(msg string) *AppError {
	return &AppError{Category: CategoryNotFound, Message: msg}
}

func NewInvalidRequest(msg string) *AppError {
	return &AppError{Category: CategoryInvalidRequest, Message: msg}
}

func NewConflict(msg string) *AppError {
	return &AppError{Category: CategoryConflict, Message: msg}
}

func WrapNotFound(msg string, err error) *AppError {
	return &AppError{Category: CategoryNotFound, Message: msg, Err: err}
}

func WrapInvalidRequest(msg string, err error) *AppError {
	return &AppError{Category: CategoryInvalidRequest, Message: msg, Err: err}
}

func WrapConflict(msg string, err error) *AppError {
	return &AppError{Category: CategoryConflict, Message: msg, Err: err}
}

type errResponse struct {
	Err            error `json:"-"`
	HTTPStatusCode int   `json:"-"`

	StatusText string `json:"status"`
	ErrorText  string `json:"error,omitempty"`
}

func (e *errResponse) Render(w http.ResponseWriter, r *http.Request) error {
	render.Status(r, e.HTTPStatusCode)
	return nil
}

func RenderError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		var statusCode int
		var statusText string

		switch appErr.Category {
		case CategoryNotFound:
			statusCode = http.StatusNotFound
			statusText = "Resource not found."
		case CategoryInvalidRequest:
			statusCode = http.StatusBadRequest
			statusText = "Invalid request."
		case CategoryConflict:
			statusCode = http.StatusConflict
			statusText = "Conflict."
		default:
			statusCode = http.StatusInternalServerError
			statusText = "Internal server error."
		}

		render.Render(w, r, &errResponse{
			Err:            appErr,
			HTTPStatusCode: statusCode,
			StatusText:     statusText,
			ErrorText:      appErr.Error(),
		})
		return
	}

	render.Render(w, r, &errResponse{
		Err:            err,
		HTTPStatusCode: http.StatusInternalServerError,
		StatusText:     "Internal server error.",
		ErrorText:      err.Error(),
	})
}
