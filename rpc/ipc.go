package rpc

import (
	"context"
	"fmt"
	"net"

	"github.com/Aurorachain/go-Aurora/log"
)

func CreateIPCListener(endpoint string) (net.Listener, error) {
	return ipcListen(endpoint)
}

func (srv *Server) ServeListener(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		log.Trace(fmt.Sprint("accepted conn", conn.RemoteAddr()))
		go srv.ServeCodec(NewJSONCodec(conn), OptionMethodInvocation|OptionSubscriptions)
	}
}

func DialIPC(ctx context.Context, endpoint string) (*Client, error) {
	return newClient(ctx, func(ctx context.Context) (net.Conn, error) {
		return newIPCConnection(ctx, endpoint)
	})
}
