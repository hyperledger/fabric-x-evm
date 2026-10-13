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

	ethcommon "github.com/ethereum/go-ethereum/common"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

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
	// A failed handshake now waits out the deadline (WaitForReady), so keep it short.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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

// A call made while the endorser is down must wait for it to come back rather
// than fail fast with Unavailable, as after a container or pod restart.
func TestDial_CallWaitsForRestartedEndorser(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().(*net.TCPAddr)
	_ = lis.Close() // endorser is down

	c, err := Dial(common.ClientConfig{Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: addr.Port}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	done := make(chan error, 1)
	go func() {
		_, err := c.NonceAt(context.Background(), ethcommon.Address{}, nil)
		done <- err
	}()

	time.Sleep(time.Second) // let the first connection attempts fail
	lis, err = net.Listen("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	// The server registers no services, so reaching it yields Unimplemented.
	if err := <-done; status.Code(err) != codes.Unimplemented {
		t.Fatalf("call error = %v, want Unimplemented from the restarted server", err)
	}
}

// dialHealthServer serves a health service reporting status and dials it.
func dialHealthServer(t *testing.T, status healthpb.HealthCheckResponse_ServingStatus) *Client {
	t.Helper()
	c, _ := dialHealth(t, status)
	return c
}

func dialHealth(t *testing.T, status healthpb.HealthCheckResponse_ServingStatus) (*Client, *health.Server) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := health.NewServer()
	hs.SetServingStatus("", status)
	srv := grpc.NewServer()
	healthpb.RegisterHealthServer(srv, hs)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	c, err := Dial(common.ClientConfig{Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: lis.Addr().(*net.TCPAddr).Port}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, hs
}

func TestProbe_HealthyEndorser(t *testing.T) {
	if err := dialHealthServer(t, healthpb.HealthCheckResponse_SERVING).Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
}

// A reachable endorser that is not synced must not probe as healthy.
func TestProbe_NotServingIsAnError(t *testing.T) {
	err := dialHealthServer(t, healthpb.HealthCheckResponse_NOT_SERVING).Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "NOT_SERVING") {
		t.Fatalf("Probe error = %v, want NOT_SERVING", err)
	}
}

// A TLS misconfiguration must surface through Probe with its cause, not as a bare timeout.
func TestProbe_ReportsTLSFailure(t *testing.T) {
	addr, caPath := serveHostnameOnlyTLS(t, "endorser.example.com")
	c := dialHostnameOnlyServer(t, addr, caPath, "")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.Probe(ctx)
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("Probe error = %v, want it to name the certificate failure", err)
	}
}

// Monitor reports the initial state and then only transitions, so an endorser
// that becomes ready after startup is logged as such.
func TestMonitor_ReportsTransitions(t *testing.T) {
	c, hs := dialHealth(t, healthpb.HealthCheckResponse_NOT_SERVING)

	changes := make(chan error, 10)
	ctx := t.Context()
	go c.Monitor(ctx, 10*time.Millisecond, func(err error) { changes <- err })

	next := func() error {
		t.Helper()
		select {
		case err := <-changes:
			return err
		case <-time.After(2 * time.Second):
			t.Fatal("no health change reported")
			return nil
		}
	}

	if err := next(); err == nil {
		t.Fatal("first report = healthy, want NOT_SERVING")
	}
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	if err := next(); err != nil {
		t.Fatalf("report after recovery = %v, want healthy", err)
	}
	hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	if err := next(); err == nil {
		t.Fatal("report after degradation = healthy, want NOT_SERVING")
	}

	time.Sleep(50 * time.Millisecond) // several probes, no change
	if len(changes) != 0 {
		t.Fatalf("%d reports without a health change", len(changes))
	}
}
