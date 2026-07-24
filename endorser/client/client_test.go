/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package client

import (
	"bytes"
	"context"
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

// mockServer is an EvmEndorsement server returning fixed responses (or gRPC
// errors), and capturing the last request so tests can assert marshaling.
type mockServer struct {
	endorsementpb.UnimplementedEvmEndorsementServer
	execResp *endorsementpb.ExecuteResponse
	execErr  error
	callResp *endorsementpb.CallResponse
	callErr  error
	balance  []byte
	storage  []byte
	code     []byte
	nonce    uint64
	readErr  error

	gotExec *endorsementpb.ExecuteRequest
	gotCall *endorsementpb.CallRequest
}

func (m *mockServer) Execute(_ context.Context, req *endorsementpb.ExecuteRequest) (*endorsementpb.ExecuteResponse, error) {
	m.gotExec = req
	return m.execResp, m.execErr
}
func (m *mockServer) Call(_ context.Context, req *endorsementpb.CallRequest) (*endorsementpb.CallResponse, error) {
	m.gotCall = req
	return m.callResp, m.callErr
}
func (m *mockServer) BalanceAt(_ context.Context, _ *endorsementpb.BalanceRequest) (*endorsementpb.BalanceResponse, error) {
	return &endorsementpb.BalanceResponse{Balance: m.balance}, m.readErr
}
func (m *mockServer) StorageAt(_ context.Context, _ *endorsementpb.StorageRequest) (*endorsementpb.StorageResponse, error) {
	return &endorsementpb.StorageResponse{Value: m.storage}, m.readErr
}
func (m *mockServer) CodeAt(_ context.Context, _ *endorsementpb.CodeRequest) (*endorsementpb.CodeResponse, error) {
	return &endorsementpb.CodeResponse{Code: m.code}, m.readErr
}
func (m *mockServer) NonceAt(_ context.Context, _ *endorsementpb.NonceRequest) (*endorsementpb.NonceResponse, error) {
	return &endorsementpb.NonceResponse{Nonce: m.nonce}, m.readErr
}

// newClient stands up mock over bufconn and returns a Client wired to it.
func newClient(t *testing.T, mock *mockServer) *Client {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	gs := grpc.NewServer()
	endorsementpb.RegisterEvmEndorsementServer(gs, mock)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c := New(conn)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestExecute_MapsResponseAndForwardsRequest(t *testing.T) {
	mock := &mockServer{execResp: &endorsementpb.ExecuteResponse{
		Status: common.StatusEVMRevert, Message: "reverted", Payload: []byte{0x01},
		EndorserId: []byte("id"), Signature: []byte("sig"),
	}}
	c := newClient(t, mock)

	tx := types.NewTx(&types.LegacyTx{Nonce: 0, Gas: 21000, GasPrice: big.NewInt(1)})
	inv := endorsement.Invocation{
		TxID: "tx1", Args: [][]byte{{0xfb}, {0xaa}},
		CCID: &peer.ChaincodeID{Name: "ns", Version: "1.0"}, ProposalHash: []byte("ph"),
	}

	resp, err := c.Execute(context.Background(), inv, tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetResponse().GetStatus() != common.StatusEVMRevert || resp.GetResponse().GetMessage() != "reverted" {
		t.Errorf("response = %+v", resp.GetResponse())
	}
	if !bytes.Equal(resp.GetEndorsement().GetEndorser(), []byte("id")) || !bytes.Equal(resp.GetEndorsement().GetSignature(), []byte("sig")) {
		t.Errorf("endorsement = %+v", resp.GetEndorsement())
	}

	raw, _ := tx.MarshalBinary()
	if !bytes.Equal(mock.gotExec.GetEthereumTx(), raw) || !bytes.Equal(mock.gotExec.GetProposalHash(), []byte("ph")) {
		t.Errorf("forwarded tx/proposal_hash wrong")
	}
	inW := mock.gotExec.GetInvocation()
	if inW.GetTxId() != "tx1" || inW.GetChaincodeName() != "ns" || inW.GetChaincodeVersion() != "1.0" || len(inW.GetArgs()) != 2 {
		t.Errorf("forwarded invocation = %+v", inW)
	}
}

func TestExecute_TransportErrorIsReturned(t *testing.T) {
	c := newClient(t, &mockServer{execErr: status.Error(codes.Unavailable, "down")})

	tx := types.NewTx(&types.LegacyTx{Nonce: 0, Gas: 21000, GasPrice: big.NewInt(1)})
	if _, err := c.Execute(context.Background(), endorsement.Invocation{}, tx); status.Code(err) != codes.Unavailable {
		t.Fatalf("code = %v, want Unavailable", status.Code(err))
	}
}

func TestCall_Success(t *testing.T) {
	c := newClient(t, &mockServer{callResp: &endorsementpb.CallResponse{ReturnData: []byte{0xde, 0xad}, Status: common.StatusOK}})

	out, err := c.Call(context.Background(), &ethereum.CallMsg{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out, []byte{0xde, 0xad}) {
		t.Errorf("return = %x", out)
	}
}

// A non-OK Call status comes back as a *common.CallError, not a gRPC error.
func TestCall_RevertBecomesCallError(t *testing.T) {
	data := []byte{0x08, 0xc3, 0x79, 0xa0}
	c := newClient(t, &mockServer{callResp: &endorsementpb.CallResponse{ReturnData: data, Status: common.StatusEVMRevert, Message: "reverted"}})

	out, err := c.Call(context.Background(), &ethereum.CallMsg{}, nil)
	ce, ok := err.(*common.CallError)
	if !ok {
		t.Fatalf("expected *common.CallError, got %T (%v)", err, err)
	}
	if !ce.Reverted() || ce.Message != "reverted" || !bytes.Equal(ce.Data, data) || !bytes.Equal(out, data) {
		t.Errorf("callError = %+v, out = %x", ce, out)
	}
}

func TestCall_TransportErrorIsNotCallError(t *testing.T) {
	c := newClient(t, &mockServer{callErr: status.Error(codes.Internal, "boom")})

	_, err := c.Call(context.Background(), &ethereum.CallMsg{}, nil)
	if _, ok := err.(*common.CallError); ok {
		t.Fatal("transport error must not be a CallError")
	}
	if status.Code(err) != codes.Internal {
		t.Errorf("code = %v, want Internal", status.Code(err))
	}
}

// Every optional call field and the block selector reach the wire.
func TestCall_ForwardsAllFields(t *testing.T) {
	mock := &mockServer{callResp: &endorsementpb.CallResponse{Status: common.StatusOK}}
	c := newClient(t, mock)

	to := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")
	msg := &ethereum.CallMsg{
		From: ethcommon.HexToAddress("0x1111111111111111111111111111111111111111"),
		To:   &to, Gas: 5000, GasPrice: big.NewInt(7), Value: big.NewInt(9), Data: []byte{0xbe, 0xef},
	}

	if _, err := c.Call(context.Background(), msg, big.NewInt(42)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := mock.gotCall
	if !bytes.Equal(got.GetFrom(), msg.From.Bytes()) || !bytes.Equal(got.GetTo(), to.Bytes()) {
		t.Errorf("from/to wrong")
	}
	if got.GetGas() != 5000 || new(big.Int).SetBytes(got.GetGasPrice()).Int64() != 7 || new(big.Int).SetBytes(got.GetValue()).Int64() != 9 {
		t.Errorf("gas/price/value wrong: %+v", got)
	}
	if !bytes.Equal(got.GetData(), []byte{0xbe, 0xef}) || got.GetBlockNumber() != 42 {
		t.Errorf("data/block wrong: %+v", got)
	}
}

func TestStateReaders_MapValues(t *testing.T) {
	c := newClient(t, &mockServer{balance: big.NewInt(42).Bytes(), storage: []byte{0x11}, code: []byte{0x22}, nonce: 7})
	ctx := context.Background()
	addr := ethcommon.Address{}

	if bal, _ := c.BalanceAt(ctx, addr, nil); bal.Cmp(big.NewInt(42)) != 0 {
		t.Errorf("balance = %v", bal)
	}
	if v, _ := c.StorageAt(ctx, addr, ethcommon.Hash{}, nil); !bytes.Equal(v, []byte{0x11}) {
		t.Errorf("storage = %x", v)
	}
	if v, _ := c.CodeAt(ctx, addr, nil); !bytes.Equal(v, []byte{0x22}) {
		t.Errorf("code = %x", v)
	}
	if n, _ := c.NonceAt(ctx, addr, nil); n != 7 {
		t.Errorf("nonce = %d", n)
	}
}

// A negative block number is an unresolved block-tag sentinel, not a real
// height, and must resolve to latest rather than converting to a nonsense uint64.
func TestBlockNumberProto_NegativeIsLatest(t *testing.T) {
	if got := blockNumberProto(big.NewInt(-1)); got != nil {
		t.Errorf("blockNumberProto(-1) = %v, want nil", got)
	}
}

func TestClose_NilConn(t *testing.T) {
	if err := (&Client{}).Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStateReaders_TransportErrors(t *testing.T) {
	c := newClient(t, &mockServer{readErr: status.Error(codes.Internal, "db down")})
	ctx := context.Background()
	addr := ethcommon.Address{}

	if _, err := c.BalanceAt(ctx, addr, nil); status.Code(err) != codes.Internal {
		t.Errorf("balance err = %v", err)
	}
	if _, err := c.StorageAt(ctx, addr, ethcommon.Hash{}, nil); status.Code(err) != codes.Internal {
		t.Errorf("storage err = %v", err)
	}
	if _, err := c.CodeAt(ctx, addr, nil); status.Code(err) != codes.Internal {
		t.Errorf("code err = %v", err)
	}
	if _, err := c.NonceAt(ctx, addr, nil); status.Code(err) != codes.Internal {
		t.Errorf("nonce err = %v", err)
	}
}
