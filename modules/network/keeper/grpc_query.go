package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/evstack/ev-abci/modules/network/types"
)

// queryServer is a wrapper around the network module's keeper providing gRPC query
// functionalities.
type queryServer struct {
	keeper Keeper
}

// NewQueryServer creates a new gRPC query server.
func NewQueryServer(k Keeper) types.QueryServer {
	return &queryServer{keeper: k}
}

var _ types.QueryServer = (*queryServer)(nil)

// Params queries the module parameters
func (q *queryServer) Params(c context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx := sdk.UnwrapSDKContext(c)

	return &types.QueryParamsResponse{Params: q.keeper.GetParams(ctx)}, nil
}

// AttestationBitmap queries the attestation bitmap for a specific height
func (q *queryServer) AttestationBitmap(c context.Context, req *types.QueryAttestationBitmapRequest) (*types.QueryAttestationBitmapResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)

	bitmapBytes, err := q.keeper.GetAttestationBitmap(ctx, req.Height)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, fmt.Errorf("get attestation bitmap: %w", err)
	}
	if bitmapBytes == nil {
		return nil, status.Error(codes.NotFound, "attestation bitmap not found for height")
	}

	// Reconstruct attestation info using keeper methods
	votedPower, err := q.keeper.CalculateVotedPower(ctx, bitmapBytes)
	if err != nil {
		return nil, err
	}
	totalPower, err := q.keeper.GetTotalPower(ctx)
	if err != nil {
		return nil, err
	}

	// Assuming IsSoftConfirmed is a method on the Keeper
	// If not, you might need to add it or compute it here using keeper.CheckQuorum
	softConfirmed, err := q.keeper.IsSoftConfirmed(ctx, req.Height)
	if err != nil {
		return nil, err
	}

	return &types.QueryAttestationBitmapResponse{
		Bitmap: &types.AttestationBitmap{
			Height:        req.Height,
			Bitmap:        bitmapBytes,
			VotedPower:    votedPower,
			TotalPower:    totalPower,
			SoftConfirmed: softConfirmed,
		},
	}, nil
}

// EpochInfo queries information about a specific epoch
func (q *queryServer) EpochInfo(c context.Context, req *types.QueryEpochInfoRequest) (*types.QueryEpochInfoResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)
	params := q.keeper.GetParams(ctx)

	startHeight := int64(req.Epoch * params.EpochLength)
	endHeight := int64((req.Epoch+1)*params.EpochLength - 1)

	epochBitmap := q.keeper.GetEpochBitmap(ctx, req.Epoch)
	if epochBitmap == nil {
		// Return info even if bitmap is not present, as per original logic
		return &types.QueryEpochInfoResponse{
			Epoch:                   req.Epoch,
			StartHeight:             startHeight,
			EndHeight:               endHeight,
			ParticipationBitmap:     []byte{},
			ActiveValidators:        0, // Consider calculating active validators even if bitmap is nil
			ParticipatingValidators: 0,
		}, nil
	}

	validators, err := q.keeper.stakingKeeper.GetLastValidators(ctx)
	if err != nil {
		return nil, err
	}
	activeValidators := uint64(0)
	for _, v := range validators {
		if v.IsBonded() {
			activeValidators++
		}
	}

	participatingValidators := uint64(q.keeper.bitmapHelper.PopCount(epochBitmap))

	return &types.QueryEpochInfoResponse{
		Epoch:                   req.Epoch,
		StartHeight:             startHeight,
		EndHeight:               endHeight,
		ParticipationBitmap:     epochBitmap,
		ActiveValidators:        activeValidators, // TODO (Alex): we need the historic validator set instead
		ParticipatingValidators: participatingValidators,
	}, nil
}

// ValidatorIndex queries the bitmap index for a validator
func (q *queryServer) ValidatorIndex(c context.Context, req *types.QueryValidatorIndexRequest) (*types.QueryValidatorIndexResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)
	// TODO (Alex): what is the use-case for this? The valset may change every epoch.
	// A request height and historic data could be useful with EpochInfo bitmap
	index, found := q.keeper.GetValidatorIndex(ctx, req.Address)
	if !found {
		return nil, status.Error(codes.NotFound, "validator index not found")
	}

	power, err := q.keeper.GetValidatorPower(ctx, index)
	if err != nil {
		return nil, fmt.Errorf("get validator power: %w", err)
	}

	return &types.QueryValidatorIndexResponse{
		Index: &types.ValidatorIndex{
			Address: req.Address,
			Index:   uint32(index),
			Power:   power,
		},
	}, nil
}

// SoftConfirmationStatus queries if a height is soft-confirmed
func (q *queryServer) SoftConfirmationStatus(c context.Context, req *types.QuerySoftConfirmationStatusRequest) (*types.QuerySoftConfirmationStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)
	isSoftConfirmed, err := q.keeper.IsSoftConfirmed(ctx, req.Height)
	if err != nil {
		return nil, err
	}
	bitmap, err := q.keeper.GetAttestationBitmap(ctx, req.Height)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, fmt.Errorf("get attestation bitmap: %w", err)
	}
	totalPower, err := q.keeper.GetTotalPower(ctx)
	if err != nil {
		return nil, err
	}

	var votedPower uint64
	if bitmap != nil {
		if votedPower, err = q.keeper.CalculateVotedPower(ctx, bitmap); err != nil {
			return nil, err
		}
	}

	return &types.QuerySoftConfirmationStatusResponse{
		IsSoftConfirmed: isSoftConfirmed,
		VotedPower:      votedPower,
		TotalPower:      totalPower,
		QuorumFraction:  q.keeper.GetParams(ctx).QuorumFraction,
	}, nil
}
