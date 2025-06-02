package keeper_test

import (
	context "context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/rollkit/go-execution-abci/modules/rollkitmngr/keeper"
	"github.com/rollkit/go-execution-abci/modules/rollkitmngr/types"
)

// mockStakingKeeper is a minimal mock for stakingKeeper used in Attesters tests.
type mockStakingKeeper struct {
	vals []types.Attester
	err  error
}

func (m *mockStakingKeeper) GetLastValidators(ctx context.Context) ([]types.Attester, error) {
	return m.vals, m.err
}

// mockKeeper is a minimal mock for Keeper used in IsMigrating and Sequencer tests.
type mockKeeper struct {
	isMigratingFn func(ctx context.Context) (uint64, uint64, bool)
	sequencerFn   func(ctx context.Context) (types.Sequencer, error)
}

func (m *mockKeeper) IsMigrating(ctx context.Context) (uint64, uint64, bool) {
	return m.isMigratingFn(ctx)
}

func (m *mockKeeper) SequencerGet(ctx context.Context) (types.Sequencer, error) {
	return m.sequencerFn(ctx)
}

// QueryServerTestSuite is the test suite for grpc_query.go
// it uses testify suite for structure.
type QueryServerTestSuite struct {
	suite.Suite
	stakingKeeper *mockStakingKeeper
	keeper        *mockKeeper
	queryServer   types.QueryServer
}

func (s *QueryServerTestSuite) SetupTest() {
	s.stakingKeeper = &mockStakingKeeper{}
	s.keeper = &mockKeeper{}
	// use a struct embedding both mocks for the queryServer
	s.queryServer = keeper.NewQueryServer(s.keeper)
}

func TestQueryServerTestSuite(t *testing.T) {
	suite.Run(t, new(QueryServerTestSuite))
}

func (s *QueryServerTestSuite) TestAttesters_Success() {
	// attesters returns the current attesters
	s.stakingKeeper.vals = []types.Attester{{Name: "foo"}}
	s.stakingKeeper.err = nil
	resp, err := s.queryServer.Attesters(context.Background(), &types.QueryAttestersRequest{})
	require.NoError(s.T(), err)
	require.Len(s.T(), resp.Attesters, 1)
	require.Equal(s.T(), "foo", resp.Attesters[0].Name)
}

func (s *QueryServerTestSuite) TestAttesters_Error() {
	s.stakingKeeper.err = errors.New("fail")
	resp, err := s.queryServer.Attesters(context.Background(), &types.QueryAttestersRequest{})
	require.Error(s.T(), err)
	require.Nil(s.T(), resp)
}

func (s *QueryServerTestSuite) TestIsMigrating() {
	// isMigrating returns correct migration state
	s.keeper.isMigratingFn = func(ctx context.Context) (uint64, uint64, bool) {
		return 10, 20, true
	}
	resp, err := s.queryServer.IsMigrating(context.Background(), &types.QueryIsMigratingRequest{})
	require.NoError(s.T(), err)
	require.True(s.T(), resp.IsMigrating)
	require.Equal(s.T(), uint64(10), resp.StartBlockHeight)
	require.Equal(s.T(), uint64(20), resp.EndBlockHeight)
}

func (s *QueryServerTestSuite) TestSequencer_Migrating() {
	// sequencer returns error if migration is in progress
	s.keeper.isMigratingFn = func(ctx context.Context) (uint64, uint64, bool) {
		return 0, 0, true
	}
	resp, err := s.queryServer.Sequencer(context.Background(), &types.QuerySequencerRequest{})
	require.Error(s.T(), err)
	require.Nil(s.T(), resp)
}

func (s *QueryServerTestSuite) TestSequencer_Success() {
	// sequencer returns the current sequencer
	s.keeper.isMigratingFn = func(ctx context.Context) (uint64, uint64, bool) {
		return 0, 0, false
	}
	seq := types.Sequencer{Name: "foo"}
	s.keeper.sequencerFn = func(ctx context.Context) (types.Sequencer, error) {
		return seq, nil
	}
	resp, err := s.queryServer.Sequencer(context.Background(), &types.QuerySequencerRequest{})
	require.NoError(s.T(), err)
	require.Equal(s.T(), seq, resp.Sequencer)
}

func (s *QueryServerTestSuite) TestSequencer_Error() {
	// sequencer returns error if Sequencer.Get fails
	s.keeper.isMigratingFn = func(ctx context.Context) (uint64, uint64, bool) {
		return 0, 0, false
	}
	s.keeper.sequencerFn = func(ctx context.Context) (types.Sequencer, error) {
		return types.Sequencer{}, errors.New("fail")
	}
	resp, err := s.queryServer.Sequencer(context.Background(), &types.QuerySequencerRequest{})
	require.Error(s.T(), err)
	require.Nil(s.T(), resp)
}
