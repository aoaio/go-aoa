package p2p

import (
	"net"

	"github.com/Aurorachain/go-Aurora/metrics"
)

var (
	ingressConnectMeter = metrics.NewMeter("p2p/InboundConnects")
	ingressTrafficMeter = metrics.NewMeter("p2p/InboundTraffic")
	egressConnectMeter  = metrics.NewMeter("p2p/OutboundConnects")
	egressTrafficMeter  = metrics.NewMeter("p2p/OutboundTraffic")
)

type meteredConn struct {
	*net.TCPConn
}

func newMeteredConn(conn net.Conn, ingress bool) net.Conn {

	if !metrics.Enabled {
		return conn
	}

	if ingress {
		ingressConnectMeter.Mark(1)
	} else {
		egressConnectMeter.Mark(1)
	}
	return &meteredConn{conn.(*net.TCPConn)}
}

func (c *meteredConn) Read(b []byte) (n int, err error) {
	n, err = c.TCPConn.Read(b)
	ingressTrafficMeter.Mark(int64(n))
	return
}

func (c *meteredConn) Write(b []byte) (n int, err error) {
	n, err = c.TCPConn.Write(b)
	egressTrafficMeter.Mark(int64(n))
	return
}
