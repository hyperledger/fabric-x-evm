/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package domain

import "errors"

// ErrUnprotectedTx signals a transaction without EIP-155 replay protection.
var ErrUnprotectedTx = errors.New("only replay-protected (EIP-155) transactions allowed over RPC")

// ErrNonceLookup wraps a backend failure to fetch the sender's nonce, so the
// API layer can distinguish backend faults from tx-rejection causes.
var ErrNonceLookup = errors.New("look up nonce")
