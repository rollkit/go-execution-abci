package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/celestiaorg/tastora/framework/testutil/sdkacc"
	"github.com/celestiaorg/tastora/framework/testutil/wallet"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/docker/container"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	"github.com/celestiaorg/tastora/framework/types"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	denom = "stake"
)

// CreateCelestiaChain sets up a Celestia app chain for DA
func CreateCelestiaChain(ctx context.Context, t *testing.T, dockerClient *client.Client, networkID string) (types.Chain, error) {
	celestia, err := docker.NewChainBuilder(t).
		WithDockerClient(dockerClient).
		WithDockerNetworkID(networkID).
		WithImage(container.NewImage("ghcr.io/celestiaorg/celestia-app", "v4.0.0-rc6", "10001:10001")).
		WithAdditionalStartArgs(
			"--force-no-bbr",
			"--grpc.enable",
			"--grpc.address", "0.0.0.0:9090",
			"--rpc.grpc_laddr=tcp://0.0.0.0:9098",
			"--timeout-commit", "1s",
		).
		WithNode(docker.NewChainNodeConfigBuilder().Build()).
		Build(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to build celestia chain: %w", err)
	}

	err = celestia.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start celestia chain: %w", err)
	}

	return celestia, nil
}

// CreateDANetwork sets up the DA network with bridge and full nodes
func CreateDANetwork(ctx context.Context, t *testing.T, dockerClient *client.Client, networkID string, celestiaChain types.Chain) (types.DataAvailabilityNetwork, types.DANode, error) {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create logger: %w", err)
	}

	config := &docker.Config{
		Logger:          logger,
		DockerClient:    dockerClient,
		DockerNetworkID: networkID,
		DataAvailabilityNetworkConfig: &docker.DataAvailabilityNetworkConfig{
			Image: container.Image{
				Repository: "ghcr.io/celestiaorg/celestia-node",
				Version:    "pr-4283",
				UIDGID:     "10001:10001",
			},
			FullNodeCount:   1,
			BridgeNodeCount: 1,
		},
	}

	provider := docker.NewProvider(*config, t)
	daNetwork, err := provider.GetDataAvailabilityNetwork(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get DA network: %w", err)
	}

	genesisHash, err := getGenesisHash(ctx, celestiaChain)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get genesis hash: %w", err)
	}

	bridgeNodes := daNetwork.GetBridgeNodes()
	if len(bridgeNodes) == 0 {
		return nil, nil, fmt.Errorf("no bridge nodes available")
	}

	bridgeNode := bridgeNodes[0]

	chainID := celestiaChain.GetChainID()
	celestiaNodeHostname, err := celestiaChain.GetNodes()[0].GetInternalHostName(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get celestia node hostname: %w", err)
	}

	err = bridgeNode.Start(ctx,
		types.WithChainID(chainID),
		types.WithAdditionalStartArguments("--p2p.network", chainID, "--core.ip", celestiaNodeHostname, "--rpc.addr", "0.0.0.0"),
		types.WithEnvironmentVariables(map[string]string{
			"CELESTIA_CUSTOM": types.BuildCelestiaCustomEnvVar(chainID, genesisHash, ""),
			"P2P_NETWORK":     chainID,
		}),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start bridge node: %w", err)
	}

	return daNetwork, bridgeNode, nil
}

// CreateRollkitChain sets up the rollkit chain connected to the DA network
func CreateRollkitChain(ctx context.Context, t *testing.T, dockerClient *client.Client, networkID string, bridgeNode types.DANode) (*docker.Chain, error) {
	// Get DA connection details
	authToken, err := bridgeNode.GetAuthToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get auth token: %w", err)
	}

	bridgeRPCAddress, err := bridgeNode.GetInternalRPCAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get bridge RPC address: %w", err)
	}

	daAddress := fmt.Sprintf("http://%s", bridgeRPCAddress)
	namespace := generateValidNamespace()

	testEncCfg := testutil.MakeTestEncodingConfig(auth.AppModuleBasic{}, bank.AppModuleBasic{})
	rollkitChain, err := docker.NewChainBuilder(t).
		WithEncodingConfig(&testEncCfg).
		WithImage(container.NewImage("rollkit-gm", "latest", "10001:10001")).
		WithDenom(denom).
		WithDockerClient(dockerClient).
		WithName("rollkit").
		WithDockerNetworkID(networkID).
		WithChainID("rollkit-test").
		WithBech32Prefix("gm").
		WithBinaryName("gmd").
		WithGasPrices(fmt.Sprintf("0.00%s", denom)).
		WithNode(docker.NewChainNodeConfigBuilder().
			// Create aggregator node with rollkit-specific start arguments
			WithAdditionalStartArgs(
				"--rollkit.node.aggregator",
				"--rollkit.signer.passphrase", "12345678",
				"--rollkit.da.address", daAddress,
				"--rollkit.da.gas_price", "0.025",
				"--rollkit.da.auth_token", authToken,
				"--rollkit.rpc.address", "0.0.0.0:7331", // bind to 0.0.0.0 so rpc is reachable from test host.
				"--rollkit.da.namespace", namespace,
			).
			WithPostInit(func(ctx context.Context, node *docker.ChainNode) error {
				// Rollkit needs validators in the genesis validators array
				// Let's create the simplest possible validator to match what staking produces

				// Read current genesis.json
				genesisBz, err := node.ReadFile(ctx, "config/genesis.json")
				if err != nil {
					return fmt.Errorf("failed to read genesis.json: %w", err)
				}

				// Parse as generic JSON
				var genDoc map[string]interface{}
				if err := json.Unmarshal(genesisBz, &genDoc); err != nil {
					return fmt.Errorf("failed to parse genesis.json: %w", err)
				}

				// Extract public key from priv_validator_key.json (like ignite rollkit init does)
				privValidatorKeyBytes, err := node.ReadFile(ctx, "config/priv_validator_key.json")
				if err != nil {
					return fmt.Errorf("failed to read priv_validator_key.json: %w", err)
				}

				var privValidatorKey map[string]interface{}
				if err := json.Unmarshal(privValidatorKeyBytes, &privValidatorKey); err != nil {
					return fmt.Errorf("failed to parse priv_validator_key.json: %w", err)
				}

				// Extract public key from priv_validator_key.json
				pubKey := privValidatorKey["pub_key"].(map[string]interface{})
				pubKeyType := pubKey["type"].(string)
				pubKeyValue := pubKey["value"].(string)

				// Calculate address from public key (first 20 bytes of sha256 hash)
				pubkeyBytes, _ := base64.StdEncoding.DecodeString(pubKeyValue)
				hash := sha256.Sum256(pubkeyBytes)
				address := strings.ToUpper(hex.EncodeToString(hash[:20]))

				// Add rollkit sequencer validator to consensus.validators (like bash script does)
				consensus := genDoc["consensus"].(map[string]interface{})
				consensus["validators"] = []map[string]interface{}{
					{
						"name":    "Rollkit Sequencer",
						"address": address,
						"pub_key": map[string]interface{}{
							"type":  pubKeyType, // Use exact type from priv_validator_key.json
							"value": pubKeyValue,
						},
						"power": "5", // NOTE: because of default fund amount this has to be set to 5
					},
				}

				// Marshal and write back
				updatedGenesis, err := json.MarshalIndent(genDoc, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal genesis: %w", err)
				}
				return node.WriteFile(ctx, "config/genesis.json", updatedGenesis)
			}).
			Build()).
		Build(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to build rollkit chain: %w", err)
	}

	err = rollkitChain.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start rollkit chain: %w", err)
	}

	return rollkitChain, nil
}

// getGenesisHash retrieves the genesis hash from the celestia chain
func getGenesisHash(ctx context.Context, celestiaChain types.Chain) (string, error) {
	node := celestiaChain.GetNodes()[0]
	c, err := node.GetRPCClient()
	if err != nil {
		return "", fmt.Errorf("failed to get node client: %w", err)
	}

	first := int64(1)
	block, err := c.Block(ctx, &first)
	if err != nil {
		return "", fmt.Errorf("failed to get block: %w", err)
	}

	genesisHash := block.Block.Header.Hash().String()
	if genesisHash == "" {
		return "", fmt.Errorf("genesis hash is empty")
	}

	return genesisHash, nil
}

func generateValidNamespace() string {
	return hex.EncodeToString(share.RandomBlobNamespace().Bytes())
}

// queryBalanceViaRPC queries the balance of an address using direct RPC calls
func queryBalanceViaRPC(ctx context.Context, chain *docker.Chain, address string, denom string) (*sdk.Coin, error) {
	// Get gRPC address from the chain
	grpcAddr := chain.GetGRPCAddress()

	// Create gRPC connection
	conn, err := grpc.Dial(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to dial gRPC: %w", err)
	}
	defer conn.Close()

	// Create bank query client
	bankClient := banktypes.NewQueryClient(conn)

	// Query balance
	resp, err := bankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
		Address: address,
		Denom:   denom,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query balance: %w", err)
	}

	return resp.Balance, nil
}

// testTransactionSubmissionAndQuery tests sending transactions and querying results using tastora API
func testTransactionSubmissionAndQuery(t *testing.T, rollkitChain *docker.Chain) {

	// TODO: move somewhere else
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount("gm", "gmpub")
	cfg.Seal()

	ctx := context.Background()

	bobsWallet, err := wallet.CreateAndFund(ctx, "bob", sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1000))), rollkitChain)
	require.NoError(t, err, "failed to create bob wallet")

	carolsWallet, err := rollkitChain.CreateWallet(ctx, "carol")
	require.NoError(t, err, "failed to create carol wallet")

	t.Logf("Bob's address: %s", bobsWallet.GetFormattedAddress())
	t.Logf("Carol's address: %s", carolsWallet.GetFormattedAddress())

	// Query bob's initial balance using RPC
	t.Log("Querying Bob's initial balance...")
	initialBalance, err := queryBalanceViaRPC(ctx, rollkitChain, bobsWallet.GetFormattedAddress(), denom)
	require.NoError(t, err, "failed to query bob's initial balance")
	require.True(t, initialBalance.Amount.GTE(math.NewInt(100)), "bob should have more tokens")

	t.Logf("Bob's initial balance: %s", initialBalance.String())

	// Create and send bank transfer message
	t.Logf("Sending 100%s from Bob to Carol...", denom)
	transferAmount := sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100)))

	// Fund the da node address
	fromAddress, err := sdkacc.AddressFromWallet(bobsWallet)
	require.NoError(t, err)

	bz, err := sdk.GetFromBech32(carolsWallet.GetFormattedAddress(), "gm")
	require.NoError(t, err)

	toAddress := sdk.AccAddress(bz)

	msg := banktypes.NewMsgSend(fromAddress, toAddress, transferAmount)

	resp, err := rollkitChain.BroadcastMessages(ctx, bobsWallet, msg)

	require.NotNil(t, resp, "transaction resp should not be empty")
	require.NoError(t, err, "failed to broadcast transaction")
	require.Equal(t, uint32(0), resp.Code, "transaction should have succeeded")
	t.Logf("Transaction hash: %s", resp.TxHash)

	t.Log("Transaction executed successfully")

	// Query bob's final balance using RPC
	t.Log("Querying Bob's final balance...")
	finalBalance, err := queryBalanceViaRPC(ctx, rollkitChain, bobsWallet.GetFormattedAddress(), denom)
	require.NoError(t, err, "failed to query bob's final balance")

	t.Logf("Bob's final balance: %s", finalBalance.String())

	// Verify balance decreased by 100 (plus gas)
	expectedBalance := initialBalance.Amount.Sub(math.NewInt(100))
	require.True(t, finalBalance.Amount.LTE(expectedBalance), "final balance should be at most initial - 100 (accounting for gas)")
	require.True(t, finalBalance.Amount.GT(expectedBalance.Sub(math.NewInt(1000))), "final balance shouldn't be too much less than expected (reasonable gas cost)")

	// Query carol's balance using RPC to verify she received the transfer
	t.Log("Querying Carol's balance...")
	carolBalance, err := queryBalanceViaRPC(ctx, rollkitChain, carolsWallet.GetFormattedAddress(), denom)
	require.NoError(t, err, "failed to query carol's balance")
	require.True(t, carolBalance.Amount.GTE(math.NewInt(100)), "carol should have received at least 100 tokens")

	t.Logf("Carol's balance: %s", carolBalance.String())

	t.Log("✅ Transaction test successful! Balance correctly updated.")
}

func TestLivenessWithCelestiaDA(t *testing.T) {
	ctx := context.Background()
	dockerClient, networkID := docker.DockerSetup(t)

	celestiaChain, err := CreateCelestiaChain(ctx, t, dockerClient, networkID)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := celestiaChain.Stop(ctx); err != nil {
			t.Logf("failed to stop celestia chain: %v", err)
		}
	})

	t.Log("Celestia app chain started")

	_, bridgeNode, err := CreateDANetwork(ctx, t, dockerClient, networkID, celestiaChain)
	require.NoError(t, err)

	t.Log("Bridge node started")

	rollkitChain, err := CreateRollkitChain(ctx, t, dockerClient, networkID, bridgeNode)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := rollkitChain.Stop(ctx); err != nil {
			t.Logf("failed to stop rollkit chain: %v", err)
		}
	})

	t.Log("Rollkit chain started")

	// Test block production - wait for rollkit chain to produce blocks
	t.Log("Testing block production...")
	require.NoError(t, wait.ForBlocks(ctx, 5, rollkitChain))

	// Test transaction submission and query
	t.Log("Testing transaction submission and query...")
	testTransactionSubmissionAndQuery(t, rollkitChain)
}
