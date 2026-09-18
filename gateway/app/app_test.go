/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperledger/fabric-x-evm/common"
	econfig "github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
)

// testMSPDir writes a throwaway ECDSA key and self-signed certificate into an
// MSP-shaped directory (keystore/signcerts), enough for identity.SignerFromMSP
// to load — it only reads these files, it never verifies the certificate
// chain, so a self-signed leaf is fine for tests.
func testMSPDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	keystoreDir := filepath.Join(dir, "keystore")
	if err := os.MkdirAll(keystoreDir, 0o755); err != nil {
		t.Fatalf("mkdir keystore: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})
	if err := os.WriteFile(filepath.Join(keystoreDir, "priv_sk"), keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	signcertsDir := filepath.Join(dir, "signcerts")
	if err := os.MkdirAll(signcertsDir, 0o755); err != nil {
		t.Fatalf("mkdir signcerts: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(filepath.Join(signcertsDir, "cert.pem"), certPEM, 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	return dir
}

func endpoint(port int) common.ClientConfig {
	return common.ClientConfig{Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: port}}
}

// localEndorserConfig returns a minimal, always-required local endorser
// config backed by an in-memory KVS, with a real (throwaway) MSP identity.
func localEndorserConfig(t *testing.T) *econfig.Endorser {
	t.Helper()
	return &econfig.Endorser{
		Name:     "org1",
		Identity: common.IdentityConfig{MspID: "Org1MSP", MSPDir: testMSPDir(t)},
		Database: econfig.DB{Database: econfig.DBMemory},
	}
}

// remoteDialFailureConfig has one remote endorser whose TLS material doesn't
// exist, so dialing it fails before newApp ever reaches local endorser
// construction. cfg.Endorser is non-nil only to clear newApp's own
// required-ness check; it is never actually built.
func remoteDialFailureConfig() config.Config {
	return config.Config{
		Endorser: &econfig.Endorser{},
		Gateway: &config.Gateway{
			Endorsers: []common.ClientConfig{
				{
					Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 1234},
					TLS: common.TLSConfig{
						Mode:        "mtls",
						CertPath:    "/no/such/cert.pem",
						KeyPath:     "/no/such/key.pem",
						CACertPaths: []string{"/no/such/ca.pem"},
					},
				},
			},
		},
	}
}

// A gateway without an endorser section is rejected before any dialing or
// endorser construction is attempted.
func TestNewApp_RequiresEndorser(t *testing.T) {
	cfg := config.Config{Gateway: &config.Gateway{}}
	_, err := newApp(context.Background(), cfg, nil, false, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "endorser is required") {
		t.Errorf("error = %q, want it to mention endorser is required", err.Error())
	}
}

// A dial failure for a remote endorser must fail newApp outright, before ever
// building the local endorser (no MSP/DB setup should be attempted).
func TestNewApp_RemoteDialFailureReturnsError(t *testing.T) {
	_, err := newApp(context.Background(), remoteDialFailureConfig(), nil, false, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "dial endorser 0") {
		t.Errorf("error = %q, want it to mention the failing endorser index", err.Error())
	}
}

// If a later remote endorser fails to dial, connections already opened for
// earlier ones must be closed rather than leaked.
func TestNewApp_ClosesEarlierConnsOnLaterDialFailure(t *testing.T) {
	cfg := config.Config{
		Endorser: &econfig.Endorser{},
		Gateway: &config.Gateway{
			Endorsers: []common.ClientConfig{
				// Dials successfully: no TLS, no live server needed (lazy dial).
				endpoint(1),
				// Fails at credential loading, before any network I/O.
				{
					Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 2},
					TLS: common.TLSConfig{
						Mode:        "mtls",
						CertPath:    "/no/such/cert.pem",
						KeyPath:     "/no/such/key.pem",
						CACertPaths: []string{"/no/such/ca.pem"},
					},
				},
			},
		},
	}

	_, err := newApp(context.Background(), cfg, nil, false, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "dial endorser 1") {
		t.Errorf("error = %q, want it to mention the second (failing) endorser", err.Error())
	}
}

// fullAppConfig returns a config with a real local endorser and one remote
// endorser that dials successfully (lazy dial, no live server needed) — a
// complete config ready for buildApp.
func fullAppConfig(t *testing.T, dbConnString string) config.Config {
	t.Helper()
	return config.Config{
		Network:   common.Network{Protocol: "fabric-x", Channel: "mychannel", Namespace: "basic", NsVersion: "1.0", ChainID: 4011},
		Committer: endpoint(2),
		Gateway: &config.Gateway{
			Database:       config.DB{ConnString: dbConnString},
			Orderers:       []common.ClientConfig{endpoint(1)},
			Endorsers:      []common.ClientConfig{endpoint(3)},
			SubmitterCount: 1,
		},
		Endorser: localEndorserConfig(t),
	}
}

// The wiring underneath buildApp (orderer/committer/peer clients) dials
// lazily, the same way endorser/client.Dial does - none of it requires a
// reachable server to construct successfully. So a full App, local endorser
// plus one remote, can be built and torn down with nothing but local disk
// (the chain DB, the throwaway MSP dir) and syntactically valid addresses.
func TestNewApp_Success(t *testing.T) {
	dbPath := "file:" + filepath.Join(t.TempDir(), "gw.db")
	cfg := fullAppConfig(t, dbPath)

	app, err := newApp(context.Background(), cfg, nil, false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app.localEndorser == nil {
		t.Error("localEndorser = nil, want the embedded endorser")
	}
	if len(app.endorserConns) != 1 {
		t.Errorf("endorserConns = %d, want 1", len(app.endorserConns))
	}

	if err := app.Shutdown(); err != nil {
		t.Errorf("Shutdown: unexpected error: %v", err)
	}
}

// If local endorser construction fails after every remote dialed
// successfully, the already-open remote connections must still be closed
// rather than leaked.
func TestNewApp_LocalEndorserFailureClosesConns(t *testing.T) {
	cfg := fullAppConfig(t, "file:"+filepath.Join(t.TempDir(), "gw.db"))
	// An MSP dir with no keystore/signcerts: SignerFromMSP fails after every
	// remote has already dialed successfully.
	cfg.Endorser.Identity.MSPDir = t.TempDir()

	_, err := newApp(context.Background(), cfg, nil, false, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to create signer") {
		t.Errorf("error = %q, want it to come from signer creation", err.Error())
	}
}

// If buildApp fails after the local endorser and every remote are ready, the
// already-open remote connections must still be closed rather than leaked.
func TestNewApp_BuildAppFailureClosesConns(t *testing.T) {
	// A DB path inside a directory that doesn't exist: local endorser and
	// remote dial both succeed first, then core.NewChain fails to open the
	// database.
	cfg := fullAppConfig(t, "file:/no/such/directory/gw.db")

	_, err := newApp(context.Background(), cfg, nil, false, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to create chain") {
		t.Errorf("error = %q, want it to come from chain creation", err.Error())
	}
}

// Shutdown must not fail or block just because closing an endorser connection
// errors - it logs and continues, the same way it already treats gateway.Stop
// and chain.Close errors as non-fatal.
func TestApp_Shutdown_ToleratesEndorserCloseError(t *testing.T) {
	dbPath := "file:" + filepath.Join(t.TempDir(), "gw.db")
	cfg := fullAppConfig(t, dbPath)

	app, err := newApp(context.Background(), cfg, nil, false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Close it once ourselves so Shutdown's own Close() call errors (closing
	// an already-closed *grpc.ClientConn returns a non-nil error).
	if err := app.endorserConns[0].Close(); err != nil {
		t.Fatalf("pre-close: unexpected error: %v", err)
	}

	if err := app.Shutdown(); err != nil {
		t.Errorf("Shutdown: unexpected error: %v", err)
	}
}
