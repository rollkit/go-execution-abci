package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"

	"cosmossdk.io/log"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/mempool"
	cmtp2p "github.com/cometbft/cometbft/p2p"
	pvm "github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cometbft/cometbft/state/indexer"
	"github.com/cometbft/cometbft/state/txindex"
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
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/rollkit/rollkit/core/sequencer"
	rollkitda "github.com/rollkit/rollkit/da"
	"github.com/rollkit/rollkit/da/proxy/jsonrpc"
	"github.com/rollkit/rollkit/node"
	"github.com/rollkit/rollkit/pkg/config"
	"github.com/rollkit/rollkit/pkg/genesis"
	"github.com/rollkit/rollkit/pkg/p2p"
	"github.com/rollkit/rollkit/pkg/p2p/key"
	"github.com/rollkit/rollkit/pkg/signer"
	"github.com/rollkit/rollkit/pkg/store"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
	"github.com/rollkit/go-execution-abci/pkg/rpc"
	rpcjson "github.com/rollkit/go-execution-abci/pkg/rpc/json"
	provider "github.com/rollkit/go-execution-abci/pkg/rpc/provider"
	execsigner "github.com/rollkit/go-execution-abci/pkg/signer"
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

	if gRPCOnly {
		// TODO: Generalize logic so that gRPC only is really in startStandAlone
		svrCtx.Logger.Info("starting node in gRPC only mode; CometBFT is disabled")
		svrCfg.GRPC.Enable = true
	} else {
		svrCtx.Logger.Info("starting node with ABCI CometBFT in-process")
		_, rpcServer, cleanupFn, err := startNode(ctx, svrCtx, cmtCfg, app)
		if err != nil {
			return err
		}
		defer cleanupFn()

		// Add the tx service to the gRPC router. We only need to register this
		// service if API or gRPC is enabled, and avoid doing so in the general
		// case, because it spawns a new local CometBFT RPC client.
		if svrCfg.API.Enable || svrCfg.GRPC.Enable {
			// Re-assign for making the client available below do not use := to avoid
			// shadowing the clientCtx variable.
			clientCtx = clientCtx.WithClient(rpcServer)

			app.RegisterTxService(clientCtx)
			app.RegisterTendermintService(clientCtx)
			app.RegisterNodeService(clientCtx, svrCfg)
		}
	}

	grpcSrv, clientCtx, err := startGrpcServer(ctx, g, svrCfg.GRPC, clientCtx, svrCtx, app)
	if err != nil {
		return err
	}

	err = startAPIServer(ctx, g, svrCfg, clientCtx, svrCtx, app, cmtCfg.RootDir, grpcSrv, metrics)
	if err != nil {
		return err
	}

	if opts.PostSetup != nil {
		if err := opts.PostSetup(svrCtx, clientCtx, ctx, g); err != nil {
			return err
		}
	}

	// wait for signal capture and gracefully return
	// we are guaranteed to be waiting for the "ListenForQuitSignals" goroutine.
	return g.Wait()
}

func startGrpcServer(
	ctx context.Context,
	g *errgroup.Group,
	config serverconfig.GRPCConfig,
	clientCtx client.Context,
	svrCtx *server.Context,
	app sdktypes.Application,
) (*grpc.Server, client.Context, error) {
	if !config.Enable {
		// return grpcServer as nil if gRPC is disabled
		return nil, clientCtx, nil
	}
	_, _, err := net.SplitHostPort(config.Address)
	if err != nil {
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
		return nil, clientCtx, err
	}

	clientCtx = clientCtx.WithGRPCClient(grpcClient)
	svrCtx.Logger.Debug("gRPC client assigned to client context", "target", config.Address)

	grpcSrv, err := servergrpc.NewGRPCServer(clientCtx, app, config)
	if err != nil {
		return nil, clientCtx, err
	}

	// Start the gRPC server in a goroutine. Note, the provided ctx will ensure
	// that the server is gracefully shut down.
	g.Go(func() error {
		return servergrpc.StartGRPCServer(ctx, svrCtx.Logger.With("module", "grpc-server"), config, grpcSrv)
	})
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
) (rolllkitNode node.Node, rpcClient rpc.RpcProvider, cleanupFn func(), err error) {
	logger := srvCtx.Logger.With("module", "rollkit")
	logger.Info("starting node with Rollkit in-process")

	pval := pvm.LoadOrGenFilePV(
		cfg.PrivValidatorKeyFile(),
		cfg.PrivValidatorStateFile(),
	)

	signingKey, err := execsigner.GetNodeKey(&cmtp2p.NodeKey{PrivKey: pval.Key.PrivKey})
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
		signer, err = execsigner.NewSignerWrapper(pval.Key.PrivKey)
		if err != nil {
			return nil, nil, cleanupFn, err
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

	metrics := node.DefaultMetricsProvider(rollkitcfg.Instrumentation)

	_, p2pMetrics := metrics(cmtGenDoc.ChainID)
	p2pClient, err := p2p.NewClient(rollkitcfg, nodeKey, database, logger.With("module", "p2p"), p2pMetrics)
	if err != nil {
		return nil, nil, cleanupFn, err
	}

	adapterMetrics := adapter.NopMetrics()
	if rollkitcfg.Instrumentation.IsPrometheusEnabled() {
		adapterMetrics = adapter.PrometheusMetrics(config.DefaultInstrumentationConfig().Namespace, "chain_id", cmtGenDoc.ChainID)
	}

	st := store.New(database)

	executor := adapter.NewABCIExecutor(
		app,
		st,
		p2pClient,
		p2pMetrics,
		logger,
		cfg,
		appGenesis,
		adapterMetrics,
	)

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
		cmtGenDoc.Validators[0].Address.Bytes(), // use the first validator as sequencer
	)

	// create the DA client
	daClient, err := jsonrpc.NewClient(ctx, logger, rollkitcfg.DA.Address, rollkitcfg.DA.AuthToken)
	if err != nil {
		return nil, nil, cleanupFn, fmt.Errorf("failed to create DA client: %w", err)
	}

	// TODO(@facu): gas price and gas multiplier should be set by the node operator
	rollkitda := rollkitda.NewDAClient(&daClient.DA, 1, 1, []byte(cmtGenDoc.ChainID), []byte{}, logger)

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

	// Create the RPC provider with necessary dependencies
	// TODO: Pass actual indexers when implemented/available
	txIndexer := txindex.TxIndexer(nil)       // Placeholder for actual TxIndexer (uses cometbft/state/txindex)
	blockIndexer := indexer.BlockIndexer(nil) // Placeholder for actual BlockIndexer (uses cometbft/state/indexer)
	rpcProvider := provider.NewRpcProvider(executor, txIndexer, blockIndexer, servercmtlog.CometLoggerWrapper{Logger: logger})

	// Create the RPC handler using the provider
	rpcHandler, err := rpcjson.GetRPCHandler(rpcProvider, servercmtlog.CometLoggerWrapper{Logger: logger})
	if err != nil {
		return nil, nil, cleanupFn, fmt.Errorf("failed to create rpc handler: %w", err)
	}

	// Pass the created handler to the RPC server constructor
	rpcServer := rpc.NewRPCServer(rpcHandler, cfg.RPC, logger)
	err = rpcServer.Start()
	if err != nil {
		return nil, nil, cleanupFn, fmt.Errorf("failed to start rpc server: %w", err)
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
	if err = executor.Start(ctx); err != nil {
		return nil, nil, cleanupFn, fmt.Errorf("failed to start executor: %w", err)
	}

	return rolllkitNode, rpcProvider, cleanupFn, nil
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
