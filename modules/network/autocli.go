package network

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	networkv1 "github.com/rollkit/go-execution-abci/modules/network/types"
)

// AutoCLIOptions implements the autocli.HasAutoCLIConfig interface.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: networkv1.Query_serviceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Query the current network parameters",
				},
				{
					RpcMethod: "AttestationBitmap",
					Use:       "attestation [height]",
					Short:     "Query the attestation bitmap for a specific height",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "height"},
					},
				},
				{
					RpcMethod: "EpochInfo",
					Use:       "epoch [epoch-number]",
					Short:     "Query information about a specific epoch",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "epoch"},
					},
				},
				{
					RpcMethod: "ValidatorIndex",
					Use:       "validator-index [address]",
					Short:     "Query the bitmap index for a validator",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "address"},
					},
				},
				{
					RpcMethod: "SoftConfirmationStatus",
					Use:       "soft-confirmation [height]",
					Short:     "Query if a height is soft-confirmed",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "height"},
					},
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: networkv1.Msg_serviceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Attest",
					Use:       "attest [height] [vote-base64]",
					Short:     "Submit an attestation for a checkpoint height",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "height"},
						{ProtoField: "vote"},
					},
				},
				{
					RpcMethod: "JoinAttesterSet",
					Use:       "join-attester",
					Short:     "Join the attester set as a validator",
				},
				{
					RpcMethod: "LeaveAttesterSet",
					Use:       "leave-attester",
					Short:     "Leave the attester set as a validator",
				},
			},
		},
	}
}
