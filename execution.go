package execution

import (
	"time"

	execution "github.com/rollkit/go-execution"
	rollkitTypes "github.com/rollkit/rollkit/types"
)

type ABCIExecutionClient struct {
	execute execution.Execute
}

func NewABCIExecutionClient(execute execution.Execute) *ABCIExecutionClient {
	return &ABCIExecutionClient{
		execute: execute,
	}
}

var _ execution.Execute = (*ABCIExecutionClient)(nil)

// InitChain initializes the blockchain with genesis information.
func (c *ABCIExecutionClient) InitChain(
	genesisTime time.Time,
	initialHeight uint64,
	chainID string,
) (rollkitTypes.Hash, uint64, error) {
	return c.execute.InitChain(genesisTime, initialHeight, chainID)
}

// GetTxs retrieves transactions from the transaction pool.
func (c *ABCIExecutionClient) GetTxs() ([]rollkitTypes.Tx, error) {
	return c.execute.GetTxs()
}

// ExecuteTxs executes the given transactions and returns the new state root and gas used.
func (c *ABCIExecutionClient) ExecuteTxs(
	txs []rollkitTypes.Tx,
	blockHeight uint64,
	timestamp time.Time,
	prevStateRoot rollkitTypes.Hash,
) (rollkitTypes.Hash, uint64, error) {
	return c.execute.ExecuteTxs(txs, blockHeight, timestamp, prevStateRoot)
}

// SetFinal marks a block at the given height as final.
func (c *ABCIExecutionClient) SetFinal(blockHeight uint64) error {
	return c.execute.SetFinal(blockHeight)
}
