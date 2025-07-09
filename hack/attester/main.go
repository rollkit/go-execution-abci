package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	dbm "github.com/cometbft/cometbft-db"
	cmtlight "github.com/cometbft/cometbft/light"
	cmtprovider "github.com/cometbft/cometbft/light/provider"
	cmthttp "github.com/cometbft/cometbft/light/provider/http"
	cmtstore "github.com/cometbft/cometbft/light/store/db"
	pvm "github.com/cometbft/cometbft/privval"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/std"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/spf13/cobra"

	networktypes "github.com/rollkit/go-execution-abci/modules/network/types"
)

const (
	flagChainID  = "chain-id"
	flagNode     = "node"
	flagAPIAddr  = "api-addr"
	flagHome     = "home"
	flagVerbose  = "verbose"
	flagMnemonic = "mnemonic"
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
	rootCmd.Flags().String(flagChainID, "", "Chain ID of the blockchain")
	rootCmd.Flags().String(flagNode, "tcp://localhost:26657", "RPC node address")
	rootCmd.Flags().String(flagAPIAddr, "http://localhost:1317", "API node address")
	rootCmd.Flags().String(flagHome, "", "Directory for config and data")
	rootCmd.Flags().Bool(flagVerbose, false, "Enable verbose output")
	rootCmd.Flags().String(flagMnemonic, "", "Mnemonic for the private key")

	_ = rootCmd.MarkFlagRequired(flagChainID)
	_ = rootCmd.MarkFlagRequired(flagMnemonic)

	// Execute
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func runAttester(cmd *cobra.Command, args []string) error {
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

	mnemonic, err := cmd.Flags().GetString(flagMnemonic)
	if err != nil {
		return err
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("gm", "gmpub")
	config.SetBech32PrefixForValidator("gmvaloper", "gmvaloperpub")
	config.Seal()

	// Create the sender key from the mnemonic
	senderKey, err := createPrivateKeyFromMnemonic(mnemonic)
	if err != nil {
		return fmt.Errorf("failed to create private key from mnemonic: %w", err)
	}

	// Get the account address from the private key
	pubKey := senderKey.PubKey()
	valAddr := sdk.ValAddress(pubKey.Address())

	if verbose {
		addr := sdk.AccAddress(pubKey.Address())
		fmt.Printf("Sender Account address: %s\n", addr.String())
		fmt.Printf("Sender Validator address: %s\n", valAddr.String())
	}

	pv := pvm.LoadOrGenFilePV(filepath.Join(home, "config", "priv_validator_key.json"), filepath.Join(home, "data", "priv_validator_state.json"))

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
	if err := joinAttesterSet(ctx, chainID, node, verbose, valAddr, senderKey, pv); err != nil {
		return fmt.Errorf("join attester set: %w", err)
	}

	// Step 2: Query network parameters to get epoch length
	fmt.Println("Querying network parameters...")
	epochLength, err := getEpochLength(apiAddr)
	if err != nil {
		return fmt.Errorf("get epoch length: %w", err)
	}
	fmt.Printf("Epoch length: %d blocks\n", epochLength)

	// Get validator address from private key
	valAccAddr := sdk.AccAddress(senderKey.PubKey().Address())

	// Step 3 & 4: Watch new block events via websocket and attest at the end of each epoch
	fmt.Println("Starting to watch for new blocks...")
	if err := pullBlocksAndAttest(ctx, chainID, node, home, epochLength, valAccAddr.Bytes(), verbose, senderKey, pv); err != nil {
		return fmt.Errorf("error watching blocks: %w", err)
	}

	return nil
}

// joinAttesterSet creates and submits a MsgJoinAttesterSet transaction
func joinAttesterSet(ctx context.Context, chainID, node string, verbose bool, valAddr sdk.ValAddress, privKey *secp256k1.PrivKey, pv *pvm.FilePV) error {
	if verbose {
		fmt.Printf("Using validator address: %s\n", valAddr.String())
	}

	// Create the message
	msg := networktypes.NewMsgJoinAttesterSet(valAddr.String())

	// Broadcast the transaction
	txHash, err := broadcastTx(ctx, chainID, node, msg, privKey, verbose)
	if err != nil {
		return fmt.Errorf("broadcast join attester set tx: %w", err)
	}

	fmt.Printf("Successfully joined attester set. Tx hash: %s\n", txHash)
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
func pullBlocksAndAttest(
	ctx context.Context,
	chainID, node, home string,
	epochLength uint64,
	valAddr sdk.ValAddress,
	verbose bool,
	senderKey *secp256k1.PrivKey,
	pv *pvm.FilePV,
) error {
	// Parse node URL
	parsed, err := url.Parse(node)
	if err != nil {
		return fmt.Errorf("parse node URL: %w", err)
	}

	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	var lastAttested int64 = 0
	var lastAppHash []byte

	// Initialize light client
	if verbose {
		fmt.Println("Initializing light client...")
	}

	// Create a database for the light client
	lightClientDB, err := dbm.NewGoLevelDB("light-client", filepath.Join(home, "data"))
	if err != nil {
		return fmt.Errorf("failed to create light client database: %w", err)
	}
	defer lightClientDB.Close()

	// Create a store for the light client
	lightClientStore := cmtstore.New(lightClientDB, "light-client")

	// Create a primary provider for the light client
	primaryProvider, err := cmthttp.New(chainID, fmt.Sprintf("http://%s", parsed.Host))
	if err != nil {
		return fmt.Errorf("failed to create primary provider: %w", err)
	}

	// Create a witness provider for the light client (using the same node for simplicity)
	witnessProvider, err := cmthttp.New(chainID, fmt.Sprintf("http://%s", parsed.Host))
	if err != nil {
		return fmt.Errorf("failed to create witness provider: %w", err)
	}

	latestHeight, _, err := queryBlock(httpClient, parsed.Host, true)
	if err != nil {
		return fmt.Errorf("failed to query block: %w", err)
	}
	fmt.Printf("Latest block height: %d\n", latestHeight)
	var nextHeight = max(latestHeight-int64(epochLength), 1)
	// Poll for new blocks
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// Query next block
			height, appHash, err := queryBlock(httpClient, parsed.Host, true, nextHeight)
			if err != nil {
				fmt.Printf("Error querying block: %v\n", err)
				time.Sleep(time.Second / 10)
				continue
			}
			if height == 0 {
				fmt.Printf("block not found: %d\n", nextHeight)
				time.Sleep(time.Second / 10)
				continue
			}

			fmt.Printf("Current block: %d\n", height)

			// Check if this is the end of an epoch and we haven't attested to it yet
			if height > 1 && height%int64(epochLength) == 0 && height > lastAttested {
				fmt.Printf("submitting attestation for height: %d\n", height)
				// Submit attestation with the block's app hash
				err = submitAttestation(ctx, chainID, node, home, height, appHash, valAddr, verbose, senderKey, pv)
				if err != nil {
					return fmt.Errorf("submitting attestation: %w", err)
				}

				if lastAttested != 0 {
					fmt.Printf("End of epoch, verifying with light client %d\n", lastAttested)

					// Create trust options for the light client
					trustOptions := cmtlight.TrustOptions{
						Period: 24 * time.Hour, // Trusting period
						Height: lastAttested,
						Hash:   lastAppHash,
					}
					if err := trustOptions.ValidateBasic(); err != nil {
						return fmt.Errorf("validate trust options: %w", err)
					}
					// Create the light client
					lightClient, err := cmtlight.NewClient(
						ctx,
						chainID,
						trustOptions,
						primaryProvider,
						[]cmtprovider.Provider{witnessProvider},
						lightClientStore,
					)
					if err != nil {
						return fmt.Errorf("failed to create light client: %w", err)
					}

					if verbose {
						fmt.Println("Light client initialized successfully")
					}

					// Verify the previous epoch block with the light client.
					_, err = lightClient.VerifyLightBlockAtHeight(ctx, height-int64(epochLength), time.Now())
					if err != nil {
						fmt.Printf("Error verifying block at height %d: %v\n", height, err)
						time.Sleep(time.Second / 10)
						continue
					}
					fmt.Printf("Block at height %d verified successfully\n", height)
				}
				lastAttested = height
				lastAppHash = appHash
			}
			nextHeight++
			// Wait before next poll
			time.Sleep(time.Second / 10)
		}
	}
}

func queryBlock(httpClient *http.Client, host string, uncommitted bool, optHeight ...int64) (int64, []byte, error) {
	blockUrl := fmt.Sprintf("http://%s/block", host)
	if uncommitted {
		blockUrl += "_fake_attester"
	}
	if len(optHeight) != 0 {
		blockUrl += "?height=" + strconv.FormatInt(optHeight[0], 10)
	}
	resp, err := httpClient.Get(blockUrl)
	if err != nil {
		return 0, nil, err
	}

	var blockResponse struct {
		Result struct {
			Block struct {
				Header struct {
					Height  string `json:"height"`
					AppHash string `json:"app_hash"`
				} `json:"header"`
			} `json:"block"`
		} `json:"result"`
	}
	var buf bytes.Buffer
	if err := json.NewDecoder(io.TeeReader(resp.Body, &buf)).Decode(&blockResponse); err != nil {
		fmt.Printf("Error parsing response: %v: %s\n", err, buf.String())
		_ = resp.Body.Close()
		if resp.StatusCode == 404 {
			return 0, nil, nil
		}
		return 0, nil, fmt.Errorf("error parsing response: %v: %s", err, buf.String())
	}
	_ = resp.Body.Close()

	// Extract block height
	height, err := strconv.ParseInt(blockResponse.Result.Block.Header.Height, 10, 64)
	if err != nil {
		return 0, nil, err
	}
	appHash, err := hex.DecodeString(blockResponse.Result.Block.Header.AppHash)
	if err != nil {
		return 0, nil, fmt.Errorf("decoding app hash: %w", err)
	}
	return height, appHash, nil
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

var accSeq uint64 = 0

// broadcastTx executes a command to broadcast a transaction using the Cosmos SDK
func broadcastTx(ctx context.Context, chainID, nodeAddr string, msg proto.Message, privKey *secp256k1.PrivKey, verbose bool) (string, error) {
	// Get validator address from private key
	valAddr := sdk.ValAddress(privKey.PubKey().Address())

	if verbose {
		fmt.Printf("Broadcasting transaction for validator: %s\n", valAddr.String())
	}

	// Initialize the transaction config with the proper codec and sign modes
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	authtypes.RegisterInterfaces(interfaceRegistry)
	std.RegisterInterfaces(interfaceRegistry)
	protoCodec := codec.NewProtoCodec(interfaceRegistry)
	txConfig := authtx.NewTxConfig(protoCodec, authtx.DefaultSignModes)

	// Create proper transaction
	txBuilder := txConfig.NewTxBuilder()
	err := txBuilder.SetMsgs(msg)
	if err != nil {
		return "", fmt.Errorf("setting messages: %w", err)
	}

	txBuilder.SetGasLimit(200000)
	txBuilder.SetFeeAmount(sdk.NewCoins())
	txBuilder.SetMemo("")
	// Get account info from node
	clientCtx, err := createClientContext(nodeAddr, chainID, txConfig)
	if err != nil {
		return "", fmt.Errorf("creating client context: %w", err)
	}
	clientCtx = clientCtx.WithInterfaceRegistry(interfaceRegistry).WithCodec(protoCodec)
	addr := sdk.AccAddress(privKey.PubKey().Address())
	accountRetriever := authtypes.AccountRetriever{}
	account, err := accountRetriever.GetAccount(clientCtx, addr)
	if err != nil {
		return "", fmt.Errorf("getting account: %w", err)
	}
	fmt.Printf("+++ chainid: %s, GetAccountNumber: %d\n", chainID, account.GetAccountNumber())
	// Sign transaction using account sequence
	if accSeq == 0 {
		accSeq = account.GetSequence()
	}

	signerData := authsigning.SignerData{
		Address:       addr.String(),
		ChainID:       chainID,
		AccountNumber: account.GetAccountNumber(),
		Sequence:      accSeq,
		PubKey:        privKey.PubKey(),
	}

	// For SIGN_MODE_DIRECT, we need to set a nil signature first
	// to generate the correct sign bytes
	sigData := signing.SingleSignatureData{
		SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
		Signature: nil,
	}
	sig := signing.SignatureV2{
		PubKey:   privKey.PubKey(),
		Data:     &sigData,
		Sequence: accSeq,
	}

	err = txBuilder.SetSignatures(sig)
	if err != nil {
		return "", fmt.Errorf("setting nil signatures: %w", err)
	}

	// Now get the bytes to sign and create the real signature
	signBytes, err := authsigning.GetSignBytesAdapter(
		ctx,
		txConfig.SignModeHandler(),
		signing.SignMode_SIGN_MODE_DIRECT,
		signerData,
		txBuilder.GetTx(),
	)
	if err != nil {
		return "", fmt.Errorf("getting sign bytes: %w", err)
	}

	// Sign those bytes
	signature, err := privKey.Sign(signBytes)
	if err != nil {
		return "", fmt.Errorf("signing bytes: %w", err)
	}

	// Construct the SignatureV2 struct with the actual signature
	sigData = signing.SingleSignatureData{
		SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
		Signature: signature,
	}
	sig = signing.SignatureV2{
		PubKey:   privKey.PubKey(),
		Data:     &sigData,
		Sequence: accSeq,
	}

	err = txBuilder.SetSignatures(sig)
	if err != nil {
		return "", fmt.Errorf("setting signatures: %w", err)
	}

	txBytes, err := txConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return "", fmt.Errorf("encoding transaction: %w", err)
	}

	// Set broadcast mode to sync
	clientCtx = clientCtx.WithBroadcastMode("sync")

	// Broadcast the transaction using the cosmos-sdk library
	resp, err := clientCtx.BroadcastTx(txBytes)
	if err != nil {
		return "", fmt.Errorf("broadcasting transaction: %w", err)
	}
	accSeq++
	// Check if the transaction was successful
	if resp.Code != 0 {
		return "", fmt.Errorf("transaction failed with code %d: %s", resp.Code, resp.RawLog)
	}

	if verbose {
		fmt.Printf("Transaction successful with hash: %s\n", resp.TxHash)
	}

	return resp.TxHash, nil
}

// createClientContext creates a client.Context with the necessary fields for broadcasting transactions
func createClientContext(nodeAddr, chainID string, txConfig client.TxConfig) (client.Context, error) {
	// Create a CometRPC client
	rpcClient, err := client.NewClientFromNode(nodeAddr)
	if err != nil {
		return client.Context{}, fmt.Errorf("creating RPC client: %w", err)
	}

	// Create a client.Context with the necessary fields
	clientCtx := client.Context{
		Client:           rpcClient,
		ChainID:          chainID,
		TxConfig:         txConfig,
		BroadcastMode:    "sync",
		Output:           os.Stdout,
		AccountRetriever: authtypes.AccountRetriever{},
	}

	return clientCtx, nil
}

// createPrivateKeyFromMnemonic derives a private key from a mnemonic using the standard
// BIP44 HD path (m/44'/118'/0'/0/0)
func createPrivateKeyFromMnemonic(mnemonic string) (*secp256k1.PrivKey, error) {
	// Create master key from mnemonic
	derivedPriv, err := hd.Secp256k1.Derive()(
		mnemonic,
		"",
		hd.CreateHDPath(118, 0, 0).String(), // Cosmos HD path
	)
	if err != nil {
		return nil, fmt.Errorf("failed to derive private key: %w", err)
	}
	return &secp256k1.PrivKey{Key: derivedPriv}, nil
}

// submitAttestation creates and submits an attestation for a block using direct RPC
func submitAttestation(
	ctx context.Context,
	chainID, node, home string,
	height int64,
	appHash []byte,
	valAddr sdk.ValAddress,
	verbose bool,
	senderKey *secp256k1.PrivKey,
	pv *pvm.FilePV,
) error {
	// Create the vote
	vote := &cmtproto.Vote{
		Type:             cmtproto.PrecommitType,
		ValidatorAddress: pv.GetAddress(),
		Height:           height,
		Round:            0,
		BlockID:          cmtproto.BlockID{Hash: appHash, PartSetHeader: cmtproto.PartSetHeader{Total: 1, Hash: appHash}},
		Timestamp:        time.Now(),
	}
	var err error
	err = pv.SignVote(chainID, vote)
	if err != nil {
		return fmt.Errorf("sign vote: %w", err)
	}

	voteBytes, err := proto.Marshal(vote)
	if err != nil {
		return fmt.Errorf("marshal vote: %w", err)
	}

	msg := networktypes.NewMsgAttest(
		valAddr.String(),
		height,
		voteBytes,
	)

	txHash, err := broadcastTx(ctx, chainID, node, msg, senderKey, verbose)
	if err != nil {
		return fmt.Errorf("broadcast attest tx: %w", err)
	}

	fmt.Printf("Attestation submitted with hash: %s\n", txHash)
	return nil
}

// verify valset hash
// verify header version
// signed header
//
//	err = light.Verify(
//		&signedHeader,
//		tmTrustedValidators, tmSignedHeader, tmValidatorSet,
//		cs.TrustingPeriod, currentTimestamp, cs.MaxClockDrift, cs.TrustLevel.ToTendermint(),
//	)
//	if err != nil {
//		return errorsmod.Wrap(err, "failed to verify header")
//	}
//
