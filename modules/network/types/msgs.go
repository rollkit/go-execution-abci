package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const TypeMsgAttest = "attest"
const TypeMsgJoinAttesterSet = "join_attester_set"
const TypeMsgLeaveAttesterSet = "leave_attester_set"
const TypeMsgUpdateParams = "update_params"

var _ sdk.Msg = &MsgAttest{}
var _ sdk.Msg = &MsgJoinAttesterSet{}
var _ sdk.Msg = &MsgLeaveAttesterSet{}
var _ sdk.Msg = &MsgUpdateParams{}

// NewMsgAttest creates a new MsgAttest instance
func NewMsgAttest(validator string, height int64, vote []byte) *MsgAttest {
	return &MsgAttest{
		Validator: validator,
		Height:    height,
		Vote:      vote,
	}
}

// NewMsgJoinAttesterSet creates a new MsgJoinAttesterSet instance
func NewMsgJoinAttesterSet(validator string) *MsgJoinAttesterSet {
	return &MsgJoinAttesterSet{
		Validator: validator,
	}
}

// NewMsgLeaveAttesterSet creates a new MsgLeaveAttesterSet instance
func NewMsgLeaveAttesterSet(validator string) *MsgLeaveAttesterSet {
	return &MsgLeaveAttesterSet{
		Validator: validator,
	}
}

// NewMsgUpdateParams creates a new MsgUpdateParams instance
func NewMsgUpdateParams(authority string, params Params) *MsgUpdateParams {
	return &MsgUpdateParams{
		Authority: authority,
		Params:    params,
	}
}
