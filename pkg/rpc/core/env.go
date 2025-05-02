package core

import (
	cmtlog "github.com/cometbft/cometbft/libs/log"
	"github.com/rollkit/go-execution-abci/pkg/adapter"
)

var (
	// set by Node
	env *Environment
)

// SetEnvironment sets up the given Environment.
// It will race if multiple Node call SetEnvironment.
func SetEnvironment(e *Environment) {
	env = e
}

// Environment contains objects and interfaces used by the RPC. It is expected
// to be setup once during startup.
type Environment struct {
	Adapter *adapter.Adapter

	Logger cmtlog.Logger
}
