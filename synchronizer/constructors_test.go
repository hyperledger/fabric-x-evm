/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package synchronizer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/synchronizer/hybridx"
)

// The SDK dials lazily, so nothing needs to listen here.
var unreachablePeer = network.PeerConf{Address: "127.0.0.1:1"}

type stubHeight struct{}

func (stubHeight) BlockNumber(context.Context) (uint64, error) { return 0, nil }

type stubSigner struct{}

func (stubSigner) Sign([]byte) ([]byte, error) { return []byte("sig"), nil }
func (stubSigner) Serialize() ([]byte, error)  { return []byte("creator"), nil }

func isHybrid(s Synchronizer) bool {
	_, ok := s.(*hybridx.HybridSynchronizer)
	return ok
}

// Fabric-X (also the default) gets the hybrid synchronizer, Fabric the plain delivery one.
func TestNew_RoutesByProtocol(t *testing.T) {
	for _, tc := range []struct {
		protocol   string
		wantHybrid bool
	}{
		{common.ProtocolFabricX, true},
		{"", true},
		{common.ProtocolFabric, false},
	} {
		t.Run("protocol "+tc.protocol, func(t *testing.T) {
			s, err := New(tc.protocol, stubHeight{}, "ch", "evm", unreachablePeer, stubSigner{}, nil, 0)
			require.NoError(t, err)
			require.NotNil(t, s)
			assert.Equal(t, tc.wantHybrid, isHybrid(s))
		})
	}
}

// NewDelivery is delivery-only so empty blocks reach the handlers.
func TestNewDelivery_NeverReturnsHybrid(t *testing.T) {
	for _, protocol := range []string{common.ProtocolFabricX, "", common.ProtocolFabric} {
		t.Run("protocol "+protocol, func(t *testing.T) {
			s, err := NewDelivery(protocol, stubHeight{}, "ch", "evm", unreachablePeer, stubSigner{}, nil)
			require.NoError(t, err)
			require.NotNil(t, s)
			assert.False(t, isHybrid(s))
		})
	}
}

type constructor func(protocol, namespace string, conf network.PeerConf, signer sdk.Signer) (Synchronizer, error)

func constructors() map[string]constructor {
	return map[string]constructor{
		"New": func(protocol, namespace string, conf network.PeerConf, signer sdk.Signer) (Synchronizer, error) {
			return New(protocol, stubHeight{}, "ch", namespace, conf, signer, nil, 0)
		},
		"NewDelivery": func(protocol, namespace string, conf network.PeerConf, signer sdk.Signer) (Synchronizer, error) {
			return NewDelivery(protocol, stubHeight{}, "ch", namespace, conf, signer, nil)
		},
	}
}

// Every way a synchronizer cannot be built is returned as an error.
func TestConstructors_RejectBadConfig(t *testing.T) {
	badTLS := network.PeerConf{Address: "127.0.0.1:1", TLS: network.TLSConfig{Mode: network.TLSModeTLS}}

	for name, build := range constructors() {
		for _, tc := range []struct {
			desc      string
			protocol  string
			namespace string
			conf      network.PeerConf
			signer    sdk.Signer
			wantErr   string
		}{
			{"unknown protocol", "bogus", "evm", unreachablePeer, stubSigner{}, "network.protocol must be"},
			{"empty namespace", common.ProtocolFabricX, "", unreachablePeer, stubSigner{}, "namespace is required"},
			{"fabric-x address without a port", common.ProtocolFabricX, "evm", network.PeerConf{Address: "no-port"}, stubSigner{}, "address must contain port"},
			{"fabric address without a port", common.ProtocolFabric, "evm", network.PeerConf{Address: "no-port"}, stubSigner{}, "address must contain port"},
			{"fabric-x invalid tls", common.ProtocolFabricX, "evm", badTLS, stubSigner{}, "invalid TLS config"},
			{"fabric invalid tls", common.ProtocolFabric, "evm", badTLS, stubSigner{}, "invalid TLS config"},
			{"fabric needs a signer", common.ProtocolFabric, "evm", unreachablePeer, nil, "signer is required"},
		} {
			t.Run(name+"/"+tc.desc, func(t *testing.T) {
				_, err := build(tc.protocol, tc.namespace, tc.conf, tc.signer)
				require.ErrorContains(t, err, tc.wantErr)
			})
		}
	}
}

// fakeSync is not ready until it has been polled readyAfter times.
type fakeSync struct {
	readyAfter int32
	polls      atomic.Int32
}

func (f *fakeSync) Start(context.Context) error { return nil }
func (f *fakeSync) Ready() error {
	if f.polls.Add(1) > f.readyAfter {
		return nil
	}
	return errors.New("not ready")
}

func TestWaitUntilSynced_ReturnsWhenReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &fakeSync{readyAfter: 3}
		start := time.Now()

		require.NoError(t, WaitUntilSynced(t.Context(), s, time.Minute))

		assert.Equal(t, int32(4), s.polls.Load())
		assert.Equal(t, 300*time.Millisecond, time.Since(start), "polls every 100ms")
	})
}

func TestWaitUntilSynced_ReadyImmediately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &fakeSync{}
		start := time.Now()

		require.NoError(t, WaitUntilSynced(t.Context(), s, time.Minute))

		assert.Equal(t, int32(1), s.polls.Load())
		assert.Zero(t, time.Since(start))
	})
}

func TestWaitUntilSynced_TimesOut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &fakeSync{readyAfter: 1 << 30}
		start := time.Now()

		err := WaitUntilSynced(t.Context(), s, time.Second)

		require.ErrorContains(t, err, "timeout waiting for sync")
		elapsed := time.Since(start)
		assert.GreaterOrEqual(t, elapsed, time.Second, "must not give up early")
		assert.LessOrEqual(t, elapsed, time.Second+100*time.Millisecond)
	})
}

func TestWaitUntilSynced_StopsWhenCallerCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &fakeSync{readyAfter: 1 << 30}
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(250*time.Millisecond, cancel)
		start := time.Now()

		err := WaitUntilSynced(ctx, s, time.Minute)

		require.Error(t, err)
		assert.Less(t, time.Since(start), time.Second, "cancelling must not wait out the timeout")
	})
}
