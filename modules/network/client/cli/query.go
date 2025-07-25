package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"

	"github.com/evstack/ev-abci/modules/network/types"
)

// GetQueryCmd returns the cli query commands for this module
func GetQueryCmd(queryRoute string) *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		CmdQueryParams(),
		CmdQueryAttestationBitmap(),
		CmdQueryEpochInfo(),
		CmdQueryValidatorIndex(),
		CmdQuerySoftConfirmationStatus(),
	)

	return cmd
}

// CmdQueryParams implements the params query command
func CmdQueryParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query the current network parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.Params(context.Background(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// CmdQueryAttestationBitmap queries the attestation bitmap for a height
func CmdQueryAttestationBitmap() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attestation [height]",
		Short: "Query the attestation bitmap for a specific height",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			height, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid height: %w", err)
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.AttestationBitmap(context.Background(), &types.QueryAttestationBitmapRequest{
				Height: height,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// CmdQueryEpochInfo queries information about an epoch
func CmdQueryEpochInfo() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "epoch [epoch-number]",
		Short: "Query information about a specific epoch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			epoch, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid epoch: %w", err)
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.EpochInfo(context.Background(), &types.QueryEpochInfoRequest{
				Epoch: epoch,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// CmdQueryValidatorIndex queries the bitmap index for a validator
func CmdQueryValidatorIndex() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validator-index [address]",
		Short: "Query the bitmap index for a validator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.ValidatorIndex(context.Background(), &types.QueryValidatorIndexRequest{
				Address: args[0],
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// CmdQuerySoftConfirmationStatus queries if a height is soft-confirmed
func CmdQuerySoftConfirmationStatus() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "soft-confirmation [height]",
		Short: "Query if a height is soft-confirmed",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			height, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid height: %w", err)
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.SoftConfirmationStatus(context.Background(), &types.QuerySoftConfirmationStatusRequest{
				Height: height,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
