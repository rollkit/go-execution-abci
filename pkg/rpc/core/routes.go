package core

import (
	"github.com/cometbft/cometbft/rpc/jsonrpc/server"
)

var Routes = map[string]*server.RPCFunc{
	// subscribe/unsubscribe are reserved for websocket events.
	"subscribe":       server.NewWSRPCFunc(Subscribe, "query"),
	"unsubscribe":     server.NewWSRPCFunc(Unsubscribe, "query"),
	"unsubscribe_all": server.NewWSRPCFunc(UnsubscribeAll, ""),

	// // info API
	"health":               server.NewRPCFunc(Health, ""),
	"status":               server.NewRPCFunc(Status, ""),
	"net_info":             server.NewRPCFunc(NetInfo, ""),
	"blockchain":           server.NewRPCFunc(BlockchainInfo, "minHeight,maxHeight", server.Cacheable()),
	"genesis":              server.NewRPCFunc(Genesis, "", server.Cacheable()),
	"genesis_chunked":      server.NewRPCFunc(GenesisChunked, "chunk", server.Cacheable()),
	"block":                server.NewRPCFunc(Block, "height", server.Cacheable("height")),
	"block_by_hash":        server.NewRPCFunc(BlockByHash, "hash", server.Cacheable()),
	"block_results":        server.NewRPCFunc(BlockResults, "height", server.Cacheable("height")),
	"commit":               server.NewRPCFunc(Commit, "height", server.Cacheable("height")),
	"header":               server.NewRPCFunc(Header, "height", server.Cacheable("height")),
	"header_by_hash":       server.NewRPCFunc(HeaderByHash, "hash", server.Cacheable()),
	"check_tx":             server.NewRPCFunc(CheckTx, "tx"),
	"tx":                   server.NewRPCFunc(Tx, "hash,prove", server.Cacheable()),
	"tx_search":            server.NewRPCFunc(TxSearch, "query,prove,page,per_page,order_by"),
	"block_search":         server.NewRPCFunc(BlockSearch, "query,page,per_page,order_by"),
	"validators":           server.NewRPCFunc(Validators, "height,page,per_page", server.Cacheable("height")),
	"dump_consensus_state": server.NewRPCFunc(DumpConsensusState, ""),
	"consensus_state":      server.NewRPCFunc(ConsensusState, ""),
	"consensus_params":     server.NewRPCFunc(ConsensusParams, "height", server.Cacheable("height")),
	"unconfirmed_txs":      server.NewRPCFunc(UnconfirmedTxs, "limit"),
	"num_unconfirmed_txs":  server.NewRPCFunc(NumUnconfirmedTxs, ""),

	// // tx broadcast API
	"broadcast_tx_commit": server.NewRPCFunc(BroadcastTxCommit, "tx"),
	"broadcast_tx_sync":   server.NewRPCFunc(BroadcastTxSync, "tx"),
	"broadcast_tx_async":  server.NewRPCFunc(BroadcastTxAsync, "tx"),

	// // abci API
	"abci_query": server.NewRPCFunc(ABCIQuery, "path,data,height,prove"),
	"abci_info":  server.NewRPCFunc(ABCIInfo, "", server.Cacheable()),

	// // evidence API
	"broadcast_evidence": server.NewRPCFunc(BroadcastEvidence, "evidence"),
}
