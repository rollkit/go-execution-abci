package keeper

import (
	"context"
	"fmt"

	"github.com/rollkit/go-execution-abci/modules/network/types"
)

type contextKey struct{}

var headerSourceKey = contextKey{}

func WithHeaderSource(ctx context.Context, s types.HeaderSource) context.Context {
	return context.WithValue(ctx, headerSourceKey, s)
}

func HeaderSource(ctx context.Context) (types.HeaderSource, error) {
	headerSource, ok := ctx.Value(headerSourceKey).(types.HeaderSource)
	if !ok {
		return nil, fmt.Errorf("header source not found in context")
	}
	return headerSource, nil
}
