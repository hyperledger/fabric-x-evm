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
}

func (s *stubService) Execute(_ context.Context, _ endorsement.Invocation, _ *types.Transaction) (*peer.ProposalResponse, error) {
	return s.execResp, s.execErr
}
func (s *stubService) Call(_ context.Context, _ *ethereum.CallMsg, _ *big.Int) ([]byte, error) {
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

func TestReadError_IsGRPCError(t *testing.T) {
	client := newTestClient(t, &stubService{readErr: errors.New("db down")})

	_, err := client.BalanceAt(context.Background(), &endorsementpb.BalanceRequest{Account: ethcommon.Address{}.Bytes()})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
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
