package adapter

import (
	"context"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"

	"github.com/rollkit/go-execution-abci/modules/network/types"
)

// Option is a functional option for configuring the Adapter.
type Option func(*Adapter)

// WithMetrics sets custom metrics for the Adapter.
func WithMetrics(m *Metrics) Option {
	return func(a *Adapter) {
		a.metrics = m
	}
}

// WithBlockFilter sets a custom block publisher for the Adapter.
func WithBlockFilter(publisher BlockFilter) Option {
	return func(a *Adapter) {
		a.blockFilter = publisher
	}
}

// WithNetworkSoftConfirmationBlockFilter creates a BlockFilter that uses the network module's SoftConfirmationStatus.
func WithNetworkSoftConfirmationBlockFilter() Option {
	return func(a *Adapter) {
		a.blockFilter = &NetworkSoftConfirmationBlockFilter{
			app:    a.App,
			logger: a.Logger,
		}
	}
}

// NetworkSoftConfirmationBlockFilter is a BlockFilter implementation that uses the network module's SoftConfirmationStatus.
type NetworkSoftConfirmationBlockFilter struct {
	app    servertypes.ABCI
	logger log.Logger
}

// IsPublishable implements the BlockFilter interface.
func (f *NetworkSoftConfirmationBlockFilter) IsPublishable(ctx context.Context, height int64) bool {
	// First check if height is valid
	if height < 2 {
		return false
	}

	req := &types.QuerySoftConfirmationStatusRequest{
		Height: height,
	}
	reqData, err := req.Marshal()
	if err != nil {
		return false
	}

	// Query soft confirmation status
	softConfirmReq := &abci.RequestQuery{
		Path: "/rollkitsdk.network.v1.Query/SoftConfirmationStatus",
		Data: reqData,
	}

	softConfirmRes, err := f.app.Query(ctx, softConfirmReq)
	if err != nil || softConfirmRes.Code != 0 {
		var msg string
		if softConfirmRes != nil {
			msg = softConfirmRes.Log
		}
		f.logger.Error("query soft confirmation status", "height", height, "error", err, "log", msg)
		return false
	}

	softConfirmResp := &types.QuerySoftConfirmationStatusResponse{}
	if err := softConfirmResp.Unmarshal(softConfirmRes.Value); err != nil {
		f.logger.Error("unmarshal soft confirmation status response", "height", height, "error", err)
		return false
	}
	f.logger.Debug("soft confirmation status", "height", height, "is_soft_confirmed", softConfirmResp.IsSoftConfirmed)
	return softConfirmResp.IsSoftConfirmed
}
