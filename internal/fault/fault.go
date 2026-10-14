// Package fault defines application errors independently of HTTP transport.
package fault

import "fmt"

type Error struct {
	Code    string
	Message string
	Fields  map[string]string
}

func (e *Error) Error() string        { return e.Message }
func New(code, message string) *Error { return &Error{Code: code, Message: message} }
func Field(field, message string) *Error {
	return &Error{Code: "VALIDATION_ERROR", Message: "输入不符合要求", Fields: map[string]string{field: message}}
}
func Missing(entity string) *Error { return New("NOT_FOUND", fmt.Sprintf("%s不存在", entity)) }
