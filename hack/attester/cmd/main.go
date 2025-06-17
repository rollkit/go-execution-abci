package main

import (
	"context"
	"encoding/json"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtp2p "github.com/cometbft/cometbft/p2p"
	"github.com/spf13/cobra"

	execsigner "github.com/rollkit/go-execution-abci/pkg/signer"
)

const (
	flagKeyFile = "key-file"
	flagNode    = "node"
	flagChainID = "chain-id"
	flagHome    = "home"
	flagFrom    = "from"
)

func main() {
	rootCmd := &cobra.Command{
		Use:                        "attester",
		Short:                      "Attester client for Rollkit",
		Long:                       `Attester client for Rollkit that can join/leave the attester set and attest to blocks.`,
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
	}

	// Add subcommands
	rootCmd.AddCommand(
		initCmd(),
		joinAttesterCmd(),
		leaveAttesterCmd(),
		createRunCmd(),
	)

	// Execute
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// initCmd initializes the attester by creating a key pair
func initCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the attester by creating a key pair",
		RunE: func(cmd *cobra.Command, args []string) error {
			keyFile, err := cmd.Flags().GetString(flagKeyFile)
			if err != nil {
				return err
			}

			// Create directory if it doesn't exist
			dir := filepath.Dir(keyFile)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

			// Check if key file already exists
			if _, err := os.Stat(keyFile); err == nil {
				return fmt.Errorf("key file already exists: %s", keyFile)
			}

			// Generate new private key
			privKey := ed25519.GenPrivKey()

			// Create node key
			nodeKey := &cmtp2p.NodeKey{
				PrivKey: privKey,
			}

			// Save to file
			if err := nodeKey.SaveAs(keyFile); err != nil {
				return fmt.Errorf("failed to save key: %w", err)
			}

			fmt.Printf("Generated and saved key to %s\n", keyFile)
			fmt.Printf("Public key: %X\n", privKey.PubKey().Bytes())

			// Get address
			signer, err := execsigner.NewSignerWrapper(privKey)
			if err != nil {
				return fmt.Errorf("failed to create signer: %w", err)
			}

			address, err := signer.GetAddress()
			if err != nil {
				return fmt.Errorf("failed to get address: %w", err)
			}

			fmt.Printf("Address: %s\n", sdk.ValAddress(address).String())

			return nil
		},
	}

	cmd.Flags().String(flagKeyFile, "attester_key.json", "File to store the private key")
	return cmd
}

// joinAttesterCmd executes the gmd tx network join-attester command
func joinAttesterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "join-attester",
		Short: "Join the attester set",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get flags
			from, err := cmd.Flags().GetString(flagFrom)
			if err != nil {
				return err
			}

			chainID, err := cmd.Flags().GetString(flagChainID)
			if err != nil {
				return err
			}

			node, err := cmd.Flags().GetString(flagNode)
			if err != nil {
				return err
			}

			home, err := cmd.Flags().GetString(flagHome)
			if err != nil {
				return err
			}

			// Prepare gmd command
			gmdCmd := exec.Command("gmd", "tx", "network", "join-attester",
				"--from", from,
				"--chain-id", chainID,
				"--node", node,
				"--home", home,
				"-y", // auto-confirm
			)

			// Set output to current process stdout and stderr
			gmdCmd.Stdout = os.Stdout
			gmdCmd.Stderr = os.Stderr

			// Execute command
			fmt.Println("Executing:", gmdCmd.String())
			if err := gmdCmd.Run(); err != nil {
				return fmt.Errorf("failed to execute gmd command: %w", err)
			}

			fmt.Println("Successfully joined attester set")
			return nil
		},
	}

	// Add flags
	cmd.Flags().String(flagFrom, "", "Name or address of the account to sign with")
	cmd.Flags().String(flagChainID, "", "Chain ID of the blockchain")
	cmd.Flags().String(flagNode, "tcp://localhost:26657", "RPC node address")
	cmd.Flags().String(flagHome, "", "Directory for config and data")

	cmd.MarkFlagRequired(flagFrom)
	cmd.MarkFlagRequired(flagChainID)

	return cmd
}

// leaveAttesterCmd executes the gmd tx network leave-attester command
func leaveAttesterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "leave-attester",
		Short: "Leave the attester set",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get flags
			from, err := cmd.Flags().GetString(flagFrom)
			if err != nil {
				return err
			}

			chainID, err := cmd.Flags().GetString(flagChainID)
			if err != nil {
				return err
			}

			node, err := cmd.Flags().GetString(flagNode)
			if err != nil {
				return err
			}

			home, err := cmd.Flags().GetString(flagHome)
			if err != nil {
				return err
			}

			// Prepare gmd command
			gmdCmd := exec.Command("gmd", "tx", "network", "leave-attester",
				"--from", from,
				"--chain-id", chainID,
				"--node", node,
				"--home", home,
				"-y", // auto-confirm
			)

			// Set output to current process stdout and stderr
			gmdCmd.Stdout = os.Stdout
			gmdCmd.Stderr = os.Stderr

			// Execute command
			fmt.Println("Executing:", gmdCmd.String())
			if err := gmdCmd.Run(); err != nil {
				return fmt.Errorf("failed to execute gmd command: %w", err)
			}

			fmt.Println("Successfully left attester set")
			return nil
		},
	}

	// Add flags
	cmd.Flags().String(flagFrom, "", "Name or address of the account to sign with")
	cmd.Flags().String(flagChainID, "", "Chain ID of the blockchain")
	cmd.Flags().String(flagNode, "tcp://localhost:26657", "RPC node address")
	cmd.Flags().String(flagHome, "", "Directory for config and data")

	cmd.MarkFlagRequired(flagFrom)
	cmd.MarkFlagRequired(flagChainID)

	return cmd
}

// createRunCmd creates the command for running the attester process
func createRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the attester process",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get flags
			from, err := cmd.Flags().GetString(flagFrom)
			if err != nil {
				return err
			}

			chainID, err := cmd.Flags().GetString(flagChainID)
			if err != nil {
				return err
			}

			node, err := cmd.Flags().GetString(flagNode)
			if err != nil {
				return err
			}

			home, err := cmd.Flags().GetString(flagHome)
			if err != nil {
				return err
			}

			// Create context with cancellation
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle OS signals for graceful shutdown
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				fmt.Println("Received signal, shutting down...")
				cancel()
			}()

			fmt.Println("Attester running, monitoring for blocks...")
			fmt.Printf("RPC address: %s\n", node)

			// Create a simple HTTP client to query the node
			httpClient := &http.Client{
				Timeout: 10 * time.Second,
			}

			// Get epoch parameters
			paramsResp, err := httpClient.Get(fmt.Sprintf("%s/cosmos/network/v1/params", node))
			if err != nil {
				return fmt.Errorf("error getting params: %w", err)
			}
			defer paramsResp.Body.Close()

			var paramsResult struct {
				Params struct {
					EpochLength uint64 `json:"epoch_length"`
				} `json:"params"`
			}

			if err := json.NewDecoder(paramsResp.Body).Decode(&paramsResult); err != nil {
				return fmt.Errorf("error decoding params: %w", err)
			}

			epochLength := paramsResult.Params.EpochLength
			if epochLength == 0 {
				return fmt.Errorf("epoch length is 0")
			}

			// This is a simplified implementation - in a real-world scenario,
			// you would use a proper RPC client library
			go func() {
				var lastAttested int64 = 0
				for {
					select {
					case <-ctx.Done():
						return
					case <-time.After(1 * time.Second):
						// Poll for the latest block
						resp, err := httpClient.Get(fmt.Sprintf("%s/status", node))
						if err != nil {
							fmt.Printf("Error getting status: %v\n", err)
							continue
						}
						defer resp.Body.Close()

						var result struct {
							Result struct {
								SyncInfo struct {
									LatestBlockHeight string `json:"latest_block_height"`
								} `json:"sync_info"`
							} `json:"result"`
						}

						if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
							fmt.Printf("Error decoding response: %v\n", err)
							continue
						}

						height, err := strconv.ParseInt(result.Result.SyncInfo.LatestBlockHeight, 10, 64)
						if err != nil {
							fmt.Printf("Error parsing height: %v\n", err)
							continue
						}

						// Check if this is the end of a period and we haven't attested to it yet
						if height%int64(epochLength) == 0 && height > lastAttested {
							fmt.Printf("End of epoch at height %d, submitting attestation\n", height)

							// Get the block
							blockResp, err := httpClient.Get(fmt.Sprintf("%s/block?height=%d", node, height))
							if err != nil {
								fmt.Printf("Error getting block: %v\n", err)
								continue
							}
							defer blockResp.Body.Close()

							var blockResult struct {
								Result struct {
									BlockID struct {
										Hash string `json:"hash"`
									} `json:"block_id"`
								} `json:"result"`
							}

							if err := json.NewDecoder(blockResp.Body).Decode(&blockResult); err != nil {
								fmt.Printf("Error decoding block response: %v\n", err)
								continue
							}

							blockHash := blockResult.Result.BlockID.Hash

							// Submit attestation
							err = submitAttestation(ctx, from, chainID, node, home, height, blockHash)
							if err != nil {
								fmt.Printf("Error submitting attestation: %v\n", err)
								continue
							}

							lastAttested = height
						}
					}
				}
			}()

			// Wait for cancellation
			<-ctx.Done()
			return nil
		},
	}

	// Add flags
	cmd.Flags().String(flagFrom, "", "Name or address of the account to sign with")
	cmd.Flags().String(flagChainID, "", "Chain ID of the blockchain")
	cmd.Flags().String(flagNode, "tcp://localhost:26657", "RPC node address")
	cmd.Flags().String(flagHome, "", "Directory for config and data")
	cmd.Flags().Int64("epoch-height", 100, "Number of blocks in an epoch")

	cmd.MarkFlagRequired(flagFrom)
	cmd.MarkFlagRequired(flagChainID)

	return cmd
}

// submitAttestation creates and submits an attestation for a block using gmd
func submitAttestation(ctx context.Context, from, chainID, node, home string, height int64, blockHash string) error {
	// Prepare gmd command
	gmdCmd := exec.Command("gmd", "tx", "network", "attest",
		strconv.FormatInt(height, 10),
		blockHash,
		"--from", from,
		"--chain-id", chainID,
		"--node", node,
		"--home", home,
		"-y", // auto-confirm
	)

	// Set output to current process stdout and stderr
	gmdCmd.Stdout = os.Stdout
	gmdCmd.Stderr = os.Stderr

	// Execute command
	fmt.Println("Executing:", gmdCmd.String())
	if err := gmdCmd.Run(); err != nil {
		return fmt.Errorf("failed to execute gmd command: %w", err)
	}

	fmt.Println("Attestation submitted successfully")
	return nil
}
