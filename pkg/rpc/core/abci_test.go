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

	"github.com/evstack/ev-abci/pkg/adapter"
)

func TestABCIQuery(t *testing.T) {
	cases := []struct {
		name             string
		path             string
		data             bytes.HexBytes
		height           int64
		prove            bool
		mockResponse     *abci.ResponseQuery
		mockError        error
		expectedResponse *abci.ResponseQuery
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name:   "successful query",
			path:   "/test",
			data:   bytes.HexBytes("testdata"),
			height: int64(10),
			prove:  true,
			mockResponse: &abci.ResponseQuery{
				Code:   0,
				Log:    "success",
				Value:  []byte("test_value"),
				Height: int64(10),
			},
			expectedResponse: &abci.ResponseQuery{
				Code:   0,
				Log:    "success",
				Value:  []byte("test_value"),
				Height: int64(10),
			},
			expectError: false,
		},
		{
			name:             "query error",
			path:             "/test/error",
			data:             bytes.HexBytes("testdata_error"),
			height:           int64(12),
			prove:            false,
			mockError:        errors.New("mock query error"),
			expectError:      true,
			expectedErrorMsg: "mock query error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockApp := new(MockApp)
			env = &Environment{
				Adapter: &adapter.Adapter{App: mockApp},
				Logger:  cmtlog.NewNopLogger(),
			}

			ctx := new(rpctypes.Context)

			mockApp.On("Query",
				context.Background(),
				&abci.RequestQuery{
					Data:   tc.data,
					Path:   tc.path,
					Height: tc.height,
					Prove:  tc.prove,
				},
			).Return(tc.mockResponse, tc.mockError)

			result, err := ABCIQuery(ctx, tc.path, tc.data, tc.height, tc.prove)

			if tc.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
				if tc.expectedErrorMsg != "" {
					assert.Contains(t, err.Error(), tc.expectedErrorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, *tc.expectedResponse, result.Response)
			}

			mockApp.AssertExpectations(t)
		})
	}
}

func TestABCIInfo(t *testing.T) {
	cases := []struct {
		name             string
		mockResponse     *abci.ResponseInfo
		mockError        error
		expectedResponse *abci.ResponseInfo
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name: "successful info",
			mockResponse: &abci.ResponseInfo{
				Data:             "app_info",
				Version:          "1.0.0",
				AppVersion:       1,
				LastBlockHeight:  100,
				LastBlockAppHash: []byte("hash"),
			},
			expectedResponse: &abci.ResponseInfo{
				Data:             "app_info",
				Version:          "1.0.0",
				AppVersion:       1,
				LastBlockHeight:  100,
				LastBlockAppHash: []byte("hash"),
			},
			expectError: false,
		},
		{
			name:             "info error",
			mockError:        errors.New("mock info error"),
			expectError:      true,
			expectedErrorMsg: "mock info error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockApp := new(MockApp)
			env = &Environment{
				Adapter: &adapter.Adapter{App: mockApp},
				Logger:  cmtlog.NewNopLogger(),
			}

			ctx := new(rpctypes.Context)

			mockApp.On("Info", &abci.RequestInfo{}).Return(tc.mockResponse, tc.mockError)

			result, err := ABCIInfo(ctx)

			if tc.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
				if tc.expectedErrorMsg != "" {
					assert.Contains(t, err.Error(), tc.expectedErrorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, *tc.expectedResponse, result.Response)
			}

			mockApp.AssertExpectations(t)
		})
	}
}
