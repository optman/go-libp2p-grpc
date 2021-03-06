package p2pgrpc

import (
	"context"
	"net"

	host "github.com/libp2p/go-libp2p-host"
	inet "github.com/libp2p/go-libp2p-net"
	protocol "github.com/libp2p/go-libp2p-protocol"
)

// Protocol is the GRPC-over-libp2p protocol.
const Protocol protocol.ID = "/grpc/0.0.1"

// Network is the "net.Addr.Network()" name returned by stream connection
// In turn, the "net.Addr.String()" will be a peer ID.
var Network = "libp2p"

// GRPCProtocol is the GRPC-transported protocol handler.
type GRPCProtocol struct {
	ctx      context.Context
	host     host.Host
	streamCh chan inet.Stream
}

// NewGRPCProtocol attaches the GRPC protocol to a host.
func NewGRPCProtocol(ctx context.Context, host host.Host) *GRPCProtocol {
	grpcProtocol := &GRPCProtocol{
		ctx:      ctx,
		host:     host,
		streamCh: make(chan inet.Stream),
	}
	host.SetStreamHandler(Protocol, grpcProtocol.HandleStream)
	return grpcProtocol
}

func (p *GRPCProtocol) NewListener() net.Listener {
	return newGrpcListener(p)
}

// HandleStream handles an incoming stream.
func (p *GRPCProtocol) HandleStream(stream inet.Stream) {
	select {
	case <-p.ctx.Done():
		return
	case p.streamCh <- stream:
	}
}
