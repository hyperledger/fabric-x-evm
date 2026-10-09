/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"context"
	"testing"
)

func TestSubscriptionConnKey_EmptyRemoteAddrReturnsNil(t *testing.T) {
	if got := subscriptionConnKey(context.Background()); got != nil {
		t.Fatalf("got %#v, want nil (global cap only; do not fall back to Notifier)", got)
	}
}
