package p2pgrpc

import (
	inet "github.com/libp2p/go-libp2p-net"
	"net"
)

// streamConn represents a net.Conn wrapped to be compatible with net.conn
type streamConn struct {
	inet.Stream
}

// LocalAddr returns the local address.
func (c *streamConn) LocalAddr() net.Addr {
	return &addr{c.Stream.Conn().LocalPeer()}
}

// RemoteAddr returns the remote address.
func (c *streamConn) RemoteAddr() net.Addr {
	return &addr{c.Stream.Conn().RemotePeer()}
}

var _ net.Conn = &streamConn{}
