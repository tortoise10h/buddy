package engine

import "fmt"

// ErrNotImplemented represents an operation that is known but not yet implemented
// for a particular execution engine (e.g. T3 mutations in Step 1).
type ErrNotImplemented struct {
	Engine    string `json:"engine"`
	Operation string `json:"operation"`
	Message   string `json:"message"`
}

func (e *ErrNotImplemented) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("%s engine %s layer is not yet implemented", e.Engine, e.Operation)
}

func (e *ErrNotImplemented) Is(target error) bool {
	_, ok := target.(*ErrNotImplemented)
	return ok
}
