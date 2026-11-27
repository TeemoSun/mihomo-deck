package mihomo

import "fmt"

type UpstreamError struct {
	StatusCode int
	Body       []byte
	Err        error
}

func (e *UpstreamError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("mihomo: %v", e.Err)
	}
	return fmt.Sprintf("mihomo: HTTP %d: %s", e.StatusCode, e.Body)
}
func (e *UpstreamError) Unwrap() error { return e.Err }
