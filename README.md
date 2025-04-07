# go-execution-abci

An ABCI adapter for [Rollkit](https://github.com/rollkit/rollkit) that enables ABCI-compatible applications to be used with Rollkit's execution layer.

## Overview

`go-execution-abci` is a bridge between ABCI-compatible applications and Rollkit's execution layer. It implements the Rollkit execution interface and adapts it to the ABCI interface, allowing developers to use existing ABCI applications with Rollkit.

This adapter connects various components of the Rollkit ecosystem:
- Provides compatibility with the Cosmos SDK and CometBFT ABCI applications
- Implements transaction handling, state management, and blockchain operations
- Supports P2P communication for transaction gossip

## Architecture Overview

```mermaid
graph TD
    A[Rollkit Core] --> B[go-execution-abci Adapter]
    B --> C[ABCI Application]
    D[P2P Network] <--> B
    E[Mempool] <--> B
    F[RPC Server] --> B
    B --> G[Store]
    
    subgraph "Rollkit"
    A
    end
    
    subgraph "go-execution-abci"
    B
    D
    E
    F
    G
    end
    
    subgraph "Application"
    C
    end
    
    style B fill:#f9f,stroke:#333,stroke-width:2px
    style A fill:#bbf,stroke:#333,stroke-width:1px
    style C fill:#bfb,stroke:#333,stroke-width:1px
```

## Features

- **ABCI Compatibility**: Run any ABCI-compatible application with Rollkit
- **Transaction Management**: Handles transaction receipt, validation, and execution
- **State Management**: Manages blockchain state including validators and consensus parameters
- **P2P Communication**: Implements transaction gossip across the network
- **RPC Endpoints**: Provides compatible API endpoints for clients to interact with

## Installation

```bash
go get github.com/rollkit/go-execution-abci
```

## Dependencies

The project relies on several key dependencies:
- [Rollkit](https://github.com/rollkit/rollkit): For the core rollup functionality
- [Cosmos SDK](https://github.com/cosmos/cosmos-sdk): For the ABCI integration
- [CometBFT](https://github.com/cometbft/cometbft): For consensus-related types and functionality
- [libp2p](https://github.com/libp2p/go-libp2p): For peer-to-peer networking

## Usage

The adapter can be used to create a Rollkit node with an ABCI application:

```go
import (
    "github.com/rollkit/go-execution-abci/adapter"
    "github.com/rollkit/rollkit/pkg/store"
    rollkitp2p "github.com/rollkit/rollkit/pkg/p2p"
    // ... other imports
)

// Create a new ABCI executor with your ABCI application
executor := adapter.NewABCIExecutor(
    app,               // Your ABCI application
    store.New(db),     // Rollkit store
    p2pClient,         // Rollkit P2P client
    logger,            // Logger
    config,            // CometBFT config
    appGenesis,        // Application genesis
)

// Set up mempool for transaction handling
executor.SetMempool(mempool)

// Start the executor
err := executor.Start(context.Background())
if err != nil {
    // Handle error
}
```

## Transaction Flow

The following diagram illustrates how transactions flow through the system:

```mermaid
sequenceDiagram
    participant Client
    participant P2P Network
    participant Mempool
    participant Adapter
    participant ABCI App
    
    Client->>P2P Network: Submit Tx
    P2P Network->>Mempool: Gossip Tx
    Mempool->>Adapter: CheckTx
    Adapter->>ABCI App: CheckTx
    ABCI App-->>Adapter: Response
    
    Note over Adapter: Block Creation Time
    
    Adapter->>Adapter: GetTxs
    Adapter->>ABCI App: PrepareProposal
    ABCI App-->>Adapter: Txs
    
    Note over Adapter: Block Execution
    
    Adapter->>ABCI App: ProcessProposal
    ABCI App-->>Adapter: Accept/Reject
    Adapter->>ABCI App: FinalizeBlock
    ABCI App-->>Adapter: AppHash
    
    Adapter->>ABCI App: Commit
```

## Architecture

The adapter consists of several key components:

1. **Adapter**: The core component that implements the Rollkit executor interface and delegates calls to the ABCI application. The adapter manages the blockchain state and coordinates between different components.

2. **Mempool**: Handles pending transactions before they're included in blocks. The mempool is responsible for:
   - Validating transactions before acceptance
   - Storing pending transactions
   - Providing transactions for block creation

3. **P2P**: Manages transaction gossip between nodes, using libp2p for:
   - Transaction dissemination
   - Discovery of peers
   - Network communication

4. **RPC**: Provides API endpoints compatible with CometBFT RPC for interacting with the node, including:
   - Transaction submission
   - Query services
   - Block and state information

5. **Store**: Persists blockchain state and data, including:
   - Validator sets
   - Consensus parameters
   - Application state

```mermaid
classDiagram
    class Adapter {
        +App ABCI
        +Store Store
        +Mempool Mempool
        +P2PClient Client
        +TxGossiper Gossiper
        +InitChain()
        +ExecuteTxs()
        +GetTxs()
        +SetFinal()
    }
    
    class Executor {
        <<interface>>
        +InitChain()
        +ExecuteTxs() 
        +GetTxs()
        +SetFinal()
    }
    
    class ABCI {
        <<interface>>
        +InitChain()
        +PrepareProposal()
        +ProcessProposal() 
        +FinalizeBlock()
        +Commit()
    }
    
    class Mempool {
        +CheckTx()
        +ReapMaxBytesMaxGas()
    }
    
    class Store {
        +Get()
        +Set()
        +Height()
    }
    
    Executor <|.. Adapter : implements
    Adapter o-- ABCI : uses
    Adapter o-- Mempool : uses
    Adapter o-- Store : uses
```

## Development

### Prerequisites

- Go 1.23.3 or later
- Protocol Buffers compiler

### Building

```bash
# Install dependencies
make deps

# Run tests
make test

# Generate protocol buffer code
make proto-gen
```

### Project Structure

```
go-execution-abci/
├── adapter/      # Core adapter implementation
├── mempool/      # Transaction mempool
├── p2p/          # Peer-to-peer networking
├── proto/        # Protocol buffer definitions
├── rpc/          # RPC server implementation
└── server/       # Server startup and configuration
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

Apache License 2.0 