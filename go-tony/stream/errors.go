package stream

// Error represents a stream error.
type Error struct {
	Msg string
}

func (e *Error) Error() string {
	return e.Msg
}
