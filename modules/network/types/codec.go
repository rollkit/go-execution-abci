package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterCodec registers the necessary types and interfaces with the codec
func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgAttest{}, "network/Attest", nil)
	cdc.RegisterConcrete(&MsgJoinAttesterSet{}, "network/JoinAttesterSet", nil)
	cdc.RegisterConcrete(&MsgLeaveAttesterSet{}, "network/LeaveAttesterSet", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "network/UpdateParams", nil)
}

// RegisterInterfaces registers the module interface types
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgAttest{},
		&MsgJoinAttesterSet{},
		&MsgLeaveAttesterSet{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	Amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
)

func init() {
	RegisterCodec(Amino)
	Amino.Seal()
}
