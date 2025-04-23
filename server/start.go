package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"cosmossdk.io/log"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmtp2p "github.com/cometbft/cometbft/p2p"
	pvm "github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servergrpc "github.com/cosmos/cosmos-sdk/server/grpc"
	servercmtlog "github.com/cosmos/cosmos-sdk/server/log"
	sdktypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/cosmos/cosmos-sdk/version"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/hashicorp/go-metrics"
	"github.com/rollkit/go-execution-abci/adapter"

	"github.com/cometbft/cometbft/mempool"
	"github.com/rollkit/go-execution-abci/rpc"
	"github.com/rollkit/rollkit/core/sequencer"
	"github.com/rollkit/rollkit/node"
	"github.com/rollkit/rollkit/pkg/config"
	"github.com/rollkit/rollkit/pkg/genesis"
	"github.com/rollkit/rollkit/pkg/p2p"
	"github.com/rollkit/rollkit/pkg/p2p/key"
	"github.com/rollkit/rollkit/pkg/signer"
	filesigner "github.com/rollkit/rollkit/pkg/signer/file"
	"github.com/rollkit/rollkit/pkg/store"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	goda "github.com/rollkit/go-da/proxy"
	rollkitda "github.com/rollkit/rollkit/da"
)

const (
	flagTraceStore = "trace-store"
	flagTransport  = "transport"
	flagGRPCOnly   = "grpc-only"
)

// StartCommandHandler is the type that must implement nova to match Cosmos SDK start logic.
type StartCommandHandler = func(svrCtx *server.Context, clientCtx client.Context, appCreator sdktypes.AppCreator, withCmt bool, opts server.StartCmdOptions) error

// StartHandler starts the Rollkit server with the provided application and options.
func StartHandler() StartCommandHandler {
	return func(svrCtx *server.Context, clientCtx client.Context, appCreator sdktypes.AppCreator, inProcess bool, opts server.StartCmdOptions) error {
		svrCfg, err := getAndValidateConfig(svrCtx)
		if err != nil {
			return err
		}

		app, appCleanupFn, err := startApp(svrCtx, appCreator, opts)
		if err != nil {
			return err
		}
		defer appCleanupFn()

		metrics, err := startTelemetry(svrCfg)
		if err != nil {
			return err
		}

		emitServerInfoMetrics()

		return startInProcess(svrCtx, svrCfg, clientCtx, app, metrics, opts)
	}
}

func startApp(svrCtx *server.Context, appCreator sdktypes.AppCreator, opts server.StartCmdOptions) (app sdktypes.Application, cleanupFn func(), err error) {
	traceWriter, traceCleanupFn, err := setupTraceWriter(svrCtx)
	if err != nil {
		return app, traceCleanupFn, err
	}

	home := svrCtx.Config.RootDir
	db, err := opts.DBOpener(home, server.GetAppDBBackend(svrCtx.Viper))
	if err != nil {
		return app, traceCleanupFn, err
	}

	app = appCreator(svrCtx.Logger, db, traceWriter, svrCtx.Viper)

	cleanupFn = func() {
		traceCleanupFn()
		if localErr := app.Close(); localErr != nil {
			svrCtx.Logger.Error(localErr.Error())
		}
	}
	return app, cleanupFn, nil
}

func startInProcess(svrCtx *server.Context, svrCfg serverconfig.Config, clientCtx client.Context, app sdktypes.Application,
	metrics *telemetry.Metrics, opts server.StartCmdOptions,
) error {
	cmtCfg := svrCtx.Config
	gRPCOnly := svrCtx.Viper.GetBool(flagGRPCOnly)
	g, ctx := getCtx(svrCtx, true)

	// Hold gRPC server instance and potential error
	var grpcSrv *grpc.Server
	var err error

	// 1. Start gRPC server FIRST (if enabled and not gRPC-only mode initially)
	if !gRPCOnly && svrCfg.GRPC.Enable {
		svrCtx.Logger.Info("Attempting to start gRPC server before Rollkit node...")
		grpcSrv, clientCtx, err = startGrpcServer(ctx, g, svrCfg.GRPC, clientCtx, svrCtx, app)
		if err != nil {
			svrCtx.Logger.Error("Failed to start gRPC server early", "error", err)
			// Decide whether to proceed or return the error
			return fmt.Errorf("failed to start gRPC server: %w", err) // Return error if gRPC is essential
		}
		svrCtx.Logger.Info("Successfully started gRPC server early.")
	}

	// 2. Start the Rollkit Node (if not in gRPC-only mode)
	var rpcServer *rpc.RPCServer
	var cleanupRollkitFn func()
	if !gRPCOnly {
		svrCtx.Logger.Info("Starting Rollkit node with ABCI CometBFT in-process...")
		// Pass the main context 'ctx' to startNode
		_, rpcServer, cleanupRollkitFn, err = startNode(ctx, svrCtx, cmtCfg, app)
		if err != nil {
			svrCtx.Logger.Error("Failed to start Rollkit node", "error", err)
			return err // Error starting Rollkit
		}
		defer cleanupRollkitFn()
		svrCtx.Logger.Info("Rollkit node components initialized.")

		// Register Tx/Tendermint services if API or gRPC are enabled
		// (API server starts later)
		if svrCfg.API.Enable || svrCfg.GRPC.Enable {
			svrCtx.Logger.Info("Registering Tx/Tendermint/Node services...")
			if rpcServer != nil {
				// Use Rollkit's RPC client if available
				clientCtx = clientCtx.WithClient(rpcServer)
				svrCtx.Logger.Info("Using Rollkit RPC client for service registration.")
				app.RegisterTxService(clientCtx)
				app.RegisterTendermintService(clientCtx)
				app.RegisterNodeService(clientCtx, svrCfg)
				svrCtx.Logger.Info("Services registered using Rollkit RPC client.")
			} else {
				// Handle case where rpcServer is nil but services are needed?
				// This might require a different RPC client if Rollkit's isn't available.
				svrCtx.Logger.Warn("Rollkit RPC server not available for registering Tx/Tendermint/Node services")
			}
		}
	} else {
		// gRPC Only Mode - Still might need to register services,
		// but won't have the Rollkit rpcServer.
		svrCtx.Logger.Info("Starting node in gRPC only mode; Rollkit node disabled.")
		svrCfg.GRPC.Enable = true // Ensure gRPC is enabled in this mode
		// We need to ensure that the registered services do not depend on
		// a CometBFT RPC client that doesn't exist in this mode.
		// app.RegisterTxService(clientCtx)       // Might fail without RPC client
		// app.RegisterTendermintService(clientCtx) // Might fail without RPC client
		// app.RegisterNodeService(clientCtx, svrCfg) // Might fail without RPC client
		svrCtx.Logger.Info("Skipping Rollkit node startup and service registration dependent on Rollkit RPC.")
	}

	// 3. Start gRPC server if we are in gRPC-only mode and it wasn't started before
	if gRPCOnly && grpcSrv == nil && svrCfg.GRPC.Enable {
		svrCtx.Logger.Info("Attempting to start gRPC server in gRPC-only mode...")
		grpcSrv, clientCtx, err = startGrpcServer(ctx, g, svrCfg.GRPC, clientCtx, svrCtx, app)
		if err != nil {
			svrCtx.Logger.Error("Failed to start gRPC server in gRPC-only mode", "error", err)
			return err
		}
		svrCtx.Logger.Info("Successfully started gRPC server in gRPC-only mode.")
	}

	// 4. Start API Server (if enabled)
	if svrCfg.API.Enable {
		svrCtx.Logger.Info("Attempting to start API server...")
		err = startAPIServer(ctx, g, svrCfg, clientCtx, svrCtx, app, cmtCfg.RootDir, grpcSrv, metrics)
		if err != nil {
			svrCtx.Logger.Error("Failed to start API server", "error", err)
			return err
		}
		svrCtx.Logger.Info("Successfully launched API server start goroutine.")
	} else {
		svrCtx.Logger.Info("API server disabled, skipping.")
	}

	// 5. Run PostSetup hook if provided
	if opts.PostSetup != nil {
		svrCtx.Logger.Info("Running PostSetup hook...")
		if err := opts.PostSetup(svrCtx, clientCtx, ctx, g); err != nil {
			svrCtx.Logger.Error("PostSetup hook failed", "error", err)
			return err
		}
		svrCtx.Logger.Info("PostSetup hook completed.")
	}

	// wait for signal capture and gracefully return
	// we are guaranteed to be waiting for the "ListenForQuitSignals" goroutine.
	svrCtx.Logger.Info("Waiting for termination signals...")
	err = g.Wait()
	svrCtx.Logger.Info("Termination signal received or error occurred.", "error", err)
	return err
}

func startGrpcServer(
	ctx context.Context,
	g *errgroup.Group,
	config serverconfig.GRPCConfig,
	clientCtx client.Context,
	svrCtx *server.Context,
	app sdktypes.Application,
) (*grpc.Server, client.Context, error) {
	svrCtx.Logger.Info("Attempting to start gRPC server...")

	if !config.Enable {
		svrCtx.Logger.Info("gRPC server disabled in config, skipping.")
		// return grpcServer as nil if gRPC is disabled
		return nil, clientCtx, nil
	}

	svrCtx.Logger.Info("gRPC server is enabled.", "address", config.Address)

	_, _, err := net.SplitHostPort(config.Address)
	if err != nil {
		svrCtx.Logger.Error("Invalid gRPC address", "address", config.Address, "error", err)
		return nil, clientCtx, err
	}

	maxSendMsgSize := config.MaxSendMsgSize
	if maxSendMsgSize == 0 {
		maxSendMsgSize = serverconfig.DefaultGRPCMaxSendMsgSize
	}

	maxRecvMsgSize := config.MaxRecvMsgSize
	if maxRecvMsgSize == 0 {
		maxRecvMsgSize = serverconfig.DefaultGRPCMaxRecvMsgSize
	}

	svrCtx.Logger.Info("Creating gRPC client for gateway...")
	// if gRPC is enabled, configure gRPC client for gRPC gateway
	grpcClient, err := grpc.NewClient(
		config.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(codec.NewProtoCodec(clientCtx.InterfaceRegistry).GRPCCodec()),
			grpc.MaxCallRecvMsgSize(maxRecvMsgSize),
			grpc.MaxCallSendMsgSize(maxSendMsgSize),
		),
	)
	if err != nil {
		svrCtx.Logger.Error("Failed to create gRPC client", "error", err)
		return nil, clientCtx, err
	}
	svrCtx.Logger.Info("Successfully created gRPC client.")

	clientCtx = clientCtx.WithGRPCClient(grpcClient)
	svrCtx.Logger.Debug("gRPC client assigned to client context", "target", config.Address)

	svrCtx.Logger.Info("Creating gRPC server instance...")
	grpcSrv, err := servergrpc.NewGRPCServer(clientCtx, app, config)
	if err != nil {
		svrCtx.Logger.Error("Failed to create gRPC server instance", "error", err)
		return nil, clientCtx, err
	}
	svrCtx.Logger.Info("Successfully created gRPC server instance.")

	// Start the gRPC server in a goroutine. Note, the provided ctx will ensure
	// that the server is gracefully shut down.
	svrCtx.Logger.Info("Launching gRPC server start goroutine...")
	g.Go(func() error {
		svrCtx.Logger.Info("Goroutine started: Calling StartGRPCServer", "address", config.Address)
		err := servergrpc.StartGRPCServer(ctx, svrCtx.Logger.With("module", "grpc-server"), config, grpcSrv)
		if err != nil {
			svrCtx.Logger.Error("StartGRPCServer returned error", "error", err)
		} else {
			svrCtx.Logger.Info("StartGRPCServer finished (graceful shutdown?).")
		}
		return err
	})
	svrCtx.Logger.Info("Successfully launched gRPC server start goroutine.")

	return grpcSrv, clientCtx, nil
}

func startAPIServer(
	ctx context.Context,
	g *errgroup.Group,
	svrCfg serverconfig.Config,
	clientCtx client.Context,
	svrCtx *server.Context,
	app sdktypes.Application,
	home string,
	grpcSrv *grpc.Server,
	metrics *telemetry.Metrics,
) error {
	if !svrCfg.API.Enable {
		return nil
	}

	clientCtx = clientCtx.WithHomeDir(home)

	apiSrv := api.New(clientCtx, svrCtx.Logger.With("module", "api-server"), grpcSrv)
	app.RegisterAPIRoutes(apiSrv, svrCfg.API)

	if svrCfg.Telemetry.Enabled {
		apiSrv.SetTelemetry(metrics)
	}

	g.Go(func() error {
		return apiSrv.Start(ctx, svrCfg)
	})
	return nil
}

func startTelemetry(cfg serverconfig.Config) (*telemetry.Metrics, error) {
	if !cfg.Telemetry.Enabled {
		return nil, nil
	}

	return telemetry.New(cfg.Telemetry)
}

func startNode(
	ctx context.Context,
	srvCtx *server.Context,
	cfg *cmtcfg.Config,
	app sdktypes.Application,
) (rolllkitNode node.Node, rpcServer *rpc.RPCServer, cleanupFn func(), err error) {
	logger := srvCtx.Logger.With("module", "rollkit")
	logger.Info("starting node with Rollkit in-process")

	pval := pvm.LoadOrGenFilePV(cfg.PrivValidatorKeyFile(), cfg.PrivValidatorStateFile())
	signingKey, err := GetNodeKey(&cmtp2p.NodeKey{PrivKey: pval.Key.PrivKey})
	if err != nil {
		return nil, nil, cleanupFn, err
	}

	nodeKey := &key.NodeKey{PrivKey: signingKey, PubKey: signingKey.GetPublic()}

	rollkitcfg, err := config.LoadFromViper(srvCtx.Viper)
	if err != nil {
		return nil, nil, cleanupFn, err
	}

	// only load signer if rollkit.node.aggregator == true
	var signer signer.Signer
	if rollkitcfg.Node.Aggregator {
		configDir := filepath.Dir(rollkitcfg.ConfigPath())
		signer, err = filesigner.LoadFileSystemSigner(configDir, []byte{})
		if err != nil {
			if os.IsNotExist(err) || strings.Contains(err.Error(), "key file not found") {
				// If the file doesn't exist, create it
				logger.Info("Creating new signer key file")
				signer, err = filesigner.CreateFileSystemSigner(configDir, []byte{})
				if err != nil {
					return nil, nil, cleanupFn, err
				}
			} else {
				return nil, nil, cleanupFn, err
			}
		}
	}

	// err = config.TranslateAddresses(&rollkitcfg)
	// if err != nil {
	// 	return nil, nil, cleanupFn, fmt.Errorf("failed to translate addresses: %w", err)
	// }

	genDoc, err := getGenDocProvider(cfg)()
	if err != nil {
		return nil, nil, cleanupFn, err
	}

	cmtGenDoc, err := genDoc.ToGenesisDoc()
	if err != nil {
		return nil, nil, cleanupFn, err
	}

	// Get AppGenesis before creating the executor
	appGenesis, err := genutiltypes.AppGenesisFromFile(cfg.GenesisFile())
	if err != nil {
		return nil, nil, cleanupFn, err
	}

	database, err := store.NewDefaultKVStore(cfg.RootDir, "data", "rollkit")
	if err != nil {
		return nil, nil, cleanupFn, err
	}

	metrics := node.DefaultMetricsProvider(config.DefaultInstrumentationConfig())

	_, p2pMetrics := metrics(cmtGenDoc.ChainID)
	p2pClient, err := p2p.NewClient(rollkitcfg, nodeKey, database, logger.With("module", "p2p"), p2pMetrics)
	if err != nil {
		return nil, nil, cleanupFn, err
	}

	st := store.New(database)

	executor := adapter.NewABCIExecutor(
		app,
		st,
		p2pClient,
		logger,
		cfg,
		appGenesis,
	)

	// TODO: pass the metrics if needed
	cmtApp := server.NewCometABCIWrapper(app)
	clientCreator := proxy.NewLocalClientCreator(cmtApp)

	proxyApp, err := initProxyApp(clientCreator, logger, proxy.NopMetrics())
	if err != nil {
		panic(err)
	}

	height, err := st.Height(context.Background())
	if err != nil {
		return nil, nil, cleanupFn, err
	}
	mempool := mempool.NewCListMempool(cfg.Mempool, proxyApp.Mempool(), int64(height))
	executor.SetMempool(mempool)

	ctxWithCancel, cancelFn := context.WithCancel(ctx)
	cleanupFn = func() {
		cancelFn()
	}

	rollkitGenesis := genesis.NewGenesis(
		cmtGenDoc.ChainID,
		uint64(cmtGenDoc.InitialHeight),
		cmtGenDoc.GenesisTime,
		cmtGenDoc.Validators[0].Address, // use the first validator as sequencer
	)

	dalc, err := goda.NewClient(rollkitcfg.DA.Address, rollkitcfg.DA.AuthToken)
	if err != nil {
		return nil, nil, cleanupFn, err
	}

	adapter := &daAdapter{dalc}

	// TODO(@facu): gas price and gas multiplier should be set by the node operator
	rollkitda := rollkitda.NewDAClient(adapter, 1, 1, []byte(cmtGenDoc.ChainID), []byte{}, logger)

	rolllkitNode, err = node.NewNode(
		ctxWithCancel,
		rollkitcfg,
		executor,
		sequencer.NewDummySequencer(),
		rollkitda,
		signer,
		*nodeKey,
		p2pClient,
		rollkitGenesis,
		database,
		metrics,
		logger,
	)
	if err != nil {
		return nil, nil, cleanupFn, err
	}

	rpcServer = rpc.NewRPCServer(executor, cfg.RPC, nil, nil, logger)
	err = rpcServer.Start()
	if err != nil {
		return nil, nil, cleanupFn, fmt.Errorf("failed to start abci rpc server: %w", err)
	}

	logger.Info("starting node")
	err = rolllkitNode.Run(ctx)
	if err != nil {
		if err == context.Canceled {
			return nil, nil, cleanupFn, nil
		}
		return nil, nil, cleanupFn, fmt.Errorf("failed to start node: %w", err)
	}

	// executor must be started after the node is started
	logger.Info("starting executor")
	err = executor.Start(ctx)
	if err != nil {
		return nil, nil, cleanupFn, fmt.Errorf("failed to start executor: %w", err)
	}

	return rolllkitNode, rpcServer, cleanupFn, nil
}

// getGenDocProvider returns a function which returns the genesis doc from the genesis file.
func getGenDocProvider(cfg *cmtcfg.Config) func() (*genutiltypes.AppGenesis, error) {
	return func() (*genutiltypes.AppGenesis, error) {
		appGenesis, err := genutiltypes.AppGenesisFromFile(cfg.GenesisFile())
		if err != nil {
			return nil, err
		}

		return appGenesis, nil
	}
}

func getAndValidateConfig(svrCtx *server.Context) (serverconfig.Config, error) {
	config, err := serverconfig.GetConfig(svrCtx.Viper)
	if err != nil {
		return config, err
	}

	if err := config.ValidateBasic(); err != nil {
		return config, err
	}
	return config, nil
}

func getCtx(svrCtx *server.Context, block bool) (*errgroup.Group, context.Context) {
	ctx, cancelFn := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)
	// listen for quit signals so the calling parent process can gracefully exit
	server.ListenForQuitSignals(g, block, cancelFn, svrCtx.Logger)
	return g, ctx
}

func openTraceWriter(traceWriterFile string) (w io.WriteCloser, err error) {
	if traceWriterFile == "" {
		return
	}
	return os.OpenFile( //nolint:gosec
		traceWriterFile,
		os.O_WRONLY|os.O_APPEND|os.O_CREATE,
		0o666,
	)
}

func setupTraceWriter(svrCtx *server.Context) (traceWriter io.WriteCloser, cleanup func(), err error) {
	// clean up the traceWriter when the server is shutting down
	cleanup = func() {}

	traceWriterFile := svrCtx.Viper.GetString(flagTraceStore)
	traceWriter, err = openTraceWriter(traceWriterFile)
	if err != nil {
		return traceWriter, cleanup, err
	}

	// if flagTraceStore is not used then traceWriter is nil
	if traceWriter != nil {
		cleanup = func() {
			if err = traceWriter.Close(); err != nil {
				svrCtx.Logger.Error("failed to close trace writer", "err", err)
			}
		}
	}

	return traceWriter, cleanup, nil
}

// emitServerInfoMetrics emits server info related metrics using application telemetry.
func emitServerInfoMetrics() {
	var ls []metrics.Label

	versionInfo := version.NewInfo()
	if len(versionInfo.GoVersion) > 0 {
		ls = append(ls, telemetry.NewLabel("go", versionInfo.GoVersion))
	}
	if len(versionInfo.CosmosSdkVersion) > 0 {
		ls = append(ls, telemetry.NewLabel("version", versionInfo.CosmosSdkVersion))
	}

	if len(ls) == 0 {
		return
	}

	telemetry.SetGaugeWithLabels([]string{"server", "info"}, 1, ls)
}

func initProxyApp(clientCreator proxy.ClientCreator, logger log.Logger, metrics *proxy.Metrics) (proxy.AppConns, error) {
	proxyApp := proxy.NewAppConns(clientCreator, metrics)
	proxyApp.SetLogger(servercmtlog.CometLoggerWrapper{Logger: logger}.With("module", "proxy"))
	if err := proxyApp.Start(); err != nil {
		return nil, fmt.Errorf("error while starting proxy app connections: %w", err)
	}
	return proxyApp, nil
}
