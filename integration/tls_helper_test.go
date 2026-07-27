/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mtlsCerts holds the file paths of a self-signed CA and a cert/key pair issued
// by it, for standing up a real mTLS gRPC connection in tests.
type mtlsCerts struct {
	caCertPath   string
	certPath     string
	keyPath      string
	certTemplate *x509.Certificate
}

// newSelfSignedCA generates a throwaway CA and writes its cert to dir.
func newSelfSignedCA(t *testing.T, dir string) (*x509.Certificate, *ecdsa.PrivateKey, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}

	path := filepath.Join(dir, "ca-cert.pem")
	writePEM(t, path, "CERTIFICATE", der)
	return cert, key, path
}

// issueLeafCert issues a cert/key pair signed by ca for 127.0.0.1, writing both
// to dir under the given prefix, and returns their paths.
func issueLeafCert(t *testing.T, dir, prefix string, ca *x509.Certificate, caKey *ecdsa.PrivateKey) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate %s key: %v", prefix, err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: prefix},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create %s cert: %v", prefix, err)
	}

	certPath = filepath.Join(dir, prefix+"-cert.pem")
	writePEM(t, certPath, "CERTIFICATE", der)

	keyPath = filepath.Join(dir, prefix+"-key.pem")
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal %s key: %v", prefix, err)
	}
	writePEM(t, keyPath, "EC PRIVATE KEY", keyDER)

	return certPath, keyPath
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}

// newMTLSFixture generates a CA plus a server and client cert issued by it, all
// under a fresh temp dir. It also returns the CA key so tests needing a second,
// untrusted identity can issue one from a different CA.
func newMTLSFixture(t *testing.T) (ca mtlsCerts, server mtlsCerts, client mtlsCerts) {
	t.Helper()
	dir := t.TempDir()

	caCert, caKey, caCertPath := newSelfSignedCA(t, dir)
	ca = mtlsCerts{caCertPath: caCertPath, certTemplate: caCert}

	serverCertPath, serverKeyPath := issueLeafCert(t, dir, "server", caCert, caKey)
	server = mtlsCerts{caCertPath: caCertPath, certPath: serverCertPath, keyPath: serverKeyPath}

	clientCertPath, clientKeyPath := issueLeafCert(t, dir, "client", caCert, caKey)
	client = mtlsCerts{caCertPath: caCertPath, certPath: clientCertPath, keyPath: clientKeyPath}

	return ca, server, client
}

// newUntrustedClientCert issues a client cert from a brand new, unrelated CA -
// valid on its own, but not chained to the server's trusted CA.
func newUntrustedClientCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	dir := t.TempDir()
	ca, caKey, _ := newSelfSignedCA(t, dir)
	return issueLeafCert(t, dir, "untrusted-client", ca, caKey)
}
