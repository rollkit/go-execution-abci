package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
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

// Route returns the message route
func (msg *MsgAttest) Route() string {
	return RouterKey
}

// Type returns the message type
func (msg *MsgAttest) Type() string {
	return TypeMsgAttest
}

// GetSigners returns the expected signers
func (msg *MsgAttest) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.Validator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the bytes for signing
func (msg *MsgAttest) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic performs basic validation
func (msg *MsgAttest) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Validator)
	if err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid validator address (%s)", err)
	}

	if msg.Height <= 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "height must be positive")
	}

	if len(msg.Vote) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "vote cannot be empty")
	}

	return nil
}

// NewMsgJoinAttesterSet creates a new MsgJoinAttesterSet instance
func NewMsgJoinAttesterSet(validator string) *MsgJoinAttesterSet {
	return &MsgJoinAttesterSet{
		Validator: validator,
	}
}

// Route returns the message route
func (msg *MsgJoinAttesterSet) Route() string {
	return RouterKey
}

// Type returns the message type
func (msg *MsgJoinAttesterSet) Type() string {
	return TypeMsgJoinAttesterSet
}

// GetSigners returns the expected signers
func (msg *MsgJoinAttesterSet) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.Validator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the bytes for signing
func (msg *MsgJoinAttesterSet) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic performs basic validation
func (msg *MsgJoinAttesterSet) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Validator)
	if err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid validator address (%s)", err)
	}

	return nil
}

// NewMsgLeaveAttesterSet creates a new MsgLeaveAttesterSet instance
func NewMsgLeaveAttesterSet(validator string) *MsgLeaveAttesterSet {
	return &MsgLeaveAttesterSet{
		Validator: validator,
	}
}

// Route returns the message route
func (msg *MsgLeaveAttesterSet) Route() string {
	return RouterKey
}

// Type returns the message type
func (msg *MsgLeaveAttesterSet) Type() string {
	return TypeMsgLeaveAttesterSet
}

// GetSigners returns the expected signers
func (msg *MsgLeaveAttesterSet) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.Validator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the bytes for signing
func (msg *MsgLeaveAttesterSet) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic performs basic validation
func (msg *MsgLeaveAttesterSet) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Validator)
	if err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid validator address (%s)", err)
	}

	return nil
}

// NewMsgUpdateParams creates a new MsgUpdateParams instance
func NewMsgUpdateParams(authority string, params Params) *MsgUpdateParams {
	return &MsgUpdateParams{
		Authority: authority,
		Params:    params,
	}
}

// Route returns the message route
func (msg *MsgUpdateParams) Route() string {
	return RouterKey
}

// Type returns the message type
func (msg *MsgUpdateParams) Type() string {
	return TypeMsgUpdateParams
}

// GetSigners returns the expected signers
func (msg *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.Authority)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the bytes for signing
func (msg *MsgUpdateParams) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

// ValidateBasic performs basic validation
func (msg *MsgUpdateParams) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Authority)
	if err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid authority address (%s)", err)
	}

	return msg.Params.Validate()
}
