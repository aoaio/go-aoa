//+build !go1.5

package debug

import "errors"

func (*HandlerT) StartGoTrace(string) error {
	return errors.New("tracing is not supported on Go < 1.5")
}

func (*HandlerT) StopGoTrace() error {
	return errors.New("tracing is not supported on Go < 1.5")
}
