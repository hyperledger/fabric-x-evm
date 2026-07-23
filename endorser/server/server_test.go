/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package server

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"net"
	"testing"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-committer/utils/serve"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/hyperledger/fabric-x-evm/api/endorsementpb"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// stubService is an api.Service returning fixed values, so the handlers can be
// exercised without a real endorser.
type stubService struct {
	execResp *peer.ProposalResponse
	execErr  error
	callOut  []byte
	callErr  error
	balance  *big.Int
	storage  []byte
	code     []byte
	nonce    uint64
	readErr  error

	// captured from the last call, so tests can assert what was forwarded
	gotInv   endorsement.Invocation
	gotMsg   *ethereum.CallMsg
	gotBlock *big.Int
}

func (s *stubService) Execute(_ context.Context, inv endorsement.Invocation, _ *types.Transaction) (*peer.ProposalResponse, error) {
	s.gotInv = inv
	return s.execResp, s.execErr
}
func (s *stubService) Call(_ context.Context, msg *ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	s.gotMsg, s.gotBlock = msg, blockNumber
	return s.callOut, s.callErr
}
func (s *stubService) BalanceAt(_ context.Context, _ ethcommon.Address, _ *big.Int) (*big.Int, error) {
	return s.balance, s.readErr
}
func (s *stubService) StorageAt(_ context.Context, _ ethcommon.Address, _ ethcommon.Hash, _ *big.Int) ([]byte, error) {
	return s.storage, s.readErr
}
func (s *stubService) CodeAt(_ context.Context, _ ethcommon.Address, _ *big.Int) ([]byte, error) {
	return s.code, s.readErr
}
func (s *stubService) NonceAt(_ context.Context, _ ethcommon.Address, _ *big.Int) (uint64, error) {
	return s.nonce, s.readErr
}

// newTestClient stands up the Server on an in-memory bufconn connection and
// returns a client wired to it.
func newTestClient(t *testing.T, svc *stubService) endorsementpb.EvmEndorsementClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	gs := grpc.NewServer()
	endorsementpb.RegisterEvmEndorsementServer(gs, New(svc))
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return endorsementpb.NewEvmEndorsementClient(conn)
}

func TestBalanceAt_Forwards(t *testing.T) {
	client := newTestClient(t, &stubService{balance: big.NewInt(42)})

	resp, err := client.BalanceAt(context.Background(), &endorsementpb.BalanceRequest{Account: ethcommon.Address{}.Bytes()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := new(big.Int).SetBytes(resp.GetBalance()); got.Cmp(big.NewInt(42)) != 0 {
		t.Errorf("balance = %v, want 42", got)
	}
}

func TestStorageAt_Forwards(t *testing.T) {
	want := []byte{0x11, 0x22}
	client := newTestClient(t, &stubService{storage: want})

	resp, err := client.StorageAt(context.Background(), &endorsementpb.StorageRequest{Account: ethcommon.Address{}.Bytes(), Key: ethcommon.Hash{}.Bytes()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(resp.GetValue(), want) {
		t.Errorf("value = %x, want %x", resp.GetValue(), want)
	}
}

func TestCodeAt_Forwards(t *testing.T) {
	want := []byte{0xfe, 0xed}
	client := newTestClient(t, &stubService{code: want})

	resp, err := client.CodeAt(context.Background(), &endorsementpb.CodeRequest{Account: ethcommon.Address{}.Bytes()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(resp.GetCode(), want) {
		t.Errorf("code = %x, want %x", resp.GetCode(), want)
	}
}

func TestNonceAt_Forwards(t *testing.T) {
	client := newTestClient(t, &stubService{nonce: 7})

	resp, err := client.NonceAt(context.Background(), &endorsementpb.NonceRequest{Account: ethcommon.Address{}.Bytes()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetNonce() != 7 {
		t.Errorf("nonce = %d, want 7", resp.GetNonce())
	}
}

// A failed state read is infrastructure, not an application outcome, so every
// reader surfaces it as a gRPC error.
func TestReadErrors_AreGRPCErrors(t *testing.T) {
	ctx := context.Background()
	addr := ethcommon.Address{}.Bytes()
	tests := []struct {
		name string
		call func(endorsementpb.EvmEndorsementClient) error
	}{
		{"balance", func(c endorsementpb.EvmEndorsementClient) error {
			_, err := c.BalanceAt(ctx, &endorsementpb.BalanceRequest{Account: addr})
			return err
		}},
		{"storage", func(c endorsementpb.EvmEndorsementClient) error {
			_, err := c.StorageAt(ctx, &endorsementpb.StorageRequest{Account: addr, Key: ethcommon.Hash{}.Bytes()})
			return err
		}},
		{"code", func(c endorsementpb.EvmEndorsementClient) error {
			_, err := c.CodeAt(ctx, &endorsementpb.CodeRequest{Account: addr})
			return err
		}},
		{"nonce", func(c endorsementpb.EvmEndorsementClient) error {
			_, err := c.NonceAt(ctx, &endorsementpb.NonceRequest{Account: addr})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, &stubService{readErr: errors.New("db down")})
			if got := status.Code(tt.call(client)); got != codes.Internal {
				t.Errorf("code = %v, want Internal", got)
			}
		})
	}
}

func TestCall_Success(t *testing.T) {
	want := []byte{0xde, 0xad}
	client := newTestClient(t, &stubService{callOut: want})

	resp, err := client.Call(context.Background(), &endorsementpb.CallRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(resp.GetReturnData(), want) {
		t.Errorf("returnData = %x, want %x", resp.GetReturnData(), want)
	}
	if resp.GetStatus() != common.StatusOK {
		t.Errorf("status = %d, want %d", resp.GetStatus(), common.StatusOK)
	}
}

// A revert is an application outcome: it comes back in the response status, not
// as a gRPC error, and carries the revert payload.
func TestCall_RevertIsInBand(t *testing.T) {
	data := []byte{0x08, 0xc3, 0x79, 0xa0}
	client := newTestClient(t, &stubService{
		callErr: &common.CallError{Status: common.StatusEVMRevert, Message: "reverted", Data: data},
	})

	resp, err := client.Call(context.Background(), &endorsementpb.CallRequest{})
	if err != nil {
		t.Fatalf("revert must not be a gRPC error, got: %v", err)
	}
	if resp.GetStatus() != common.StatusEVMRevert {
		t.Errorf("status = %d, want %d", resp.GetStatus(), common.StatusEVMRevert)
	}
	if resp.GetMessage() != "reverted" || !bytes.Equal(resp.GetReturnData(), data) {
		t.Errorf("message = %q, data = %x", resp.GetMessage(), resp.GetReturnData())
	}
}

// A non-CallError from Call is a transport fault and surfaces as a gRPC error.
func TestCall_TransportErrorIsGRPCError(t *testing.T) {
	client := newTestClient(t, &stubService{callErr: errors.New("connection reset")})

	_, err := client.Call(context.Background(), &endorsementpb.CallRequest{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
}

func TestExecute_MapsProposalResponse(t *testing.T) {
	pr := &peer.ProposalResponse{
		Response:    &peer.Response{Status: common.StatusEVMRevert, Message: "reverted", Payload: []byte{0x01}},
		Endorsement: &peer.Endorsement{Endorser: []byte("id"), Signature: []byte("sig")},
	}
	client := newTestClient(t, &stubService{execResp: pr})

	tx := types.NewTx(&types.LegacyTx{Nonce: 0, Gas: 21000, GasPrice: big.NewInt(1)})
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal tx: %v", err)
	}

	resp, err := client.Execute(context.Background(), &endorsementpb.ExecuteRequest{EthereumTx: raw})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetStatus() != common.StatusEVMRevert || resp.GetMessage() != "reverted" {
		t.Errorf("status = %d, message = %q", resp.GetStatus(), resp.GetMessage())
	}
	if !bytes.Equal(resp.GetEndorserId(), []byte("id")) || !bytes.Equal(resp.GetSignature(), []byte("sig")) {
		t.Errorf("endorserId = %x, signature = %x", resp.GetEndorserId(), resp.GetSignature())
	}
}

func TestExecute_InvalidTxIsInvalidArgument(t *testing.T) {
	client := newTestClient(t, &stubService{})

	_, err := client.Execute(context.Background(), &endorsementpb.ExecuteRequest{EthereumTx: []byte("not a tx")})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestExecute_TransportErrorIsGRPCError(t *testing.T) {
	client := newTestClient(t, &stubService{execErr: errors.New("engine down")})

	tx := types.NewTx(&types.LegacyTx{Nonce: 0, Gas: 21000, GasPrice: big.NewInt(1)})
	raw, _ := tx.MarshalBinary()

	_, err := client.Execute(context.Background(), &endorsementpb.ExecuteRequest{EthereumTx: raw})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
}

// The sender's invocation is mapped onto the invocation the builder consumes.
func TestExecute_ForwardsInvocation(t *testing.T) {
	svc := &stubService{execResp: &peer.ProposalResponse{Response: &peer.Response{Status: common.StatusOK}}}
	client := newTestClient(t, svc)

	tx := types.NewTx(&types.LegacyTx{Nonce: 0, Gas: 21000, GasPrice: big.NewInt(1)})
	raw, _ := tx.MarshalBinary()

	_, err := client.Execute(context.Background(), &endorsementpb.ExecuteRequest{
		EthereumTx:   raw,
		ProposalHash: []byte{0xaa},
		Invocation: &endorsementpb.Invocation{
			TxId:             "tx-1",
			Args:             [][]byte{{0xfb}, raw},
			ChaincodeName:    "evm",
			ChaincodeVersion: "1.0",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := svc.gotInv
	if got.TxID != "tx-1" {
		t.Errorf("TxID = %q, want tx-1", got.TxID)
	}
	if got.CCID.GetName() != "evm" || got.CCID.GetVersion() != "1.0" {
		t.Errorf("CCID = %v", got.CCID)
	}
	if !bytes.Equal(got.ProposalHash, []byte{0xaa}) {
		t.Errorf("ProposalHash = %x, want aa", got.ProposalHash)
	}
	if len(got.Args) != 2 {
		t.Errorf("Args = %d, want 2", len(got.Args))
	}
}

// A response carrying neither a Response nor an Endorsement maps to an empty result.
func TestExecute_EmptyProposalResponse(t *testing.T) {
	client := newTestClient(t, &stubService{execResp: &peer.ProposalResponse{}})

	tx := types.NewTx(&types.LegacyTx{Nonce: 0, Gas: 21000, GasPrice: big.NewInt(1)})
	raw, _ := tx.MarshalBinary()

	resp, err := client.Execute(context.Background(), &endorsementpb.ExecuteRequest{EthereumTx: raw})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetStatus() != 0 || len(resp.GetEndorserId()) != 0 || len(resp.GetSignature()) != 0 {
		t.Errorf("resp = %+v, want empty", resp)
	}
}

// Every optional call field and the block selector reach the engine.
func TestCall_ForwardsAllFields(t *testing.T) {
	svc := &stubService{callOut: []byte{0x01}}
	client := newTestClient(t, svc)

	from := ethcommon.HexToAddress("0x1111111111111111111111111111111111111111")
	to := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")
	blockNumber := uint64(7)

	if _, err := client.Call(context.Background(), &endorsementpb.CallRequest{
		From:        from.Bytes(),
		To:          to.Bytes(),
		Gas:         21000,
		GasPrice:    big.NewInt(5).Bytes(),
		Value:       big.NewInt(9).Bytes(),
		Data:        []byte{0xde, 0xad},
		BlockNumber: &blockNumber,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg := svc.gotMsg
	if msg.From != from {
		t.Errorf("From = %s, want %s", msg.From, from)
	}
	if msg.To == nil || *msg.To != to {
		t.Errorf("To = %v, want %s", msg.To, to)
	}
	if msg.Gas != 21000 || msg.GasPrice.Int64() != 5 || msg.Value.Int64() != 9 {
		t.Errorf("gas = %d, gasPrice = %v, value = %v", msg.Gas, msg.GasPrice, msg.Value)
	}
	if !bytes.Equal(msg.Data, []byte{0xde, 0xad}) {
		t.Errorf("Data = %x", msg.Data)
	}
	if svc.gotBlock == nil || svc.gotBlock.Uint64() != 7 {
		t.Errorf("blockNumber = %v, want 7", svc.gotBlock)
	}
}

// RegisterService exposes both the endorsement and health services.
func TestRegisterService(t *testing.T) {
	gs := grpc.NewServer()
	New(&stubService{}).RegisterService(serve.Servers{GRPC: gs})

	info := gs.GetServiceInfo()
	for _, name := range []string{"endorsementpb.EvmEndorsement", "grpc.health.v1.Health"} {
		if _, ok := info[name]; !ok {
			t.Errorf("%s not registered", name)
		}
	}
}
