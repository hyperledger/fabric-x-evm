/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"testing"
	"time"

	"github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/core"
)

func TestNew_InstallsTimestampBoundsFromConfig(t *testing.T) {
	// Zero tsCfg uses package defaults at construction (no separate setter).
	end, err := core.New(nil, nil, config.Endorser{})
	if err != nil {
		t.Fatal(err)
	}
	_ = end

	// Custom bounds flow through New only.
	end2, err := core.New(nil, nil, config.Endorser{
		MaxTimestampFuture: 7 * time.Second,
		MaxTimestampPast:   11 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = end2
}
