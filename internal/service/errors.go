package service

import "fmt"

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

type NotFoundError struct {
	Entity string
	ID     int64
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found", e.Entity)
}

type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string { return e.Message }
