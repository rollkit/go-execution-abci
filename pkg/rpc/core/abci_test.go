package core

import (
	"context"
	"errors"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/libs/bytes"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	"github.com/stretchr/testify/assert"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
)

func TestABCIQuery(t *testing.T) {
	mockApp := new(MockApp)
	env = &Environment{
		Adapter: &adapter.Adapter{App: mockApp},
		Logger:  cmtlog.NewNopLogger(),
	}

	ctx := new(rpctypes.Context)
	path := "/test"
	data := bytes.HexBytes("testdata")
	height := int64(10)
	prove := true

	expectedResponse := &abci.ResponseQuery{
		Code:   0,
		Log:    "success",
		Value:  []byte("test_value"),
		Height: height,
	}

	mockApp.On("Query",
		context.Background(),
		&abci.RequestQuery{
			Data:   data,
			Path:   path,
			Height: height,
			Prove:  prove,
		},
	).Return(expectedResponse, nil)

	result, err := ABCIQuery(ctx, path, data, height, prove)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, *expectedResponse, result.Response)

	mockApp.AssertExpectations(t)
}

func TestABCIQuery_Error(t *testing.T) {
	mockApp := new(MockApp)
	env = &Environment{
		Adapter: &adapter.Adapter{App: mockApp},
		Logger:  cmtlog.NewNopLogger(),
	}

	ctx := new(rpctypes.Context)
	path := "/test/error"
	data := bytes.HexBytes("testdata_error")
	height := int64(12)
	prove := false

	expectedError := errors.New("mock query error")

	mockApp.On("Query",
		context.Background(),
		&abci.RequestQuery{
			Data:   data,
			Path:   path,
			Height: height,
			Prove:  prove,
		}).Return(nil, expectedError)

	result, err := ABCIQuery(ctx, path, data, height, prove)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedError, err)

	mockApp.AssertExpectations(t)
}

func TestABCIInfo(t *testing.T) {
	mockApp := new(MockApp) // Initialize mockApp for this test
	env = &Environment{
		Adapter: &adapter.Adapter{App: mockApp},
		Logger:  cmtlog.NewNopLogger(),
	}

	ctx := new(rpctypes.Context)
	expectedResponse := &abci.ResponseInfo{
		Data:             "app_info",
		Version:          "1.0.0",
		AppVersion:       1,
		LastBlockHeight:  100,
		LastBlockAppHash: []byte("hash"),
	}

	mockApp.On("Info", &abci.RequestInfo{}).Return(expectedResponse, nil)

	result, err := ABCIInfo(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, *expectedResponse, result.Response)

	mockApp.AssertExpectations(t)
}

func TestABCIInfo_Error(t *testing.T) {
	mockApp := new(MockApp)
	env = &Environment{
		Adapter: &adapter.Adapter{App: mockApp},
		Logger:  cmtlog.NewNopLogger(),
	}

	ctx := new(rpctypes.Context)
	expectedError := errors.New("mock info error")

	mockApp.On("Info", &abci.RequestInfo{}).Return(nil, expectedError)

	result, err := ABCIInfo(ctx)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedError, err)

	mockApp.AssertExpectations(t)
}
