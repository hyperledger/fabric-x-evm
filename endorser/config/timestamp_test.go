/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config_test

import (
	"testing"
	"time"

	"github.com/hyperledger/fabric-x-evm/endorser/config"
)

func TestTimestampSkewHelpers(t *testing.T) {
	var zero config.Endorser
	if got := zero.TimestampFutureSkew(); got != config.DefaultTimestampFutureSkew {
		t.Errorf("zero future = %v, want default %v", got, config.DefaultTimestampFutureSkew)
	}
	if got := zero.TimestampPastSkew(); got != config.DefaultTimestampPastSkew {
		t.Errorf("zero past = %v, want default %v", got, config.DefaultTimestampPastSkew)
	}

	neg := config.Endorser{MaxTimestampFuture: -1, MaxTimestampPast: -1}
	if got := neg.TimestampFutureSkew(); got != config.DefaultTimestampFutureSkew {
		t.Errorf("negative future = %v, want default", got)
	}
	if got := neg.TimestampPastSkew(); got != config.DefaultTimestampPastSkew {
		t.Errorf("negative past = %v, want default", got)
	}

	custom := config.Endorser{
		MaxTimestampFuture: 5 * time.Second,
		MaxTimestampPast:   30 * time.Second,
	}
	if got := custom.TimestampFutureSkew(); got != 5*time.Second {
		t.Errorf("custom future = %v, want 5s", got)
	}
	if got := custom.TimestampPastSkew(); got != 30*time.Second {
		t.Errorf("custom past = %v, want 30s", got)
	}
}
