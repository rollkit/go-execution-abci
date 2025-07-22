package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/docker/container"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	"github.com/celestiaorg/tastora/framework/types"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"strings"
	"testing"
)

const (
	// pre-built rollkit chain image (built by workflow before test)
	rollkitChainImage = "rollkit-chain:latest"
)

type LivenessTestSuite struct {
	suite.Suite
	ctx           context.Context
	dockerClient  *client.Client
	networkID     string
	logger        *zap.Logger
	provider      *docker.Provider
	celestiaChain types.Chain
	rollkitChain  *docker.Chain
}

func (s *LivenessTestSuite) SetupSuite() {
	s.ctx = context.Background()

	// setup logger
	logger, err := zap.NewDevelopment()
	s.Require().NoError(err)
	s.logger = logger

	// setup docker
	s.dockerClient, s.networkID = docker.DockerSetup(s.T())

	// setup celestia DA network
	s.setupCelestiaNetwork()
}

func (s *LivenessTestSuite) setupCelestiaNetwork() {
	builder := docker.NewChainBuilder(s.T()).
		WithDockerClient(s.dockerClient).
		WithDockerNetworkID(s.networkID).
		WithImage(container.NewImage("ghcr.io/celestiaorg/celestia-app", "v4.0.0-rc6", "10001:10001")).
		WithAdditionalStartArgs(
			"--force-no-bbr",
			"--grpc.enable",
			"--grpc.address", "0.0.0.0:9090",
			"--rpc.grpc_laddr=tcp://0.0.0.0:9098",
			"--timeout-commit", "1s",
		).
		WithNode(docker.NewChainNodeConfigBuilder().Build())

	// build and start celestia chain
	chain, err := builder.Build(s.ctx)
	s.Require().NoError(err)
	s.celestiaChain = chain

	err = chain.Start(s.ctx)
	s.Require().NoError(err)
	s.T().Log("Celestia app chain started")

	// create our DA provider with config
	config := &docker.Config{
		Logger:          s.logger,
		DockerClient:    s.dockerClient,
		DockerNetworkID: s.networkID,
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
	s.provider = docker.NewProvider(*config, s.T())
}

func (s *LivenessTestSuite) TearDownSuite() {
	// TODO: implement Stop method for rollkit chain
	if s.celestiaChain != nil {
		_ = s.celestiaChain.Stop(s.ctx)
	}
}

func (s *LivenessTestSuite) TestLivenessWithCelestiaDA() {
	// get genesis hash from celestia chain
	genesisHash, err := s.getGenesisHash()
	s.Require().NoError(err)
	s.T().Logf("Genesis hash: %s", genesisHash)

	// get DA network
	daNetwork, err := s.provider.GetDataAvailabilityNetwork(s.ctx)
	s.Require().NoError(err)

	// start bridge node with correct genesis hash
	bridgeNodes := daNetwork.GetBridgeNodes()
	s.Require().Len(bridgeNodes, 1)
	bridgeNode := bridgeNodes[0]

	chainID := s.celestiaChain.GetChainID()

	err = bridgeNode.Start(s.ctx,
		types.WithChainID(chainID),
		types.WithAdditionalStartArguments("--p2p.network", chainID, "--core.ip", "celestia-app", "--rpc.addr", "0.0.0.0"),
		types.WithEnvironmentVariables(map[string]string{
			"CELESTIA_CUSTOM": types.BuildCelestiaCustomEnvVar(chainID, genesisHash, ""),
			"P2P_NETWORK":     chainID,
		}),
	)
	s.Require().NoError(err)
	s.T().Log("Bridge node started")

	// get DA connection details
	authToken, err := bridgeNode.GetAuthToken()
	s.Require().NoError(err)

	bridgeRPCAddress, err := bridgeNode.GetInternalRPCAddress()
	s.Require().NoError(err)

	daAddress := fmt.Sprintf("http://%s", bridgeRPCAddress)
	// start rollkit chain using pre-built image
	s.startRollkitChain(authToken, daAddress)

	// test block production (wait for 5 blocks)
	s.testBlockProduction()
}

func (s *LivenessTestSuite) getGenesisHash() (string, error) {
	// getGenesisHash retrieves the genesis hash from the celestia chain
	node := s.celestiaChain.GetNodes()[0]
	c, err := node.GetRPCClient()
	if err != nil {
		return "", fmt.Errorf("failed to get node client: %w", err)
	}

	first := int64(1)
	block, err := c.Block(s.ctx, &first)
	if err != nil {
		return "", fmt.Errorf("failed to get block: %w", err)
	}

	genesisHash := block.Block.Header.Hash().String()
	if genesisHash == "" {
		return "", fmt.Errorf("genesis hash is empty")
	}

	return genesisHash, nil
}

func (s *LivenessTestSuite) startRollkitChain(authToken, daAddress string) {
	s.T().Log("Starting rollkit chain...")

	namespace := generateValidNamespace()

	rollkitChain, err := docker.NewChainBuilder(s.T()).
		WithImage(container.NewImage("rollkit-gm", "latest", "10001:10001")).
		WithDenom("utia").
		WithDockerClient(s.dockerClient).
		WithName("rollkit").
		WithDockerNetworkID(s.networkID).
		WithChainID("rollkit-test").
		WithBech32Prefix("gm").
		WithBinaryName("gmd").
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
						"power": "5",
					},
				}

				genDoc["initial_height"] = 1

				// Marshal and write back
				updatedGenesis, err := json.MarshalIndent(genDoc, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal genesis: %w", err)
				}
				return node.WriteFile(ctx, "config/genesis.json", updatedGenesis)
			}).
			Build()).
		Build(s.ctx)

	s.Require().NoError(err)

	s.rollkitChain = rollkitChain

	err = s.rollkitChain.Start(s.ctx)
	s.Require().NoError(err)
}

func generateValidNamespace() string {
	return hex.EncodeToString(share.RandomBlobNamespace().Bytes())
}

func (s *LivenessTestSuite) testBlockProduction() {
	s.T().Log("Testing block production...")

	s.Require().NoError(wait.ForBlocks(s.ctx, 5, s.rollkitChain))

	s.T().Log("Block production test passed - rollkit chain is running")
}

func TestLivenessSuite(t *testing.T) {
	suite.Run(t, new(LivenessTestSuite))
}
