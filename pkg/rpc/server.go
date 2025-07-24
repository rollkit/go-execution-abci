package rpc

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"cosmossdk.io/log"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	rpcserver "github.com/cometbft/cometbft/rpc/jsonrpc/server"
	servercmtlog "github.com/cosmos/cosmos-sdk/server/log"
	"github.com/rs/cors"
	"golang.org/x/net/netutil"

	"github.com/evstack/ev-abci/pkg/rpc/core"
)

// NewRPCServer creates a new RPC server.
func NewRPCServer(
	cfg *cmtcfg.RPCConfig,
	logger log.Logger,
) *RPCServer {
	cmtLogger := servercmtlog.CometLoggerWrapper{Logger: logger}
	return &RPCServer{
		config: cfg,
		logger: cmtLogger,
	}
}

// RPCServer manages the HTTP server for RPC requests.
// It delegates the actual RPC method implementations to an rpcProvider.
type RPCServer struct {
	config      *cmtcfg.RPCConfig
	httpHandler http.Handler
	server      http.Server
	logger      cmtlog.Logger
}

// Start starts the RPC server.
func (r *RPCServer) Start() error {
	return r.startRPC()
}

func (r *RPCServer) startRPC() error {
	if r.config.ListenAddress == "" {
		r.logger.Info("listen address not specified - RPC will not be exposed")
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

	mux := http.NewServeMux()
	rpcserver.RegisterRPCFuncs(mux, core.Routes, r.logger)
	r.httpHandler = mux

	if r.config.MaxOpenConnections != 0 {
		r.logger.Debug("limiting number of connections", "limit", r.config.MaxOpenConnections)
		listener = netutil.LimitListener(listener, r.config.MaxOpenConnections)
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
		r.httpHandler = c.Handler(r.httpHandler)
	}

	go func() {
		err := r.serve(listener, r.httpHandler)
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

// Stop stops the RPC server gracefully.
func (r *RPCServer) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := r.server.Shutdown(ctx); err != nil {
		r.logger.Error("RPC server shutdown error", "err", err)
		return err
	}

	r.logger.Info("RPC server stopped")
	return nil
}
