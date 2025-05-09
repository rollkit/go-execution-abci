package core

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/cometbft/cometbft/p2p"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
)

// env is assumed to be a package-level variable or accessible through ctx
// as it is used by other functions in this file (e.g., Genesis).

const (
	// genesisChunkSize is the maximum size, in bytes, of each
	// chunk in the genesis structure for the chunked API.
	// This value is taken from the provided reference node/full_node.go.
	genesisChunkSize = 16 * 1024 * 1024 // 16 MiB
)

func NetInfo(ctx *rpctypes.Context) (*coretypes.ResultNetInfo, error) {
	res := coretypes.ResultNetInfo{
		Listening: true,       // Assuming node is always listening if RPC is up
		Listeners: []string{}, // TODO: Populate with actual listener addresses if available
	}

	// Access P2P client from adapter to get peer info
	if env.Adapter.P2PClient != nil {
		for _, ma := range env.Adapter.P2PClient.Addrs() {
			res.Listeners = append(res.Listeners, ma.String())
		}
		peers := env.Adapter.P2PClient.Peers()
		res.NPeers = len(peers)
		for _, peer := range peers {
			// Convert peer info to coretypes.Peer
			// Ensure p2p.DefaultNodeInfo is correctly populated from peer.NodeInfo
			res.Peers = append(res.Peers, coretypes.Peer{
				NodeInfo: p2p.DefaultNodeInfo{ // Adapt this based on actual available PeerInfo structure
					// Access fields via peer.NodeInfo
					DefaultNodeID: p2p.ID(peer.NodeInfo.NodeID),
					ListenAddr:    peer.NodeInfo.ListenAddr,
					Network:       peer.NodeInfo.Network,
					// Other fields like Moniker, Version might be available too
				},
				IsOutbound: peer.IsOutbound,
				RemoteIP:   peer.RemoteIP,
			})
		}
	} else {
		// Handle case where P2P client is not available or initialized
		res.NPeers = 0
		res.Peers = []coretypes.Peer{}
	}

	return &res, nil
}

func Genesis(ctx *rpctypes.Context) (*coretypes.ResultGenesis, error) {
	genesisDoc, err := env.Adapter.AppGenesis.ToGenesisDoc()
	if err != nil {
		return nil, err
	}

	return &coretypes.ResultGenesis{Genesis: genesisDoc}, nil
}

func GenesisChunked(_ *rpctypes.Context, chunk uint) (*coretypes.ResultGenesisChunk, error) {
	allChunks, err := getGenesisChunks()
	if err != nil {
		return nil, fmt.Errorf("error preparing genesis chunks: %w", err)
	}

	numChunks := len(allChunks)

	if numChunks == 0 {
		return nil, fmt.Errorf("genesis document is empty or yields no chunks")
	}

	if int(chunk) >= numChunks {
		return nil, fmt.Errorf("requested chunk index %d is out of bounds (total chunks: %d, valid indices: 0 to %d)", chunk, numChunks, numChunks-1)
	}

	return &coretypes.ResultGenesisChunk{ // Corrected from ctypes to coretypes
		TotalChunks: numChunks,
		ChunkNumber: int(chunk),
		Data:        allChunks[chunk],
	}, nil
}

// getGenesisChunks fetches the genesis document, marshals it to JSON,
// and then splits it into base64-encoded chunks.
// This function is based on the initGenesisChunks logic from the provided example node/full_node.go.
func getGenesisChunks() ([]string, error) {
	if env == nil || env.Adapter == nil || env.Adapter.AppGenesis == nil {
		return nil, fmt.Errorf("environment or adapter not initialized correctly for genesis access")
	}

	genesisDoc, err := env.Adapter.AppGenesis.ToGenesisDoc()
	if err != nil {
		return nil, fmt.Errorf("error getting genesis doc: %w", err)
	}
	if genesisDoc == nil {
		// If genesisDoc is nil, it implies no genesis content.
		// Return an empty list of chunks.
		return []string{}, nil
	}

	data, err := json.Marshal(genesisDoc)
	if err != nil {
		return nil, fmt.Errorf("error marshalling genesis doc to json: %w", err)
	}

	if len(data) == 0 {
		return []string{}, nil
	}

	var chunks []string
	for i := 0; i < len(data); i += genesisChunkSize {
		end := i + genesisChunkSize
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, base64.StdEncoding.EncodeToString(data[i:end]))
	}

	return chunks, nil
}
