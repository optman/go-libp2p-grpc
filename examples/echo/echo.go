package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"os"

	golog "github.com/ipfs/go-log"
	csms "github.com/libp2p/go-conn-security-multistream"
	"github.com/libp2p/go-libp2p-core/sec/insecure"
	crypto "github.com/libp2p/go-libp2p-crypto"
	host "github.com/libp2p/go-libp2p-host"
	peer "github.com/libp2p/go-libp2p-peer"
	pstore "github.com/libp2p/go-libp2p-peerstore"
	pstoremem "github.com/libp2p/go-libp2p-peerstore/pstoremem"
	swarm "github.com/libp2p/go-libp2p-swarm"
	tptu "github.com/libp2p/go-libp2p-transport-upgrader"
	yamux "github.com/libp2p/go-libp2p-yamux"
	bhost "github.com/libp2p/go-libp2p/p2p/host/basic"
	msmux "github.com/libp2p/go-stream-muxer-multistream"
	tcp "github.com/libp2p/go-tcp-transport"
	ma "github.com/multiformats/go-multiaddr"
	gologging "github.com/whyrusleeping/go-logging"

	"github.com/golang/protobuf/jsonpb"
	"github.com/paralin/go-libp2p-grpc"
	"github.com/paralin/go-libp2p-grpc/examples/echo/echosvc"
	"google.golang.org/grpc"
)

// Echoer implements the EchoService.
type Echoer struct {
	PeerID peer.ID
}

// Echo asks a node to respond with a message.
func (e *Echoer) Echo(ctx context.Context, req *echosvc.EchoRequest) (*echosvc.EchoReply, error) {
	return &echosvc.EchoReply{
		Message: req.GetMessage(),
		PeerId:  e.PeerID.Pretty(),
	}, nil
}

// makeBasicHost creates a LibP2P host with a random peer ID listening on the
// given multiaddress.
func makeBasicHost(listenPort int, randseed int64) (host.Host, error) {
	// If the seed is zero, use real cryptographic randomness. Otherwise, use a
	// deterministic randomness source to make generated keys stay the same
	// across multiple runs
	var r io.Reader
	if randseed == 0 {
		r = rand.Reader
	} else {
		r = mrand.New(mrand.NewSource(randseed))
	}

	// Generate a key pair for this host. We will use it at least
	// to obtain a valid host ID.
	priv, pub, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	if err != nil {
		return nil, err
	}

	// Obtain Peer ID from public key
	pid, err := peer.IDFromPublicKey(pub)
	if err != nil {
		return nil, err
	}

	// Create a multiaddress
	addr, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", listenPort))
	if err != nil {
		return nil, err
	}

	// Create a peerstore
	ps := pstoremem.NewPeerstore()
	ps.AddPrivKey(pid, priv)
	ps.AddPubKey(pid, pub)

	// Create swarm (implements libP2P Network)
	swrm := swarm.NewSwarm(context.Background(), pid, ps, nil)

	// Set up stream multiplexer
	muxer := msmux.NewBlankTransport()
	muxer.AddTransport("/yamux/1.0.0", yamux.DefaultTransport)

	secMuxer := new(csms.SSMuxer)
	secMuxer.AddTransport(insecure.ID, insecure.NewWithIdentity(pid, priv))

	upgrader := &tptu.Upgrader{
		Secure: secMuxer,
		Muxer:  muxer,
	}

	tpt := tcp.NewTCPTransport(upgrader)
	if err := swrm.AddTransport(tpt); err != nil {
		return nil, err
	}

	basicHost := bhost.New(swrm)
	if err := basicHost.Network().Listen([]ma.Multiaddr{addr}...); err != nil {
		return nil, err
	}

	// Build host multiaddress
	hostAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ipfs/%s", basicHost.ID().Pretty()))

	// Now we can build a full multiaddress to reach this host
	// by encapsulating both addresses:
	fullAddr := addr.Encapsulate(hostAddr)
	log.Printf("I am %s\n", fullAddr)
	log.Printf("Now run \"./echo -l %d -d %s\" on a different terminal\n", listenPort+1, fullAddr)

	return basicHost, nil
}

func main() {
	// LibP2P code uses golog to log messages. They log with different
	// string IDs (i.e. "swarm"). We can control the verbosity level for
	// all loggers with:
	golog.SetAllLoggers(gologging.INFO) // Change to DEBUG for extra info

	// Parse options from the command line
	listenF := flag.Int("l", 0, "wait for incoming connections")
	target := flag.String("d", "", "target peer to dial")
	echoMsg := flag.String("m", "Hello, world", "message to echo")
	seed := flag.Int64("seed", 0, "set random seed for id generation")
	flag.Parse()

	if *listenF == 0 {
		log.Fatal("Please provide a port to bind on with -l")
	}

	// Make a host that listens on the given multiaddress
	ha, err := makeBasicHost(*listenF, *seed)
	if err != nil {
		log.Fatal(err)
	}

	grpcProto := p2pgrpc.NewGRPCProtocol(context.Background(), ha)

	//server
	if *target == "" {
		grpcServer := grpc.NewServer()

		// Register our echoer GRPC service.
		echosvc.RegisterEchoServiceServer(grpcServer, &Echoer{PeerID: ha.ID()})

		log.Println("listening for connections")

		listener := grpcProto.NewListener()

		log.Fatal(grpcServer.Serve(listener))
	}
	/**** This is where the listener code ends ****/

	// The following code extracts target's the peer ID from the
	// given multiaddress
	ipfsaddr, err := ma.NewMultiaddr(*target)
	if err != nil {
		log.Fatalln(err)
	}

	pid, err := ipfsaddr.ValueForProtocol(ma.P_IPFS)
	if err != nil {
		log.Fatalln(err)
	}

	peerid, err := peer.IDB58Decode(pid)
	if err != nil {
		log.Fatalln(err)
	}

	// Decapsulate the /ipfs/<peerID> part from the target
	// /ip4/<a.b.c.d>/ipfs/<peer> becomes /ip4/<a.b.c.d>
	targetPeerAddr, _ := ma.NewMultiaddr(
		fmt.Sprintf("/ipfs/%s", peer.IDB58Encode(peerid)))
	targetAddr := ipfsaddr.Decapsulate(targetPeerAddr)

	// We have a peer ID and a targetAddr so we add it to the peerstore
	// so LibP2P knows how to contact it
	ha.Peerstore().AddAddr(peerid, targetAddr, pstore.PermanentAddrTTL)

	// make a new stream from host B to host A
	log.Println("dialing via grpc")
	grpcConn, err := grpcProto.Dial(context.Background(), peerid, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalln(err)
	}

	// create our service client
	echoClient := echosvc.NewEchoServiceClient(grpcConn)
	echoReply, err := echoClient.Echo(context.Background(), &echosvc.EchoRequest{Message: *echoMsg})
	if err != nil {
		log.Fatalln(err)
	}

	log.Println("read reply:")
	err = (&jsonpb.Marshaler{EmitDefaults: true, Indent: "\t"}).
		Marshal(os.Stdout, echoReply)
	if err != nil {
		log.Fatalln(err)
	}
	log.Println()
}
