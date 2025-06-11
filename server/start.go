package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"cosmossdk.io/log"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/mempool"
	cmtp2p "github.com/cometbft/cometbft/p2p"
	pvm "github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cometbft/cometbft/state/indexer"
	"github.com/cometbft/cometbft/state/indexer/block"
	"github.com/cometbft/cometbft/state/txindex"
	cmttypes "github.com/cometbft/cometbft/types"
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

	"github.com/rollkit/rollkit/da/jsonrpc"
	"github.com/rollkit/rollkit/node"
	"github.com/rollkit/rollkit/pkg/config"
	"github.com/rollkit/rollkit/pkg/genesis"
	"github.com/rollkit/rollkit/pkg/p2p"
	"github.com/rollkit/rollkit/pkg/p2p/key"
	"github.com/rollkit/rollkit/pkg/signer"
	"github.com/rollkit/rollkit/pkg/store"
	"github.com/rollkit/rollkit/sequencers/single"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
	"github.com/rollkit/go-execution-abci/pkg/cometcompat"
	"github.com/rollkit/go-execution-abci/pkg/rpc"
	"github.com/rollkit/go-execution-abci/pkg/rpc/core"
	execsigner "github.com/rollkit/go-execution-abci/pkg/signer"
)

const (
	flagTraceStore = "trace-store"
	flagTransport  = "transport"
	flagGRPCOnly   = "grpc-only"
)

// StartCommandHandler is the type that must implement nova to match Cosmos SDK start logic.
type StartCommandHandler = func(
	svrCtx *server.Context,
	clientCtx client.Context,
	appCreator sdktypes.AppCreator,
	withCmt bool,
	opts server.StartCmdOptions,
) error

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
	g, ctx, cancelFn := getCtx(svrCtx)
	defer cancelFn()

	if gRPCOnly := svrCtx.Viper.GetBool(flagGRPCOnly); gRPCOnly {
		// TODO: Generalize logic so that gRPC only is really in startStandAlone
		svrCtx.Logger.Info("starting node in gRPC only mode; CometBFT is disabled")
		svrCfg.GRPC.Enable = true
	} else {
		svrCtx.Logger.Info("starting node with ABCI CometBFT in-process")
		rollkitNode, executor, cleanupFn, err := setupNodeAndExecutor(ctx, svrCtx, cmtCfg, app)
		if err != nil {
			return err
		}
		defer cleanupFn()

		g.Go(func() error {
			svrCtx.Logger.Info("rollkit node run loop launched in background goroutine")
			err := rollkitNode.Run(ctx)
			if err == context.Canceled {
				svrCtx.Logger.Info("rollkit node run loop cancelled by context")
			} else if err != nil {
				return fmt.Errorf("rollkit node run failed: %w", err)
			}

			// cancel context to stop all other processes
			cancelFn()

			return nil
		})

		// Wait for the node to start p2p before attempting to start the gossiper
		time.Sleep(1 * time.Second)

		// Start the executor (Adapter) AFTER launching the node goroutine.
		// Assumption: rollkitNode.Run initializes PubSub quickly enough.
		svrCtx.Logger.Info("attempting to start executor (Adapter.Start)")
		if err := executor.Start(ctx); err != nil {
			svrCtx.Logger.Error("failed to start executor", "error", err)
			// If this fails, the node goroutine might still be running.
			// The errgroup context cancellation should handle shutdown.
			return fmt.Errorf("failed to start executor: %w", err)
		}
		svrCtx.Logger.Info("executor started successfully")

		// Add the tx service to the gRPC router.
		if svrCfg.API.Enable || svrCfg.GRPC.Enable {
			// Use the started rpcServer for the client context
			// clientCtx = clientCtx.WithClient(rpcProvider)

			app.RegisterTxService(clientCtx)
			app.RegisterTendermintService(clientCtx)
			app.RegisterNodeService(clientCtx, svrCfg)
		}
	}

	// Start gRPC Server (if enabled)
	grpcSrv, clientCtx, err := startGrpcServer(ctx, g, svrCfg.GRPC, clientCtx, svrCtx, app)
	if err != nil {
		return err
	}

	// Start API Server (if enabled)
	err = startAPIServer(ctx, g, svrCfg, clientCtx, svrCtx, app, cmtCfg.RootDir, grpcSrv, metrics)
	if err != nil {
		return err
	}

	if opts.PostSetup != nil {
		if err := opts.PostSetup(svrCtx, clientCtx, ctx, g); err != nil {
			return err
		}
	}

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

	// Start the gRPC server in a goroutine.
	// Note, the provided ctx will ensure that the server is gracefully shut down.
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

func setupNodeAndExecutor(
	ctx context.Context,
	srvCtx *server.Context,
	cfg *cmtcfg.Config,
	app sdktypes.Application,
) (rolllkitNode node.Node, executor *adapter.Adapter, cleanupFn func(), err error) {
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

	executor = adapter.NewABCIExecutor(
		app,
		database,
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

	height, err := executor.RollkitStore.Height(context.Background())
	if err != nil {
		return nil, nil, cleanupFn, err
	}
	mempool := mempool.NewCListMempool(cfg.Mempool, proxyApp.Mempool(), int64(height))
	executor.SetMempool(mempool)

	rollkitGenesis := genesis.NewGenesis(
		cmtGenDoc.ChainID,
		uint64(cmtGenDoc.InitialHeight),
		cmtGenDoc.GenesisTime,
		cmtGenDoc.Validators[0].Address.Bytes(), // use the first validator as sequencer
	)

	// create the DA client
	daClient, err := jsonrpc.NewClient(ctx, logger, rollkitcfg.DA.Address, rollkitcfg.DA.AuthToken, rollkitcfg.DA.Namespace)
	if err != nil {
		return nil, nil, cleanupFn, fmt.Errorf("failed to create DA client: %w", err)
	}

	singleMetrics, err := single.NopMetrics()
	if err != nil {
		return nil, nil, cleanupFn, err
	}

	if rollkitcfg.Instrumentation.IsPrometheusEnabled() {
		singleMetrics, err = single.PrometheusMetrics(config.DefaultInstrumentationConfig().Namespace, "chain_id", cmtGenDoc.ChainID)
		if err != nil {
			return nil, nil, cleanupFn, err
		}
	}

	sequencer, err := single.NewSequencer(
		ctx,
		logger,
		database,
		&daClient.DA,
		[]byte(cmtGenDoc.ChainID),
		rollkitcfg.Node.BlockTime.Duration,
		singleMetrics,
		rollkitcfg.Node.Aggregator,
	)
	if err != nil {
		return nil, nil, cleanupFn, err
	}

	rolllkitNode, err = node.NewNode(
		ctx,
		rollkitcfg,
		executor,
		sequencer,
		&daClient.DA,
		signer,
		p2pClient,
		rollkitGenesis,
		database,
		metrics,
		logger,
		cometcompat.PayloadProvider(),
	)
	if err != nil {
		return nil, nil, cleanupFn, err
	}
	eventBus, err := createAndStartEventBus(logger)
	if err != nil {
		return nil, nil, cleanupFn, fmt.Errorf("setup event-bus: %w", err)
	}
	executor.EventBus = eventBus

	idxSvc, txIndexer, blockIndexer, err := createAndStartIndexerService(cfg, cmtGenDoc.ChainID, cmtcfg.DefaultDBProvider, eventBus, logger)
	if err != nil {
		return nil, nil, cleanupFn, fmt.Errorf("start indexer service: %w", err)
	}
	core.SetEnvironment(&core.Environment{
		Signer:       signer,
		Adapter:      executor,
		TxIndexer:    txIndexer,
		BlockIndexer: blockIndexer,
		Logger:       servercmtlog.CometLoggerWrapper{Logger: logger},
		Config:       *cfg.RPC,
	})

	// Pass the created handler to the RPC server constructor
	rpcServer := rpc.NewRPCServer(cfg.RPC, logger)
	if err = rpcServer.Start(); err != nil {
		return nil, nil, cleanupFn, fmt.Errorf("failed to start rpc server: %w", err)
	}

	cleanupFn = func() {
		if eventBus != nil {
			_ = eventBus.Stop()
		}
		if idxSvc != nil {
			_ = idxSvc.Stop()
		}
		if rpcServer != nil {
			_ = rpcServer.Stop()
		}
	}

	return rolllkitNode, executor, cleanupFn, nil
}

func createAndStartEventBus(logger log.Logger) (*cmttypes.EventBus, error) {
	eventBus := cmttypes.NewEventBus()
	eventBus.SetLogger(servercmtlog.CometLoggerWrapper{Logger: logger}.With("module", "events"))
	if err := eventBus.Start(); err != nil {
		return nil, err
	}
	return eventBus, nil
}

func createAndStartIndexerService(
	config *cmtcfg.Config,
	chainID string,
	dbProvider cmtcfg.DBProvider,
	eventBus *cmttypes.EventBus,
	logger log.Logger,
) (*txindex.IndexerService, txindex.TxIndexer, indexer.BlockIndexer, error) {
	var (
		txIndexer    txindex.TxIndexer
		blockIndexer indexer.BlockIndexer
	)

	txIndexer, blockIndexer, allIndexersDisabled, err := block.IndexerFromConfigWithDisabledIndexers(config, dbProvider, chainID)
	if err != nil {
		return nil, nil, nil, err
	}
	if allIndexersDisabled {
		return nil, txIndexer, blockIndexer, nil
	}

	cmtLogger := servercmtlog.CometLoggerWrapper{Logger: logger}.With("module", "txindex")
	txIndexer.SetLogger(cmtLogger)
	blockIndexer.SetLogger(cmtLogger)

	indexerService := txindex.NewIndexerService(txIndexer, blockIndexer, eventBus, false)
	indexerService.SetLogger(cmtLogger)
	if err := indexerService.Start(); err != nil {
		return nil, nil, nil, err
	}

	return indexerService, txIndexer, blockIndexer, nil
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

func getCtx(svrCtx *server.Context) (*errgroup.Group, context.Context, context.CancelFunc) {
	ctx, cancelFn := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)
	// listen for quit signals so the calling parent process can gracefully exit
	server.ListenForQuitSignals(g /* block */, false, cancelFn, svrCtx.Logger)
	return g, ctx, cancelFn
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
