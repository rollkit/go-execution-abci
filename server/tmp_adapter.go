package server

import (
	"context"

	goda "github.com/rollkit/go-da"
	coreda "github.com/rollkit/rollkit/core/da"
)

// TODO: temporary adapter until we remove go-da
type daAdapter struct {
	client goda.DA
}

var _ coreda.DA = &daAdapter{}

func (a *daAdapter) GasMultiplier(ctx context.Context) (float64, error) {
	return 1.0, nil
}

func (a *daAdapter) GasPrice(ctx context.Context) (float64, error) {
	return 1, nil
}

func (a *daAdapter) GetIDs(ctx context.Context, height uint64, namespace []byte) (*coreda.GetIDsResult, error) {
	result, err := a.client.GetIDs(ctx, height, goda.Namespace(namespace))
	if err != nil {
		return nil, err
	}

	response := &coreda.GetIDsResult{}

	if result != nil {
		response.IDs = result.IDs
	}

	return response, nil
}

func (a *daAdapter) Submit(ctx context.Context, blobs []coreda.Blob, gasPrice float64, namespace []byte, options []byte) ([]coreda.ID, error) {
	ids, err := a.client.Submit(ctx, blobs, gasPrice, goda.Namespace(namespace))
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (a *daAdapter) Validate(ctx context.Context, ids []coreda.ID, proofs []coreda.Proof, namespace []byte) ([]bool, error) {
	results, err := a.client.Validate(ctx, ids, proofs, goda.Namespace(namespace))
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (a *daAdapter) Get(ctx context.Context, ids []coreda.ID, namespace []byte) ([]coreda.Blob, error) {
	blobs, err := a.client.Get(ctx, ids, goda.Namespace(namespace))
	if err != nil {
		return nil, err
	}
	return blobs, nil
}

func (a *daAdapter) GetProofs(ctx context.Context, ids []coreda.ID, namespace []byte) ([]coreda.Proof, error) {
	proofs, err := a.client.GetProofs(ctx, ids, goda.Namespace(namespace))
	if err != nil {
		return nil, err
	}
	return proofs, nil
}

func (a *daAdapter) MaxBlobSize(ctx context.Context) (uint64, error) {
	return a.client.MaxBlobSize(ctx)
}

func (a *daAdapter) Commit(ctx context.Context, blobs []coreda.Blob, namespace []byte) ([]coreda.Commitment, error) {
	ids, err := a.client.Submit(ctx, blobs, 0, goda.Namespace(namespace))
	if err != nil {
		return nil, err
	}
	commitments := make([]coreda.Commitment, len(ids))
	for i, id := range ids {
		commitments[i] = coreda.Commitment(id)
	}
	return commitments, nil
}
