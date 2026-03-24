/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"github.com/hyperledger/fabric-lib-go/bccsp/sw"
	"github.com/hyperledger/fabric/msp"
)

func NewFabricDeserializer(mspDir, mspID string) (*FabricIdDeserialiser, error) {
	cryptoProvider, err := sw.NewDefaultSecurityLevelWithKeystore(sw.NewDummyKeyStore())
	if err != nil {
		return nil, err
	}

	MSP, err := msp.New(
		&msp.BCCSPNewOpts{NewBaseOpts: msp.NewBaseOpts{Version: msp.MSPv1_4_3}},
		cryptoProvider,
	)
	if err != nil {
		return nil, err
	}

	conf, err := msp.GetVerifyingMspConfig(mspDir, mspID, "bccsp")
	if err != nil {
		return nil, err
	}

	err = MSP.Setup(conf)
	if err != nil {
		return nil, err
	}
	return &FabricIdDeserialiser{MSP}, nil
}

type fabricIdentity struct {
	id msp.Identity
}

func (f *fabricIdentity) Validate() error {
	return f.id.Validate()
}

func (f *fabricIdentity) Verify(msg []byte, sig []byte) error {
	return f.id.Verify(msg, sig)
}

type FabricIdDeserialiser struct {
	msp msp.IdentityDeserializer
}

func (f *FabricIdDeserialiser) DeserializeIdentity(serializedIdentity []byte) (Identity, error) {
	identity, err := f.msp.DeserializeIdentity(serializedIdentity)
	if err != nil {
		return nil, err
	}

	return &fabricIdentity{identity}, nil
}

// Identity interface defining operations associated to a "certificate".
// That is, the public part of the identity could be thought to be a certificate,
// and offers solely signature verification capabilities. This is to be used
// at the peer side when verifying certificates that transactions are signed
// with, and verifying signatures that correspond to these certificates.
type Identity interface {
	// Validate uses the rules that govern this identity to validate it.
	// E.g., if it is a fabric TCert implemented as identity, validate
	// will check the TCert signature against the assumed root certificate
	// authority.
	Validate() error

	// Verify a signature over some message using this identity as reference
	Verify(msg []byte, sig []byte) error
}

// IdentityDeserializer is implemented by both MSPManger and MSP
type IdentityDeserializer interface {
	// DeserializeIdentity deserializes an identity.
	// Deserialization will fail if the identity is associated to
	// an msp that is different from this one that is performing
	// the deserialization.
	DeserializeIdentity(serializedIdentity []byte) (Identity, error)
}
