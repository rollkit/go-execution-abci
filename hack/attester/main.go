package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

const (
	flagFrom    = "from"
	flagChainID = "chain-id"
	flagNode    = "node"
	flagAPIAddr = "api-addr"
	flagHome    = "home"
	flagVerbose = "verbose"
)

func main() {
	rootCmd := &cobra.Command{
		Use:                        "attester_ws",
		Short:                      "Attester client for Rollkit using websocket",
		Long:                       `Attester client for Rollkit that joins the attester set and attests to blocks at the end of each epoch.`,
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       runAttester,
	}

	// Add flags
	rootCmd.Flags().String(flagFrom, "validator", "Name or address of the account to sign with")
	rootCmd.Flags().String(flagChainID, "", "Chain ID of the blockchain")
	rootCmd.Flags().String(flagNode, "tcp://localhost:26657", "RPC node address")
	rootCmd.Flags().String(flagAPIAddr, "http://localhost:1317", "API node address")
	rootCmd.Flags().String(flagHome, "", "Directory for config and data")
	rootCmd.Flags().Bool(flagVerbose, false, "Enable verbose output")

	_ = rootCmd.MarkFlagRequired(flagChainID)
	_ = rootCmd.MarkFlagRequired(flagHome)

	// Execute
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runAttester(cmd *cobra.Command, args []string) error {
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
	apiAddr, err := cmd.Flags().GetString(flagAPIAddr)
	if err != nil {
		return err
	}

	home, err := cmd.Flags().GetString(flagHome)
	if err != nil {
		return err
	}

	verbose, err := cmd.Flags().GetBool(flagVerbose)
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

	// Step 1: Join attester set
	fmt.Println("Joining attester set...")
	if err := joinAttesterSet(from, chainID, node, home, verbose); err != nil {
		return fmt.Errorf("join attester set: %w", err)
	}

	// Step 2: Query network parameters to get epoch length
	fmt.Println("Querying network parameters...")
	epochLength, err := getEpochLength(apiAddr)
	if err != nil {
		return fmt.Errorf("get epoch length: %w", err)
	}
	fmt.Printf("Epoch length: %d blocks\n", epochLength)

	// Step 3 & 4: Watch new block events via websocket and attest at the end of each epoch
	fmt.Println("Starting to watch for new blocks...")
	if err := pullBlocksAndAttest(ctx, from, chainID, node, home, epochLength, verbose); err != nil {
		return fmt.Errorf("error watching blocks: %w", err)
	}

	return nil
}

// joinAttesterSet executes the gmd tx network join-attester command
func joinAttesterSet(from, chainID, node, home string, verbose bool) error {
	// Prepare gmd command
	args := []string{
		"tx", "network", "join-attester",
		"--from", from,
		"--chain-id", chainID,
		"--node", node,
		"--home", home,
		"--keyring-backend=test",
		"-y", // auto-confirm
	}
	gmdCmd := exec.Command("gmd", args...)

	// Set output to current process stdout and stderr
	gmdCmd.Stdout = os.Stdout
	gmdCmd.Stderr = os.Stderr

	// Execute command
	if verbose {
		fmt.Println("Executing command with all parameters:")
		fmt.Printf("gmd %s\n", formatCommandArgs(args))
	} else {
		fmt.Println("Executing:", gmdCmd.String())
	}

	if err := gmdCmd.Run(); err != nil {
		return fmt.Errorf("execute gmd command: %w", err)
	}

	fmt.Println("Successfully joined attester set")
	return nil
}

// getEpochLength queries the network parameters to get the epoch length
func getEpochLength(apiAddr string) (uint64, error) {
	// Create a simple HTTP client to query the node
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Get epoch parameters
	paramsResp, err := httpClient.Get(fmt.Sprintf("%s/rollkit/network/v1/params", apiAddr))
	if err != nil {
		return 0, fmt.Errorf("error getting params: %w", err)
	}
	defer paramsResp.Body.Close() //nolint:errcheck // test code

	var paramsResult struct {
		Params struct {
			EpochLength string `json:"epoch_length"`
		} `json:"params"`
	}
	var buf bytes.Buffer

	if err := json.NewDecoder(io.TeeReader(paramsResp.Body, &buf)).Decode(&paramsResult); err != nil {
		return 0, fmt.Errorf("error decoding params: %w: got %s", err, buf.String())
	}

	epochLength, err := strconv.ParseUint(paramsResult.Params.EpochLength, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("epoch length: %w", err)
	}
	if epochLength == 0 {
		return 0, fmt.Errorf("epoch length is 0")
	}

	return epochLength, nil
}

// pullBlocksAndAttest polls for new blocks via HTTP and attests at the end of each epoch
func pullBlocksAndAttest(ctx context.Context, from, chainID, node, home string, epochLength uint64, verbose bool) error {
	// Parse node URL
	parsed, err := url.Parse(node)
	if err != nil {
		return fmt.Errorf("parse node URL: %w", err)
	}

	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	var lastAttested int64 = 0

	// Poll for new blocks
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// Query latest block
			resp, err := httpClient.Get(fmt.Sprintf("http://%s/block", parsed.Host))
			if err != nil {
				fmt.Printf("Error querying block: %v\n", err)
				time.Sleep(time.Second / 10)
				continue
			}

			var blockResponse struct {
				Result struct {
					Block struct {
						Header struct {
							Height string `json:"height"`
						} `json:"header"`
					} `json:"block"`
				} `json:"result"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&blockResponse); err != nil {
				fmt.Printf("Error parsing response: %v\n", err)
				_ = resp.Body.Close()
				time.Sleep(time.Second / 10)
				continue
			}
			_ = resp.Body.Close()

			// Extract block height
			height, err := strconv.ParseInt(blockResponse.Result.Block.Header.Height, 10, 64)
			if err != nil {
				fmt.Printf("Error parsing height: %v\n", err)
				time.Sleep(time.Second / 10)
				continue
			}

			fmt.Printf("Current block: %d\n", height)

			// Check if this is the end of an epoch and we haven't attested to it yet
			if height > 1 && height%int64(epochLength) == 0 && height > lastAttested {
				fmt.Printf("End of epoch at height %d, submitting attestation\n", height)

				// Submit attestation with "0x00" as the hash
				err = submitAttestation(ctx, from, chainID, node, home, height, "0x00", verbose)
				if err != nil {
					fmt.Printf("Error submitting attestation: %v\n", err)
					//time.Sleep(time.Second / 10)
					//continue
					return err
				}

				lastAttested = height
			}

			// Wait before next poll
			time.Sleep(time.Second / 10)
		}
	}
}

// formatCommandArgs formats command arguments for verbose output
func formatCommandArgs(args []string) string {
	var result string
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		// Add quotes if the argument contains spaces
		if containsSpace(arg) {
			result += "\"" + arg + "\""
		} else {
			result += arg
		}
	}
	return result
}

// containsSpace checks if a string contains any space character
func containsSpace(s string) bool {
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			return true
		}
	}
	return false
}

// submitAttestation creates and submits an attestation for a block using gmd
func submitAttestation(ctx context.Context, from, chainID, node, home string, height int64, blockHash string, verbose bool) error {
	// Prepare gmd command
	args := []string{
		"tx", "network", "attest",
		strconv.FormatInt(height, 10),
		blockHash,
		"--from", from,
		"--chain-id", chainID,
		"--node", node,
		"--home", home,
		"--keyring-backend=test",
		"-y", // auto-confirm
	}
	gmdCmd := exec.Command("gmd", args...)

	// Set output to current process stdout and stderr
	gmdCmd.Stdout = os.Stdout
	gmdCmd.Stderr = os.Stderr

	// Execute command
	if verbose {
		fmt.Println("Executing command with all parameters:")
		fmt.Printf("gmd %s\n", formatCommandArgs(args))
	} else {
		fmt.Println("Executing:", gmdCmd.String())
	}

	if err := gmdCmd.Run(); err != nil {
		return fmt.Errorf("execute gmd command: %w", err)
	}

	fmt.Println("Attestation submitted successfully")
	return nil
}
