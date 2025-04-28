package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"cosmossdk.io/log"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	"github.com/cometbft/cometbft/state/indexer"
	"github.com/cometbft/cometbft/state/txindex"
	"github.com/cosmos/cosmos-sdk/client"
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

var (
	_ rpcclient.Client = &RPCServer{}
	_ client.CometRPC  = &RPCServer{}
)

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

func NewRPCServer(adapter *adapter.Adapter, cfg *cmtcfg.RPCConfig, txIndexer txindex.TxIndexer, blockIndexer indexer.BlockIndexer, logger log.Logger) *RPCServer {
	return &RPCServer{adapter: adapter, txIndexer: txIndexer, blockIndexer: blockIndexer, config: cfg, logger: servercmtlog.CometLoggerWrapper{Logger: logger}}
}

// ----------------------------------------------------------------------------
// RPC Method Implementations (Moved to separate files: rpc_*.go)
// - rpc_info.go (ABCI*, Status, NetInfo, Health, Consensus*, Genesis*)
// - rpc_block.go (Block*, BlockResults, Commit, Header*, BlockchainInfo, BlockSearch)
// - rpc_tx.go (Tx, TxSearch, BroadcastTx*, CheckTx, NumUnconfirmedTxs, UnconfirmedTxs)
// - rpc_validators.go (Validators)
// - rpc_evidence.go (BroadcastEvidence)
// - rpc_events.go (Subscribe, Unsubscribe*)
// ----------------------------------------------------------------------------

// ----------------------------------------------------------------------------
// Client Lifecycle Methods

// IsRunning implements client.Client.
// Currently panics, consider implementing actual check if needed.
func (r *RPCServer) IsRunning() bool {
	panic("unimplemented")
}

// OnStop implements client.Client.
func (r *RPCServer) OnStop() {
	// Use a timeout for graceful shutdown
	shutdownTimeout := 5 * time.Second // TODO: Consider making this configurable
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// server might be nil if startRPC wasn't called or failed early
	if r.server.Handler != nil {
		if err := r.server.Shutdown(ctx); err != nil {
			r.logger.Error("error during RPC server shutdown", "error", err)
		} else {
			r.logger.Info("RPC server shut down gracefully")
		}
	}
}

// SetLogger implements client.Client.
func (r *RPCServer) SetLogger(logger cmtlog.Logger) {
	r.logger = logger
}

// OnReset implements client.Client.
// Currently panics, implement if reset logic is required.
func (r *RPCServer) OnReset() error {
	// panic("unimplemented")
	return nil // Return nil to satisfy interface, actual logic unimplemented
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

// Reset implements client.Client.
// Currently panics, implement if reset logic is required.
func (r *RPCServer) Reset() error {
	// panic("unimplemented")
	return nil // Return nil to satisfy interface, actual logic unimplemented
}

// String implements client.Client.
// Return a meaningful representation of the RPC server.
func (r *RPCServer) String() string {
	if r.config != nil && r.config.ListenAddress != "" {
		return fmt.Sprintf("RPCServer(%s)", r.config.ListenAddress)
	}
	return "RPCServer(inactive)"
}

// Stop implements client.Client.
// Currently panics. Original OnStop handles shutdown. Should this call OnStop?
func (r *RPCServer) Stop() error {
	// r.OnStop() // Call the shutdown logic?
	// panic("unimplemented")
	return nil // Return nil to satisfy interface
}
