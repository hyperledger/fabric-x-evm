/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/hyperledger/fabric-x-evm/endorser"
)

// EndorserWrapper wraps an Endorser and adds ethStateDB management for testing.
// It injects an EVMEngineWrapper into the endorser to provide DualStateDB support.
type EndorserWrapper struct {
	*endorser.Endorser
	engineWrapper *EVMEngineWrapper
}

// NewEndorserWrapper creates a new EndorserWrapper by injecting the engine wrapper into the endorser.
func NewEndorserWrapper(
	end *endorser.Endorser,
	engineWrapper *EVMEngineWrapper,
) *EndorserWrapper {
	// Inject the engine wrapper into the endorser
	end.Engine = engineWrapper

	return &EndorserWrapper{
		Endorser:      end,
		engineWrapper: engineWrapper,
	}
}

// SetEthStateDB sets the ethStateDB for testing purposes.
func (w *EndorserWrapper) SetEthStateDB(ethStateDB *state.StateDB) {
	w.engineWrapper.SetEthStateDB(ethStateDB)
}

// GetEthStateDB returns the ethStateDB used for testing.
func (w *EndorserWrapper) GetEthStateDB() *state.StateDB {
	return w.engineWrapper.GetEthStateDB()
}
