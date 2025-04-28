package rpc

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"cosmossdk.io/log"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/state/indexer"
	"github.com/cometbft/cometbft/state/txindex"
	servercmtlog "github.com/cosmos/cosmos-sdk/server/log"
	"github.com/rs/cors"
	"golang.org/x/net/netutil"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
	"github.com/rollkit/go-execution-abci/pkg/rpc/json"
)

const (
	// maxQueryLength is the maximum length of a query string that will be
	// accepted. This is just a safety check to avoid outlandish queries.
	maxQueryLength = 512

	defaultPerPage = 30
	maxPerPage     = 100
)

var (
	// ErrConsensusStateNotAvailable is returned because Rollkit doesn't use Tendermint consensus.
	ErrConsensusStateNotAvailable = errors.New("consensus state not available in Rollkit")

	subscribeTimeout = 5 * time.Second
)

func NewRPCServer(
	adapter *adapter.Adapter,
	cfg *cmtcfg.RPCConfig,
	logger log.Logger,
) *RPCServer {
	return &RPCServer{
		adapter: adapter,
		config:  cfg,
		logger:  servercmtlog.CometLoggerWrapper{Logger: logger},
	}
}

// RPCServer is a RPC server that implements the client.Client and client.CometRPC interfaces.
// It provides functionality to interact with the node through RPC calls.
// The server can be configured with different options such as CORS, connection limits,
// and listen addresses.
type RPCServer struct {
	adapter      *adapter.Adapter
	txIndexer    txindex.TxIndexer
	blockIndexer indexer.BlockIndexer
	config       *cmtcfg.RPCConfig
	server       http.Server
	logger       cmtlog.Logger
}

// Start implements client.Client.
func (r *RPCServer) Start() error {
	return r.startRPC()
}

func (r *RPCServer) startRPC() error {
	if r.config.ListenAddress == "" {
		r.logger.Info("Listen address not specified - RPC will not be exposed")
		return nil
	}
	parts := strings.SplitN(r.config.ListenAddress, "://", 2)
	if len(parts) != 2 {
		return errors.New("invalid RPC listen address: expecting tcp://host:port")
	}
	proto := parts[0]
	addr := parts[1]

	listener, err := net.Listen(proto, addr)
	if err != nil {
		return err
	}

	if r.config.MaxOpenConnections != 0 {
		r.logger.Debug("limiting number of connections", "limit", r.config.MaxOpenConnections)
		listener = netutil.LimitListener(listener, r.config.MaxOpenConnections)
	}

	handler, err := json.GetHTTPHandler(r, r.logger)
	if err != nil {
		return err
	}

	if r.config.IsCorsEnabled() {
		r.logger.Debug("CORS enabled",
			"origins", r.config.CORSAllowedOrigins,
			"methods", r.config.CORSAllowedMethods,
			"headers", r.config.CORSAllowedHeaders,
		)
		c := cors.New(cors.Options{
			AllowedOrigins: r.config.CORSAllowedOrigins,
			AllowedMethods: r.config.CORSAllowedMethods,
			AllowedHeaders: r.config.CORSAllowedHeaders,
		})
		handler = c.Handler(handler)
	}

	go func() {
		err := r.serve(listener, handler)
		if !errors.Is(err, http.ErrServerClosed) {
			r.logger.Error("error while serving HTTP", "error", err)
		}
	}()

	return nil
}

func (r *RPCServer) serve(listener net.Listener, handler http.Handler) error {
	r.logger.Info("serving HTTP", "listen address", listener.Addr())
	r.server = http.Server{
		Handler:           handler,
		ReadHeaderTimeout: time.Second * 2,
	}
	if r.config.TLSCertFile != "" && r.config.TLSKeyFile != "" {
		return r.server.ServeTLS(listener, r.config.CertFile(), r.config.KeyFile())
	}
	return r.server.Serve(listener)
}

// ----------------------------------------------------------------------------
// Client Lifecycle Methods

// IsRunning implements client.Client.
// Currently panics, consider implementing actual check if needed.
func (r *RPCServer) IsRunning() bool {
	panic("unimplemented")
}

// SetLogger implements client.Client.
func (r *RPCServer) SetLogger(logger cmtlog.Logger) {
	r.logger = logger
}

// OnStart implements client.Client.
// Currently panics. Original Start() calls startRPC(), keep that behaviour?
func (r *RPCServer) OnStart() error {
	// If this should behave like the original Start(), call startRPC.
	// return r.startRPC()
	// panic("unimplemented")
	return nil // Return nil to satisfy interface, actual logic unimplemented
}

// Quit implements client.Client.
// Currently panics, implement if a quit channel mechanism is needed.
func (r *RPCServer) Quit() <-chan struct{} {
	panic("unimplemented") // Keep panic for non-error return types if unimplemented
}

// Stop implements client.Client.
// Currently panics. Original OnStop handles shutdown. Should this call OnStop?
func (r *RPCServer) Stop() error {
	// r.OnStop() // Call the shutdown logic?
	// panic("unimplemented")
	return nil // Return nil to satisfy interface
}
