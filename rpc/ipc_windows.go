package rpc

import (
	"context"
	"net"
	"time"

	"gopkg.in/natefinch/npipe.v2"
)

const defaultPipeDialTimeout = 2 * time.Second

func ipcListen(endpoint string) (net.Listener, error) {
	return npipe.Listen(endpoint)
}

func newIPCConnection(ctx context.Context, endpoint string) (net.Conn, error) {
	timeout := defaultPipeDialTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = deadline.Sub(time.Now())
		if timeout < 0 {
			timeout = 0
		}
	}
	return npipe.DialTimeout(endpoint, timeout)
}
