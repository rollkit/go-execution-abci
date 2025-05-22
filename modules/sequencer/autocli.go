package sequencer

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/rollkit/go-execution-abci/modules/sequencer/types"
)

// AutoCLIOptions implements the autocli.HasAutoCLIConfig interface.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: types.Query_serviceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "SequencersChanges",
					Use:       "sequencers-changes",
					Short:     "Shows the upcoming sequencer changes",
				},
				{
					RpcMethod: "Sequencers",
					Use:       "sequencers",
					Short:     "Shows the current sequencer",
				},
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service: types.Msg_serviceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "ChangeSequencers",
					Skip:      true, // skipped because authority gated
				},
			},
		},
	}
}
