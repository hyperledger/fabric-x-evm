// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package contracts

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// UniswapV2PairMetaData contains all meta data concerning the UniswapV2Pair contract.
var UniswapV2PairMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount0\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount1\",\"type\":\"uint256\"}],\"name\":\"Mint\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount0Out\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount1Out\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"}],\"name\":\"Swap\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint112\",\"name\":\"reserve0\",\"type\":\"uint112\"},{\"indexed\":false,\"internalType\":\"uint112\",\"name\":\"reserve1\",\"type\":\"uint112\"}],\"name\":\"Sync\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"balanceOf\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getReserves\",\"outputs\":[{\"internalType\":\"uint112\",\"name\":\"\",\"type\":\"uint112\"},{\"internalType\":\"uint112\",\"name\":\"\",\"type\":\"uint112\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_token0\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_token1\",\"type\":\"address\"}],\"name\":\"initialize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"}],\"name\":\"mint\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"liquidity\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"amount0Out\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"amount1Out\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"}],\"name\":\"swap\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"token0\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"token1\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"totalSupply\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b5061179e806100206000396000f3fe608060405234801561001057600080fd5b50600436106100885760003560e01c8063485cc9551161005b578063485cc955146101045780636a6278421461012057806370a0823114610150578063d21220a71461018057610088565b8063022c0d9f1461008d5780630902f1ac146100a95780630dfe1681146100c857806318160ddd146100e6575b600080fd5b6100a760048036038101906100a29190610fbe565b61019e565b005b6100b16106cb565b6040516100bf92919061106f565b60405180910390f35b6100d0610710565b6040516100dd91906110a7565b60405180910390f35b6100ee610734565b6040516100fb91906110d1565b60405180910390f35b61011e600480360381019061011991906110ec565b61073a565b005b61013a6004803603810190610135919061112c565b610898565b60405161014791906110d1565b60405180910390f35b61016a6004803603810190610165919061112c565b610d29565b60405161017791906110d1565b60405180910390f35b610188610d41565b60405161019591906110a7565b60405180910390f35b60008511806101ad5750600084115b6101ec576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101e3906111b6565b60405180910390fd5b600260009054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff168510801561025257506002600e9054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff1684105b610291576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161028890611222565b60405180910390fd5b60008511156103785760008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663a9059cbb84876040518363ffffffff1660e01b81526004016102f5929190611242565b6020604051808303816000875af1158015610314573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061033891906112a3565b610377576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161036e9061131c565b60405180910390fd5b5b600084111561046157600160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663a9059cbb84866040518363ffffffff1660e01b81526004016103de929190611242565b6020604051808303816000875af11580156103fd573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061042191906112a3565b610460576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161045790611388565b60405180910390fd5b5b60008060009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff166370a08231306040518263ffffffff1660e01b81526004016104bd91906110a7565b602060405180830381865afa1580156104da573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906104fe91906113bd565b90506000600160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff166370a08231306040518263ffffffff1660e01b815260040161055d91906110a7565b602060405180830381865afa15801561057a573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061059e91906113bd565b90506002600e9054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff16600260009054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff166106049190611419565b81836106109190611419565b1015610651576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610648906114a7565b60405180910390fd5b61065b8282610d67565b8473ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167f2a9237ff5aa599ef4c5ee4b1142b53429d5755e2685fe6288b2e3320202115f589896040516106ba9291906114c7565b60405180910390a350505050505050565b600080600260009054906101000a90046dffffffffffffffffffffffffffff166002600e9054906101000a90046dffffffffffffffffffffffffffff16915091509091565b60008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b60035481565b6002601c9054906101000a900460ff161561078a576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016107819061153c565b60405180910390fd5b8073ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff16036107f8576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016107ef906115a8565b60405180910390fd5b816000806101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555080600160006101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555060016002601c6101000a81548160ff0219169083151502179055505050565b60008060008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff166370a08231306040518263ffffffff1660e01b81526004016108f491906110a7565b602060405180830381865afa158015610911573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061093591906113bd565b90506000600160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff166370a08231306040518263ffffffff1660e01b815260040161099491906110a7565b602060405180830381865afa1580156109b1573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906109d591906113bd565b9050600080600260009054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff16841115610a4957600260009054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff1684610a4691906115c8565b91505b6002600e9054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff16831115610ab8576002600e9054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff1683610ab591906115c8565b90505b6000821180610ac75750600081115b610b06576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610afd90611648565b60405180910390fd5b600060035403610b6e57610b248183610b1f9190611419565b610e46565b945060008511610b69576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610b60906116b4565b60405180910390fd5b610c57565b6000600260009054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff1660035484610bab9190611419565b610bb59190611703565b905060006002600e9054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff1660035484610bf49190611419565b610bfe9190611703565b9050808210610c0d5780610c0f565b815b965060008711610c54576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610c4b906116b4565b60405180910390fd5b50505b8460036000828254610c699190611734565b9250508190555084600460008873ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000206000828254610cbf9190611734565b92505081905550610cd08484610d67565b3373ffffffffffffffffffffffffffffffffffffffff167f4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f8383604051610d189291906114c7565b60405180910390a250505050919050565b60046020528060005260406000206000915090505481565b600160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b81600260006101000a8154816dffffffffffffffffffffffffffff02191690836dffffffffffffffffffffffffffff160217905550806002600e6101000a8154816dffffffffffffffffffffffffffff02191690836dffffffffffffffffffffffffffff1602179055507f1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1600260009054906101000a90046dffffffffffffffffffffffffffff166002600e9054906101000a90046dffffffffffffffffffffffffffff16604051610e3a92919061106f565b60405180910390a15050565b6000808203610e585760009050610eb6565b600082905082915060006002600183610e719190611734565b610e7b9190611703565b90505b82811015610eb3578092506002818284610e989190611703565b610ea29190611734565b610eac9190611703565b9050610e7e565b50505b919050565b600080fd5b600080fd5b6000819050919050565b610ed881610ec5565b8114610ee357600080fd5b50565b600081359050610ef581610ecf565b92915050565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b6000610f2682610efb565b9050919050565b610f3681610f1b565b8114610f4157600080fd5b50565b600081359050610f5381610f2d565b92915050565b600080fd5b600080fd5b600080fd5b60008083601f840112610f7e57610f7d610f59565b5b8235905067ffffffffffffffff811115610f9b57610f9a610f5e565b5b602083019150836001820283011115610fb757610fb6610f63565b5b9250929050565b600080600080600060808688031215610fda57610fd9610ebb565b5b6000610fe888828901610ee6565b9550506020610ff988828901610ee6565b945050604061100a88828901610f44565b935050606086013567ffffffffffffffff81111561102b5761102a610ec0565b5b61103788828901610f68565b92509250509295509295909350565b60006dffffffffffffffffffffffffffff82169050919050565b61106981611046565b82525050565b60006040820190506110846000830185611060565b6110916020830184611060565b9392505050565b6110a181610f1b565b82525050565b60006020820190506110bc6000830184611098565b92915050565b6110cb81610ec5565b82525050565b60006020820190506110e660008301846110c2565b92915050565b6000806040838503121561110357611102610ebb565b5b600061111185828601610f44565b925050602061112285828601610f44565b9150509250929050565b60006020828403121561114257611141610ebb565b5b600061115084828501610f44565b91505092915050565b600082825260208201905092915050565b7f494e53554646494349454e545f4f55545055545f414d4f554e54000000000000600082015250565b60006111a0601a83611159565b91506111ab8261116a565b602082019050919050565b600060208201905081810360008301526111cf81611193565b9050919050565b7f494e53554646494349454e545f4c495155494449545900000000000000000000600082015250565b600061120c601683611159565b9150611217826111d6565b602082019050919050565b6000602082019050818103600083015261123b816111ff565b9050919050565b60006040820190506112576000830185611098565b61126460208301846110c2565b9392505050565b60008115159050919050565b6112808161126b565b811461128b57600080fd5b50565b60008151905061129d81611277565b92915050565b6000602082840312156112b9576112b8610ebb565b5b60006112c78482850161128e565b91505092915050565b7f5452414e534645525f4641494c45445f544f4b454e3000000000000000000000600082015250565b6000611306601683611159565b9150611311826112d0565b602082019050919050565b60006020820190508181036000830152611335816112f9565b9050919050565b7f5452414e534645525f4641494c45445f544f4b454e3100000000000000000000600082015250565b6000611372601683611159565b915061137d8261133c565b602082019050919050565b600060208201905081810360008301526113a181611365565b9050919050565b6000815190506113b781610ecf565b92915050565b6000602082840312156113d3576113d2610ebb565b5b60006113e1848285016113a8565b91505092915050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b600061142482610ec5565b915061142f83610ec5565b925082820261143d81610ec5565b91508282048414831517611454576114536113ea565b5b5092915050565b7f4b00000000000000000000000000000000000000000000000000000000000000600082015250565b6000611491600183611159565b915061149c8261145b565b602082019050919050565b600060208201905081810360008301526114c081611484565b9050919050565b60006040820190506114dc60008301856110c2565b6114e960208301846110c2565b9392505050565b7f414c52454144595f494e495449414c495a454400000000000000000000000000600082015250565b6000611526601383611159565b9150611531826114f0565b602082019050919050565b6000602082019050818103600083015261155581611519565b9050919050565b7f4944454e544943414c5f41444452455353455300000000000000000000000000600082015250565b6000611592601383611159565b915061159d8261155c565b602082019050919050565b600060208201905081810360008301526115c181611585565b9050919050565b60006115d382610ec5565b91506115de83610ec5565b92508282039050818111156115f6576115f56113ea565b5b92915050565b7f4e4f5f4c49515549444954595f41444445440000000000000000000000000000600082015250565b6000611632601283611159565b915061163d826115fc565b602082019050919050565b6000602082019050818103600083015261166181611625565b9050919050565b7f494e53554646494349454e545f4c49515549444954595f4d494e544544000000600082015250565b600061169e601d83611159565b91506116a982611668565b602082019050919050565b600060208201905081810360008301526116cd81611691565b9050919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601260045260246000fd5b600061170e82610ec5565b915061171983610ec5565b925082611729576117286116d4565b5b828204905092915050565b600061173f82610ec5565b915061174a83610ec5565b9250828201905080821115611762576117616113ea565b5b9291505056fea2646970667358221220b99a911e223eed73f0a0895ab485ffc5a0f9e87996f3afd72ca09650cbf10c9264736f6c63430008140033",
}

// UniswapV2PairABI is the input ABI used to generate the binding from.
// Deprecated: Use UniswapV2PairMetaData.ABI instead.
var UniswapV2PairABI = UniswapV2PairMetaData.ABI

// UniswapV2PairBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use UniswapV2PairMetaData.Bin instead.
var UniswapV2PairBin = UniswapV2PairMetaData.Bin

// DeployUniswapV2Pair deploys a new Ethereum contract, binding an instance of UniswapV2Pair to it.
func DeployUniswapV2Pair(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *UniswapV2Pair, error) {
	parsed, err := UniswapV2PairMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(UniswapV2PairBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &UniswapV2Pair{UniswapV2PairCaller: UniswapV2PairCaller{contract: contract}, UniswapV2PairTransactor: UniswapV2PairTransactor{contract: contract}, UniswapV2PairFilterer: UniswapV2PairFilterer{contract: contract}}, nil
}

// UniswapV2Pair is an auto generated Go binding around an Ethereum contract.
type UniswapV2Pair struct {
	UniswapV2PairCaller     // Read-only binding to the contract
	UniswapV2PairTransactor // Write-only binding to the contract
	UniswapV2PairFilterer   // Log filterer for contract events
}

// UniswapV2PairCaller is an auto generated read-only Go binding around an Ethereum contract.
type UniswapV2PairCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// UniswapV2PairTransactor is an auto generated write-only Go binding around an Ethereum contract.
type UniswapV2PairTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// UniswapV2PairFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type UniswapV2PairFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// UniswapV2PairSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type UniswapV2PairSession struct {
	Contract     *UniswapV2Pair    // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// UniswapV2PairCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type UniswapV2PairCallerSession struct {
	Contract *UniswapV2PairCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts        // Call options to use throughout this session
}

// UniswapV2PairTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type UniswapV2PairTransactorSession struct {
	Contract     *UniswapV2PairTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts        // Transaction auth options to use throughout this session
}

// UniswapV2PairRaw is an auto generated low-level Go binding around an Ethereum contract.
type UniswapV2PairRaw struct {
	Contract *UniswapV2Pair // Generic contract binding to access the raw methods on
}

// UniswapV2PairCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type UniswapV2PairCallerRaw struct {
	Contract *UniswapV2PairCaller // Generic read-only contract binding to access the raw methods on
}

// UniswapV2PairTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type UniswapV2PairTransactorRaw struct {
	Contract *UniswapV2PairTransactor // Generic write-only contract binding to access the raw methods on
}

// NewUniswapV2Pair creates a new instance of UniswapV2Pair, bound to a specific deployed contract.
func NewUniswapV2Pair(address common.Address, backend bind.ContractBackend) (*UniswapV2Pair, error) {
	contract, err := bindUniswapV2Pair(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &UniswapV2Pair{UniswapV2PairCaller: UniswapV2PairCaller{contract: contract}, UniswapV2PairTransactor: UniswapV2PairTransactor{contract: contract}, UniswapV2PairFilterer: UniswapV2PairFilterer{contract: contract}}, nil
}

// NewUniswapV2PairCaller creates a new read-only instance of UniswapV2Pair, bound to a specific deployed contract.
func NewUniswapV2PairCaller(address common.Address, caller bind.ContractCaller) (*UniswapV2PairCaller, error) {
	contract, err := bindUniswapV2Pair(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &UniswapV2PairCaller{contract: contract}, nil
}

// NewUniswapV2PairTransactor creates a new write-only instance of UniswapV2Pair, bound to a specific deployed contract.
func NewUniswapV2PairTransactor(address common.Address, transactor bind.ContractTransactor) (*UniswapV2PairTransactor, error) {
	contract, err := bindUniswapV2Pair(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &UniswapV2PairTransactor{contract: contract}, nil
}

// NewUniswapV2PairFilterer creates a new log filterer instance of UniswapV2Pair, bound to a specific deployed contract.
func NewUniswapV2PairFilterer(address common.Address, filterer bind.ContractFilterer) (*UniswapV2PairFilterer, error) {
	contract, err := bindUniswapV2Pair(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &UniswapV2PairFilterer{contract: contract}, nil
}

// bindUniswapV2Pair binds a generic wrapper to an already deployed contract.
func bindUniswapV2Pair(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := UniswapV2PairMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_UniswapV2Pair *UniswapV2PairRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _UniswapV2Pair.Contract.UniswapV2PairCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_UniswapV2Pair *UniswapV2PairRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _UniswapV2Pair.Contract.UniswapV2PairTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_UniswapV2Pair *UniswapV2PairRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _UniswapV2Pair.Contract.UniswapV2PairTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_UniswapV2Pair *UniswapV2PairCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _UniswapV2Pair.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_UniswapV2Pair *UniswapV2PairTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _UniswapV2Pair.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_UniswapV2Pair *UniswapV2PairTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _UniswapV2Pair.Contract.contract.Transact(opts, method, params...)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address ) view returns(uint256)
func (_UniswapV2Pair *UniswapV2PairCaller) BalanceOf(opts *bind.CallOpts, arg0 common.Address) (*big.Int, error) {
	var out []interface{}
	err := _UniswapV2Pair.contract.Call(opts, &out, "balanceOf", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address ) view returns(uint256)
func (_UniswapV2Pair *UniswapV2PairSession) BalanceOf(arg0 common.Address) (*big.Int, error) {
	return _UniswapV2Pair.Contract.BalanceOf(&_UniswapV2Pair.CallOpts, arg0)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address ) view returns(uint256)
func (_UniswapV2Pair *UniswapV2PairCallerSession) BalanceOf(arg0 common.Address) (*big.Int, error) {
	return _UniswapV2Pair.Contract.BalanceOf(&_UniswapV2Pair.CallOpts, arg0)
}

// GetReserves is a free data retrieval call binding the contract method 0x0902f1ac.
//
// Solidity: function getReserves() view returns(uint112, uint112)
func (_UniswapV2Pair *UniswapV2PairCaller) GetReserves(opts *bind.CallOpts) (*big.Int, *big.Int, error) {
	var out []interface{}
	err := _UniswapV2Pair.contract.Call(opts, &out, "getReserves")

	if err != nil {
		return *new(*big.Int), *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	out1 := *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)

	return out0, out1, err

}

// GetReserves is a free data retrieval call binding the contract method 0x0902f1ac.
//
// Solidity: function getReserves() view returns(uint112, uint112)
func (_UniswapV2Pair *UniswapV2PairSession) GetReserves() (*big.Int, *big.Int, error) {
	return _UniswapV2Pair.Contract.GetReserves(&_UniswapV2Pair.CallOpts)
}

// GetReserves is a free data retrieval call binding the contract method 0x0902f1ac.
//
// Solidity: function getReserves() view returns(uint112, uint112)
func (_UniswapV2Pair *UniswapV2PairCallerSession) GetReserves() (*big.Int, *big.Int, error) {
	return _UniswapV2Pair.Contract.GetReserves(&_UniswapV2Pair.CallOpts)
}

// Token0 is a free data retrieval call binding the contract method 0x0dfe1681.
//
// Solidity: function token0() view returns(address)
func (_UniswapV2Pair *UniswapV2PairCaller) Token0(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _UniswapV2Pair.contract.Call(opts, &out, "token0")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Token0 is a free data retrieval call binding the contract method 0x0dfe1681.
//
// Solidity: function token0() view returns(address)
func (_UniswapV2Pair *UniswapV2PairSession) Token0() (common.Address, error) {
	return _UniswapV2Pair.Contract.Token0(&_UniswapV2Pair.CallOpts)
}

// Token0 is a free data retrieval call binding the contract method 0x0dfe1681.
//
// Solidity: function token0() view returns(address)
func (_UniswapV2Pair *UniswapV2PairCallerSession) Token0() (common.Address, error) {
	return _UniswapV2Pair.Contract.Token0(&_UniswapV2Pair.CallOpts)
}

// Token1 is a free data retrieval call binding the contract method 0xd21220a7.
//
// Solidity: function token1() view returns(address)
func (_UniswapV2Pair *UniswapV2PairCaller) Token1(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _UniswapV2Pair.contract.Call(opts, &out, "token1")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Token1 is a free data retrieval call binding the contract method 0xd21220a7.
//
// Solidity: function token1() view returns(address)
func (_UniswapV2Pair *UniswapV2PairSession) Token1() (common.Address, error) {
	return _UniswapV2Pair.Contract.Token1(&_UniswapV2Pair.CallOpts)
}

// Token1 is a free data retrieval call binding the contract method 0xd21220a7.
//
// Solidity: function token1() view returns(address)
func (_UniswapV2Pair *UniswapV2PairCallerSession) Token1() (common.Address, error) {
	return _UniswapV2Pair.Contract.Token1(&_UniswapV2Pair.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_UniswapV2Pair *UniswapV2PairCaller) TotalSupply(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _UniswapV2Pair.contract.Call(opts, &out, "totalSupply")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_UniswapV2Pair *UniswapV2PairSession) TotalSupply() (*big.Int, error) {
	return _UniswapV2Pair.Contract.TotalSupply(&_UniswapV2Pair.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_UniswapV2Pair *UniswapV2PairCallerSession) TotalSupply() (*big.Int, error) {
	return _UniswapV2Pair.Contract.TotalSupply(&_UniswapV2Pair.CallOpts)
}

// Initialize is a paid mutator transaction binding the contract method 0x485cc955.
//
// Solidity: function initialize(address _token0, address _token1) returns()
func (_UniswapV2Pair *UniswapV2PairTransactor) Initialize(opts *bind.TransactOpts, _token0 common.Address, _token1 common.Address) (*types.Transaction, error) {
	return _UniswapV2Pair.contract.Transact(opts, "initialize", _token0, _token1)
}

// Initialize is a paid mutator transaction binding the contract method 0x485cc955.
//
// Solidity: function initialize(address _token0, address _token1) returns()
func (_UniswapV2Pair *UniswapV2PairSession) Initialize(_token0 common.Address, _token1 common.Address) (*types.Transaction, error) {
	return _UniswapV2Pair.Contract.Initialize(&_UniswapV2Pair.TransactOpts, _token0, _token1)
}

// Initialize is a paid mutator transaction binding the contract method 0x485cc955.
//
// Solidity: function initialize(address _token0, address _token1) returns()
func (_UniswapV2Pair *UniswapV2PairTransactorSession) Initialize(_token0 common.Address, _token1 common.Address) (*types.Transaction, error) {
	return _UniswapV2Pair.Contract.Initialize(&_UniswapV2Pair.TransactOpts, _token0, _token1)
}

// Mint is a paid mutator transaction binding the contract method 0x6a627842.
//
// Solidity: function mint(address to) returns(uint256 liquidity)
func (_UniswapV2Pair *UniswapV2PairTransactor) Mint(opts *bind.TransactOpts, to common.Address) (*types.Transaction, error) {
	return _UniswapV2Pair.contract.Transact(opts, "mint", to)
}

// Mint is a paid mutator transaction binding the contract method 0x6a627842.
//
// Solidity: function mint(address to) returns(uint256 liquidity)
func (_UniswapV2Pair *UniswapV2PairSession) Mint(to common.Address) (*types.Transaction, error) {
	return _UniswapV2Pair.Contract.Mint(&_UniswapV2Pair.TransactOpts, to)
}

// Mint is a paid mutator transaction binding the contract method 0x6a627842.
//
// Solidity: function mint(address to) returns(uint256 liquidity)
func (_UniswapV2Pair *UniswapV2PairTransactorSession) Mint(to common.Address) (*types.Transaction, error) {
	return _UniswapV2Pair.Contract.Mint(&_UniswapV2Pair.TransactOpts, to)
}

// Swap is a paid mutator transaction binding the contract method 0x022c0d9f.
//
// Solidity: function swap(uint256 amount0Out, uint256 amount1Out, address to, bytes ) returns()
func (_UniswapV2Pair *UniswapV2PairTransactor) Swap(opts *bind.TransactOpts, amount0Out *big.Int, amount1Out *big.Int, to common.Address, arg3 []byte) (*types.Transaction, error) {
	return _UniswapV2Pair.contract.Transact(opts, "swap", amount0Out, amount1Out, to, arg3)
}

// Swap is a paid mutator transaction binding the contract method 0x022c0d9f.
//
// Solidity: function swap(uint256 amount0Out, uint256 amount1Out, address to, bytes ) returns()
func (_UniswapV2Pair *UniswapV2PairSession) Swap(amount0Out *big.Int, amount1Out *big.Int, to common.Address, arg3 []byte) (*types.Transaction, error) {
	return _UniswapV2Pair.Contract.Swap(&_UniswapV2Pair.TransactOpts, amount0Out, amount1Out, to, arg3)
}

// Swap is a paid mutator transaction binding the contract method 0x022c0d9f.
//
// Solidity: function swap(uint256 amount0Out, uint256 amount1Out, address to, bytes ) returns()
func (_UniswapV2Pair *UniswapV2PairTransactorSession) Swap(amount0Out *big.Int, amount1Out *big.Int, to common.Address, arg3 []byte) (*types.Transaction, error) {
	return _UniswapV2Pair.Contract.Swap(&_UniswapV2Pair.TransactOpts, amount0Out, amount1Out, to, arg3)
}

// UniswapV2PairMintIterator is returned from FilterMint and is used to iterate over the raw logs and unpacked data for Mint events raised by the UniswapV2Pair contract.
type UniswapV2PairMintIterator struct {
	Event *UniswapV2PairMint // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *UniswapV2PairMintIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(UniswapV2PairMint)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(UniswapV2PairMint)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *UniswapV2PairMintIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *UniswapV2PairMintIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// UniswapV2PairMint represents a Mint event raised by the UniswapV2Pair contract.
type UniswapV2PairMint struct {
	Sender  common.Address
	Amount0 *big.Int
	Amount1 *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterMint is a free log retrieval operation binding the contract event 0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f.
//
// Solidity: event Mint(address indexed sender, uint256 amount0, uint256 amount1)
func (_UniswapV2Pair *UniswapV2PairFilterer) FilterMint(opts *bind.FilterOpts, sender []common.Address) (*UniswapV2PairMintIterator, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _UniswapV2Pair.contract.FilterLogs(opts, "Mint", senderRule)
	if err != nil {
		return nil, err
	}
	return &UniswapV2PairMintIterator{contract: _UniswapV2Pair.contract, event: "Mint", logs: logs, sub: sub}, nil
}

// WatchMint is a free log subscription operation binding the contract event 0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f.
//
// Solidity: event Mint(address indexed sender, uint256 amount0, uint256 amount1)
func (_UniswapV2Pair *UniswapV2PairFilterer) WatchMint(opts *bind.WatchOpts, sink chan<- *UniswapV2PairMint, sender []common.Address) (event.Subscription, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _UniswapV2Pair.contract.WatchLogs(opts, "Mint", senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(UniswapV2PairMint)
				if err := _UniswapV2Pair.contract.UnpackLog(event, "Mint", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseMint is a log parse operation binding the contract event 0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f.
//
// Solidity: event Mint(address indexed sender, uint256 amount0, uint256 amount1)
func (_UniswapV2Pair *UniswapV2PairFilterer) ParseMint(log types.Log) (*UniswapV2PairMint, error) {
	event := new(UniswapV2PairMint)
	if err := _UniswapV2Pair.contract.UnpackLog(event, "Mint", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// UniswapV2PairSwapIterator is returned from FilterSwap and is used to iterate over the raw logs and unpacked data for Swap events raised by the UniswapV2Pair contract.
type UniswapV2PairSwapIterator struct {
	Event *UniswapV2PairSwap // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *UniswapV2PairSwapIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(UniswapV2PairSwap)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(UniswapV2PairSwap)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *UniswapV2PairSwapIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *UniswapV2PairSwapIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// UniswapV2PairSwap represents a Swap event raised by the UniswapV2Pair contract.
type UniswapV2PairSwap struct {
	Sender     common.Address
	Amount0Out *big.Int
	Amount1Out *big.Int
	To         common.Address
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterSwap is a free log retrieval operation binding the contract event 0x2a9237ff5aa599ef4c5ee4b1142b53429d5755e2685fe6288b2e3320202115f5.
//
// Solidity: event Swap(address indexed sender, uint256 amount0Out, uint256 amount1Out, address indexed to)
func (_UniswapV2Pair *UniswapV2PairFilterer) FilterSwap(opts *bind.FilterOpts, sender []common.Address, to []common.Address) (*UniswapV2PairSwapIterator, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _UniswapV2Pair.contract.FilterLogs(opts, "Swap", senderRule, toRule)
	if err != nil {
		return nil, err
	}
	return &UniswapV2PairSwapIterator{contract: _UniswapV2Pair.contract, event: "Swap", logs: logs, sub: sub}, nil
}

// WatchSwap is a free log subscription operation binding the contract event 0x2a9237ff5aa599ef4c5ee4b1142b53429d5755e2685fe6288b2e3320202115f5.
//
// Solidity: event Swap(address indexed sender, uint256 amount0Out, uint256 amount1Out, address indexed to)
func (_UniswapV2Pair *UniswapV2PairFilterer) WatchSwap(opts *bind.WatchOpts, sink chan<- *UniswapV2PairSwap, sender []common.Address, to []common.Address) (event.Subscription, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _UniswapV2Pair.contract.WatchLogs(opts, "Swap", senderRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(UniswapV2PairSwap)
				if err := _UniswapV2Pair.contract.UnpackLog(event, "Swap", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseSwap is a log parse operation binding the contract event 0x2a9237ff5aa599ef4c5ee4b1142b53429d5755e2685fe6288b2e3320202115f5.
//
// Solidity: event Swap(address indexed sender, uint256 amount0Out, uint256 amount1Out, address indexed to)
func (_UniswapV2Pair *UniswapV2PairFilterer) ParseSwap(log types.Log) (*UniswapV2PairSwap, error) {
	event := new(UniswapV2PairSwap)
	if err := _UniswapV2Pair.contract.UnpackLog(event, "Swap", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// UniswapV2PairSyncIterator is returned from FilterSync and is used to iterate over the raw logs and unpacked data for Sync events raised by the UniswapV2Pair contract.
type UniswapV2PairSyncIterator struct {
	Event *UniswapV2PairSync // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *UniswapV2PairSyncIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(UniswapV2PairSync)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(UniswapV2PairSync)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *UniswapV2PairSyncIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *UniswapV2PairSyncIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// UniswapV2PairSync represents a Sync event raised by the UniswapV2Pair contract.
type UniswapV2PairSync struct {
	Reserve0 *big.Int
	Reserve1 *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterSync is a free log retrieval operation binding the contract event 0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1.
//
// Solidity: event Sync(uint112 reserve0, uint112 reserve1)
func (_UniswapV2Pair *UniswapV2PairFilterer) FilterSync(opts *bind.FilterOpts) (*UniswapV2PairSyncIterator, error) {

	logs, sub, err := _UniswapV2Pair.contract.FilterLogs(opts, "Sync")
	if err != nil {
		return nil, err
	}
	return &UniswapV2PairSyncIterator{contract: _UniswapV2Pair.contract, event: "Sync", logs: logs, sub: sub}, nil
}

// WatchSync is a free log subscription operation binding the contract event 0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1.
//
// Solidity: event Sync(uint112 reserve0, uint112 reserve1)
func (_UniswapV2Pair *UniswapV2PairFilterer) WatchSync(opts *bind.WatchOpts, sink chan<- *UniswapV2PairSync) (event.Subscription, error) {

	logs, sub, err := _UniswapV2Pair.contract.WatchLogs(opts, "Sync")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(UniswapV2PairSync)
				if err := _UniswapV2Pair.contract.UnpackLog(event, "Sync", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseSync is a log parse operation binding the contract event 0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1.
//
// Solidity: event Sync(uint112 reserve0, uint112 reserve1)
func (_UniswapV2Pair *UniswapV2PairFilterer) ParseSync(log types.Log) (*UniswapV2PairSync, error) {
	event := new(UniswapV2PairSync)
	if err := _UniswapV2Pair.contract.UnpackLog(event, "Sync", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
