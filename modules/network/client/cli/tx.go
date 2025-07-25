package cli

import (
	"fmt"
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/evstack/ev-abci/modules/network/types"
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		CmdAttest(),
		CmdJoinAttesterSet(),
		CmdLeaveAttesterSet(),
	)

	return cmd
}

// CmdAttest returns a CLI command for creating a MsgAttest transaction
func CmdAttest() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attest [height] [vote-base64]",
		Short: "Submit an attestation for a checkpoint height",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			height, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid height: %w", err)
			}

			vote := []byte(args[1])
			valAddress := sdk.ValAddress(clientCtx.GetFromAddress()).String()
			msg := types.NewMsgAttest(
				valAddress,
				height,
				vote,
			)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// CmdJoinAttesterSet returns a CLI command for creating a MsgJoinAttesterSet transaction
func CmdJoinAttesterSet() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "join-attester",
		Short: "Join the attester set as a validator",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			valAddress := sdk.ValAddress(clientCtx.GetFromAddress()).String()
			msg := types.NewMsgJoinAttesterSet(valAddress)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// CmdLeaveAttesterSet returns a CLI command for creating a MsgLeaveAttesterSet transaction
func CmdLeaveAttesterSet() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "leave-attester",
		Short: "Leave the attester set as a validator",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			valAddress := sdk.ValAddress(clientCtx.GetFromAddress()).String()
			msg := types.NewMsgLeaveAttesterSet(valAddress)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
