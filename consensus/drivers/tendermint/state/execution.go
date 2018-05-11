package state

import (
	"fmt"

	log "github.com/inconshreveable/log15"
	"gitlab.33.cn/chain33/chain33/consensus/drivers/tendermint/types"
)

//-----------------------------------------------------------------------------
// BlockExecutor handles block execution and state updates.
// It exposes ApplyBlock(), which validates & executes the block, updates state w/ ABCI responses,
// then commits and updates the mempool atomically, then saves state.

// BlockExecutor provides the context and accessories for properly executing a block.
type BlockExecutor struct {
	// save state, validators, consensus params, abci responses here
	db *CSStateDB

	// execute the app against this
	//proxyApp proxy.AppConnConsensus

	// events
	eventBus types.BlockEventPublisher

	// update these with block results after commit
	//mempool types.Mempool
	evpool types.EvidencePool

	logger log.Logger
}

// NewBlockExecutor returns a new BlockExecutor with a NopEventBus.
// Call SetEventBus to provide one.
func NewBlockExecutor(db *CSStateDB, logger log.Logger, evpool types.EvidencePool) *BlockExecutor {
	return &BlockExecutor{
		db: db,
		//proxyApp: proxyApp,
		eventBus: types.NopEventBus{},
		//mempool:  mempool,
		evpool: evpool,
		logger: logger,
	}
}

// SetEventBus - sets the event bus for publishing block related events.
// If not called, it defaults to types.NopEventBus.
func (blockExec *BlockExecutor) SetEventBus(eventBus types.BlockEventPublisher) {
	blockExec.eventBus = eventBus
}

// ValidateBlock validates the given block against the given state.
// If the block is invalid, it returns an error.
// Validation does not mutate state, but does require historical information from the stateDB,
// ie. to verify evidence from a validator at an old height.
func (blockExec *BlockExecutor) ValidateBlock(s State, block *types.Block) error {
	return validateBlock(blockExec.db, s, block)
}

// ApplyBlock validates the block against the state, executes it against the app,
// fires the relevant events, commits the app, and saves the new state and responses.
// It's the only function that needs to be called
// from outside this package to process and commit an entire block.
// It takes a blockID to avoid recomputing the parts hash.
func (blockExec *BlockExecutor) ApplyBlock(s State, blockID types.BlockID, block *types.Block) (State, error) {

	if err := blockExec.ValidateBlock(s, block); err != nil {
		return s, ErrInvalidBlock(err)
	}
	/*
		abciResponses, err := execBlockOnProxyApp(blockExec.logger, blockExec.proxyApp, block)
		if err != nil {
			return s, ErrProxyAppConn(err)
		}
	*/
	//fail.Fail() // XXX

	// save the results before we commit
	//saveABCIResponses(blockExec.db, block.Height, abciResponses)

	//fail.Fail() // XXX

	// update the state with the block and responses
	s, err := updateState(s, blockID, block)
	if err != nil {
		return s, fmt.Errorf("Commit failed for application: %v", err)
	}

	// lock mempool, commit state, update mempoool
	/*
		appHash, err := blockExec.Commit(block)
		if err != nil {
			return s, fmt.Errorf("Commit failed for application: %v", err)
		}
	*/
	//fail.Fail() // XXX

	// update the app hash and save the state
	//s.AppHash = appHash
	blockExec.db.SaveState(s)

	//fail.Fail() // XXX

	// Update evpool now that state is saved
	// TODO: handle the crash/recover scenario
	// ie. (may need to call Update for last block)
	blockExec.evpool.Update(block)

	// events are fired after everything else
	// NOTE: if we crash between Commit and Save, events wont be fired during replay
	fireEvents(blockExec.logger, blockExec.eventBus, block /*, abciResponses*/)

	return s, nil
}

// updateState returns a new State updated according to the header and responses.
func updateState(s State, blockID types.BlockID, block *types.Block) (State, error) {

	// copy the valset so we can apply changes from EndBlock
	// and update s.LastValidators and s.Validators
	prevValSet := s.Validators.Copy()
	nextValSet := prevValSet.Copy()

	// update the validator set with the latest abciResponses
	lastHeightValsChanged := s.LastHeightValidatorsChanged

	// Update validator accums and set state variables
	nextValSet.IncrementAccum(1)

	// update the params with the latest abciResponses
	nextParams := s.ConsensusParams
	lastHeightParamsChanged := s.LastHeightConsensusParamsChanged

	// NOTE: the AppHash has not been populated.
	// It will be filled on state.Save.
	return State{
		ChainID:                          s.ChainID,
		LastBlockHeight:                  block.Header.Height,
		LastBlockTotalTx:                 s.LastBlockTotalTx + block.Header.NumTxs,
		LastBlockID:                      blockID,
		LastBlockTime:                    block.Header.Time,
		Validators:                       nextValSet,
		LastValidators:                   s.Validators.Copy(),
		LastHeightValidatorsChanged:      lastHeightValsChanged,
		ConsensusParams:                  nextParams,
		LastHeightConsensusParamsChanged: lastHeightParamsChanged,
		LastResultsHash:                  nil,
		AppHash:                          nil,
	}, nil
}

// Fire NewBlock, NewBlockHeader.
// Fire TxEvent for every tx.
// NOTE: if Tendermint crashes before commit, some or all of these events may be published again.
func fireEvents(logger log.Logger, eventBus types.BlockEventPublisher, block *types.Block /*, abciResponses *ABCIResponses*/) {
	/*
		// NOTE: do we still need this buffer ?
		txEventBuffer := types.NewTxEventBuffer(eventBus, int(block.NumTxs))
		for i, tx := range block.Data.Txs {
			txEventBuffer.PublishEventTx(types.EventDataTx{types.TxResult{
				Height: block.Height,
				Index:  uint32(i),
				Tx:     tx,
				Result: *(abciResponses.DeliverTx[i]),
			}})
		}
	*/
	eventBus.PublishEventNewBlock(types.EventDataNewBlock{block})
	eventBus.PublishEventNewBlockHeader(types.EventDataNewBlockHeader{block.Header})
	/*
		err := txEventBuffer.Flush()
		if err != nil {
			logger.Error("Failed to flush event buffer", "err", err)
		}
	*/
}

//----------------------------------------------------------------------------------------------------
// Execute block without state. TODO: eliminate

// ExecCommitBlock executes and commits a block on the proxyApp without validating or mutating the state.
// It returns the application root hash (result of abci.Commit).
/*
func ExecCommitBlock(appConnConsensus proxy.AppConnConsensus, block *types.Block, logger log.Logger) ([]byte, error) {
	_, err := execBlockOnProxyApp(logger, appConnConsensus, block)
	if err != nil {
		logger.Error("Error executing block on proxy app", "height", block.Height, "err", err)
		return nil, err
	}
	// Commit block, get hash back
	res, err := appConnConsensus.CommitSync()
	if err != nil {
		logger.Error("Client error during proxyAppConn.CommitSync", "err", res)
		return nil, err
	}
	if res.IsErr() {
		logger.Error("Error in proxyAppConn.CommitSync", "err", res)
		return nil, res
	}
	if res.Log != "" {
		logger.Info("Commit.Log: " + res.Log)
	}
	return res.Data, nil
}
*/
