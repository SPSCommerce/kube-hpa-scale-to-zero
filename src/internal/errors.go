package internal

import "fmt"

type TrackableErrorType int

const (
	ErrorTypeScaling = 1
	ErrorTypeMetrics = 2
)

type TrackableError struct {
	ErrorType  TrackableErrorType
	Msg        string
	InnerError error
}

func (e TrackableError) Error() string {
	return fmt.Sprintf("%s:%s", e.Msg, e.InnerError)
}
