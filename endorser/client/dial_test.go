/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package client

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/hyperledger/fabric-x-evm/common"
)

// writeGarbage writes non-PEM content to a temp file and returns its path -
// the file exists and reads fine, but isn't valid certificate data.
func writeGarbage(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "garbage.pem")
	if err := os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDial_NoTLS_Succeeds(t *testing.T) {
	cfg := common.ClientConfig{Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 0}}

	c, err := Dial(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if c == nil {
		t.Fatal("Client = nil, want non-nil")
	}
}

func TestDial_MissingCertFile_ReturnsLoadError(t *testing.T) {
	cfg := common.ClientConfig{
		Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 0},
		TLS: common.TLSConfig{
			Mode:        "mtls",
			CertPath:    "/no/such/cert.pem",
			KeyPath:     "/no/such/key.pem",
			CACertPaths: []string{"/no/such/ca.pem"},
		},
	}

	_, err := Dial(cfg)
	if err == nil {
		t.Fatal("expected error for missing cert files")
	}
	if !strings.Contains(err.Error(), "load TLS credentials") {
		t.Errorf("error = %q, want it to mention loading TLS credentials", err.Error())
	}
}

// serveHostnameOnlyTLS starts a TLS gRPC server on 127.0.0.1 whose certificate
// is valid for hostname only - no IP SANs, like a real peer certificate. It
// registers no services, so reaching the handshake is what matters: a
// completed one surfaces as an "unimplemented" RPC error, a failed one as a
// certificate error. Returns the address and the CA file trusting it.
func serveHostnameOnlyTLS(t *testing.T, hostname string) (addr, caPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: hostname},
		DNSNames:              []string{hostname}, // deliberately no IPAddresses
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	caPath = filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{pair}})))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return lis.Addr().String(), caPath
}

// probe makes one RPC so the lazy connection actually performs its handshake,
// and returns the resulting error.
func probe(t *testing.T, c *Client) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.conn.Invoke(ctx, "/probe/Probe", &struct{}{}, &struct{}{})
}

func dialHostnameOnlyServer(t *testing.T, addr, caPath, serverName string) *Client {
	t.Helper()

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := net.LookupPort("tcp", portStr)
	if err != nil {
		t.Fatal(err)
	}

	c, err := Dial(common.ClientConfig{
		Endpoint: &common.Endpoint{Host: host, Port: port},
		TLS: common.TLSConfig{
			Mode:        "tls",
			ServerName:  serverName,
			CACertPaths: []string{caPath},
		},
	})
	if err != nil {
		t.Fatalf("unexpected dial error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// A server certificate issued for a hostname must be verified against that
// name, not against the address it is reached on, or every deployment dialed
// by IP fails the handshake.
func TestDial_ServerName_VerifiesAgainstConfiguredName(t *testing.T) {
	addr, caPath := serveHostnameOnlyTLS(t, "endorser.example.com")

	err := probe(t, dialHostnameOnlyServer(t, addr, caPath, "endorser.example.com"))
	if err == nil {
		t.Fatal("expected an unimplemented-method error, got nil")
	}
	if strings.Contains(err.Error(), "certificate") {
		t.Errorf("handshake failed despite matching server-name: %v", err)
	}
}

// Without server-name the certificate is verified against the dial address,
// which a hostname-only certificate cannot satisfy.
func TestDial_NoServerName_VerifiesAgainstAddress(t *testing.T) {
	addr, caPath := serveHostnameOnlyTLS(t, "endorser.example.com")

	err := probe(t, dialHostnameOnlyServer(t, addr, caPath, ""))
	if err == nil {
		t.Fatal("expected a certificate verification error, got nil")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("error = %q, want a certificate verification failure", err.Error())
	}
}

// The cert files exist and are readable (so NewClientTLSCredentials succeeds),
// but their content isn't valid certificate data, so building the transport
// credentials from the loaded bytes fails.
func TestDial_InvalidCertContent_ReturnsBuildError(t *testing.T) {
	garbage := writeGarbage(t)
	cfg := common.ClientConfig{
		Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 0},
		TLS: common.TLSConfig{
			Mode:        "mtls",
			CertPath:    garbage,
			KeyPath:     garbage,
			CACertPaths: []string{garbage},
		},
	}

	_, err := Dial(cfg)
	if err == nil {
		t.Fatal("expected error for invalid certificate content")
	}
	if !strings.Contains(err.Error(), "build transport credentials") {
		t.Errorf("error = %q, want it to mention building transport credentials", err.Error())
	}
}
