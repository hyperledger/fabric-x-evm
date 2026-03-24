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

// GenericERC20MetaData contains all meta data concerning the GenericERC20 contract.
var GenericERC20MetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"string\",\"name\":\"tokenName\",\"type\":\"string\"},{\"internalType\":\"string\",\"name\":\"tokenSymbol\",\"type\":\"string\"},{\"internalType\":\"uint256\",\"name\":\"initialSupply\",\"type\":\"uint256\"},{\"internalType\":\"uint8\",\"name\":\"tokenDecimals\",\"type\":\"uint8\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Approval\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Transfer\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"allowance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"approve\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"balanceOf\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"decimals\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"name\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"symbol\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"totalSupply\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"transfer\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"transferFrom\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x60806040523480156200001157600080fd5b50604051620014a0380380620014a08339818101604052810190620000379190620002d7565b8360009081620000489190620005c8565b5082600190816200005a9190620005c8565b5080600260006101000a81548160ff021916908360ff1602179055508160038190555081600460003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000208190555050505050620006af565b6000604051905090565b600080fd5b600080fd5b600080fd5b600080fd5b6000601f19601f8301169050919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b6200013482620000e9565b810181811067ffffffffffffffff82111715620001565762000155620000fa565b5b80604052505050565b60006200016b620000cb565b905062000179828262000129565b919050565b600067ffffffffffffffff8211156200019c576200019b620000fa565b5b620001a782620000e9565b9050602081019050919050565b60005b83811015620001d4578082015181840152602081019050620001b7565b60008484015250505050565b6000620001f7620001f1846200017e565b6200015f565b905082815260208101848484011115620002165762000215620000e4565b5b62000223848285620001b4565b509392505050565b600082601f830112620002435762000242620000df565b5b815162000255848260208601620001e0565b91505092915050565b6000819050919050565b62000273816200025e565b81146200027f57600080fd5b50565b600081519050620002938162000268565b92915050565b600060ff82169050919050565b620002b18162000299565b8114620002bd57600080fd5b50565b600081519050620002d181620002a6565b92915050565b60008060008060808587031215620002f457620002f3620000d5565b5b600085015167ffffffffffffffff811115620003155762000314620000da565b5b62000323878288016200022b565b945050602085015167ffffffffffffffff811115620003475762000346620000da565b5b62000355878288016200022b565b9350506040620003688782880162000282565b92505060606200037b87828801620002c0565b91505092959194509250565b600081519050919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052602260045260246000fd5b60006002820490506001821680620003da57607f821691505b602082108103620003f057620003ef62000392565b5b50919050565b60008190508160005260206000209050919050565b60006020601f8301049050919050565b600082821b905092915050565b6000600883026200045a7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff826200041b565b6200046686836200041b565b95508019841693508086168417925050509392505050565b6000819050919050565b6000620004a9620004a36200049d846200025e565b6200047e565b6200025e565b9050919050565b6000819050919050565b620004c58362000488565b620004dd620004d482620004b0565b84845462000428565b825550505050565b600090565b620004f4620004e5565b62000501818484620004ba565b505050565b5b8181101562000529576200051d600082620004ea565b60018101905062000507565b5050565b601f82111562000578576200054281620003f6565b6200054d846200040b565b810160208510156200055d578190505b620005756200056c856200040b565b83018262000506565b50505b505050565b600082821c905092915050565b60006200059d600019846008026200057d565b1980831691505092915050565b6000620005b883836200058a565b9150826002028217905092915050565b620005d38262000387565b67ffffffffffffffff811115620005ef57620005ee620000fa565b5b620005fb8254620003c1565b620006088282856200052d565b600060209050601f8311600181146200064057600084156200062b578287015190505b620006378582620005aa565b865550620006a7565b601f1984166200065086620003f6565b60005b828110156200067a5784890151825560018201915060208501945060208101905062000653565b868310156200069a578489015162000696601f8916826200058a565b8355505b6001600288020188555050505b505050505050565b610de180620006bf6000396000f3fe608060405234801561001057600080fd5b50600436106100935760003560e01c8063313ce56711610066578063313ce5671461013457806370a082311461015257806395d89b4114610182578063a9059cbb146101a0578063dd62ed3e146101d057610093565b806306fdde0314610098578063095ea7b3146100b657806318160ddd146100e657806323b872dd14610104575b600080fd5b6100a0610200565b6040516100ad919061098a565b60405180910390f35b6100d060048036038101906100cb9190610a45565b61028e565b6040516100dd9190610aa0565b60405180910390f35b6100ee610380565b6040516100fb9190610aca565b60405180910390f35b61011e60048036038101906101199190610ae5565b610386565b60405161012b9190610aa0565b60405180910390f35b61013c61067d565b6040516101499190610b54565b60405180910390f35b61016c60048036038101906101679190610b6f565b610690565b6040516101799190610aca565b60405180910390f35b61018a6106a8565b604051610197919061098a565b60405180910390f35b6101ba60048036038101906101b59190610a45565b610736565b6040516101c79190610aa0565b60405180910390f35b6101ea60048036038101906101e59190610b9c565b6108d5565b6040516101f79190610aca565b60405180910390f35b6000805461020d90610c0b565b80601f016020809104026020016040519081016040528092919081815260200182805461023990610c0b565b80156102865780601f1061025b57610100808354040283529160200191610286565b820191906000526020600020905b81548152906001019060200180831161026957829003601f168201915b505050505081565b600081600560003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055508273ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9258460405161036e9190610aca565b60405180910390a36001905092915050565b60035481565b600080600560008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205490508281101561044b576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161044290610c88565b60405180910390fd5b82600460008773ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410156104cd576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016104c490610cf4565b60405180910390fd5b82600560008773ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008282546105599190610d43565b9250508190555082600460008773ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008282546105af9190610d43565b9250508190555082600460008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008282546106059190610d77565b925050819055508373ffffffffffffffffffffffffffffffffffffffff168573ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef856040516106699190610aca565b60405180910390a360019150509392505050565b600260009054906101000a900460ff1681565b60046020528060005260406000206000915090505481565b600180546106b590610c0b565b80601f01602080910402602001604051908101604052809291908181526020018280546106e190610c0b565b801561072e5780601f106107035761010080835404028352916020019161072e565b820191906000526020600020905b81548152906001019060200180831161071157829003601f168201915b505050505081565b600081600460003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205410156107ba576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016107b190610cf4565b60405180910390fd5b81600460003373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008282546108099190610d43565b9250508190555081600460008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825461085f9190610d77565b925050819055508273ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef846040516108c39190610aca565b60405180910390a36001905092915050565b6005602052816000526040600020602052806000526040600020600091509150505481565b600081519050919050565b600082825260208201905092915050565b60005b83811015610934578082015181840152602081019050610919565b60008484015250505050565b6000601f19601f8301169050919050565b600061095c826108fa565b6109668185610905565b9350610976818560208601610916565b61097f81610940565b840191505092915050565b600060208201905081810360008301526109a48184610951565b905092915050565b600080fd5b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b60006109dc826109b1565b9050919050565b6109ec816109d1565b81146109f757600080fd5b50565b600081359050610a09816109e3565b92915050565b6000819050919050565b610a2281610a0f565b8114610a2d57600080fd5b50565b600081359050610a3f81610a19565b92915050565b60008060408385031215610a5c57610a5b6109ac565b5b6000610a6a858286016109fa565b9250506020610a7b85828601610a30565b9150509250929050565b60008115159050919050565b610a9a81610a85565b82525050565b6000602082019050610ab56000830184610a91565b92915050565b610ac481610a0f565b82525050565b6000602082019050610adf6000830184610abb565b92915050565b600080600060608486031215610afe57610afd6109ac565b5b6000610b0c868287016109fa565b9350506020610b1d868287016109fa565b9250506040610b2e86828701610a30565b9150509250925092565b600060ff82169050919050565b610b4e81610b38565b82525050565b6000602082019050610b696000830184610b45565b92915050565b600060208284031215610b8557610b846109ac565b5b6000610b93848285016109fa565b91505092915050565b60008060408385031215610bb357610bb26109ac565b5b6000610bc1858286016109fa565b9250506020610bd2858286016109fa565b9150509250929050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052602260045260246000fd5b60006002820490506001821680610c2357607f821691505b602082108103610c3657610c35610bdc565b5b50919050565b7f416c6c6f77616e63652065786365656465640000000000000000000000000000600082015250565b6000610c72601283610905565b9150610c7d82610c3c565b602082019050919050565b60006020820190508181036000830152610ca181610c65565b9050919050565b7f496e73756666696369656e742062616c616e6365000000000000000000000000600082015250565b6000610cde601483610905565b9150610ce982610ca8565b602082019050919050565b60006020820190508181036000830152610d0d81610cd1565b9050919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b6000610d4e82610a0f565b9150610d5983610a0f565b9250828203905081811115610d7157610d70610d14565b5b92915050565b6000610d8282610a0f565b9150610d8d83610a0f565b9250828201905080821115610da557610da4610d14565b5b9291505056fea2646970667358221220e35067cc12102fd4a3ad2a7abe259ae6364c0b867ded43af9129a147ef43640564736f6c63430008140033",
}

// GenericERC20ABI is the input ABI used to generate the binding from.
// Deprecated: Use GenericERC20MetaData.ABI instead.
var GenericERC20ABI = GenericERC20MetaData.ABI

// GenericERC20Bin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use GenericERC20MetaData.Bin instead.
var GenericERC20Bin = GenericERC20MetaData.Bin

// DeployGenericERC20 deploys a new Ethereum contract, binding an instance of GenericERC20 to it.
func DeployGenericERC20(auth *bind.TransactOpts, backend bind.ContractBackend, tokenName string, tokenSymbol string, initialSupply *big.Int, tokenDecimals uint8) (common.Address, *types.Transaction, *GenericERC20, error) {
	parsed, err := GenericERC20MetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(GenericERC20Bin), backend, tokenName, tokenSymbol, initialSupply, tokenDecimals)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &GenericERC20{GenericERC20Caller: GenericERC20Caller{contract: contract}, GenericERC20Transactor: GenericERC20Transactor{contract: contract}, GenericERC20Filterer: GenericERC20Filterer{contract: contract}}, nil
}

// GenericERC20 is an auto generated Go binding around an Ethereum contract.
type GenericERC20 struct {
	GenericERC20Caller     // Read-only binding to the contract
	GenericERC20Transactor // Write-only binding to the contract
	GenericERC20Filterer   // Log filterer for contract events
}

// GenericERC20Caller is an auto generated read-only Go binding around an Ethereum contract.
type GenericERC20Caller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// GenericERC20Transactor is an auto generated write-only Go binding around an Ethereum contract.
type GenericERC20Transactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// GenericERC20Filterer is an auto generated log filtering Go binding around an Ethereum contract events.
type GenericERC20Filterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// GenericERC20Session is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type GenericERC20Session struct {
	Contract     *GenericERC20     // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// GenericERC20CallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type GenericERC20CallerSession struct {
	Contract *GenericERC20Caller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts       // Call options to use throughout this session
}

// GenericERC20TransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type GenericERC20TransactorSession struct {
	Contract     *GenericERC20Transactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts       // Transaction auth options to use throughout this session
}

// GenericERC20Raw is an auto generated low-level Go binding around an Ethereum contract.
type GenericERC20Raw struct {
	Contract *GenericERC20 // Generic contract binding to access the raw methods on
}

// GenericERC20CallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type GenericERC20CallerRaw struct {
	Contract *GenericERC20Caller // Generic read-only contract binding to access the raw methods on
}

// GenericERC20TransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type GenericERC20TransactorRaw struct {
	Contract *GenericERC20Transactor // Generic write-only contract binding to access the raw methods on
}

// NewGenericERC20 creates a new instance of GenericERC20, bound to a specific deployed contract.
func NewGenericERC20(address common.Address, backend bind.ContractBackend) (*GenericERC20, error) {
	contract, err := bindGenericERC20(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &GenericERC20{GenericERC20Caller: GenericERC20Caller{contract: contract}, GenericERC20Transactor: GenericERC20Transactor{contract: contract}, GenericERC20Filterer: GenericERC20Filterer{contract: contract}}, nil
}

// NewGenericERC20Caller creates a new read-only instance of GenericERC20, bound to a specific deployed contract.
func NewGenericERC20Caller(address common.Address, caller bind.ContractCaller) (*GenericERC20Caller, error) {
	contract, err := bindGenericERC20(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &GenericERC20Caller{contract: contract}, nil
}

// NewGenericERC20Transactor creates a new write-only instance of GenericERC20, bound to a specific deployed contract.
func NewGenericERC20Transactor(address common.Address, transactor bind.ContractTransactor) (*GenericERC20Transactor, error) {
	contract, err := bindGenericERC20(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &GenericERC20Transactor{contract: contract}, nil
}

// NewGenericERC20Filterer creates a new log filterer instance of GenericERC20, bound to a specific deployed contract.
func NewGenericERC20Filterer(address common.Address, filterer bind.ContractFilterer) (*GenericERC20Filterer, error) {
	contract, err := bindGenericERC20(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &GenericERC20Filterer{contract: contract}, nil
}

// bindGenericERC20 binds a generic wrapper to an already deployed contract.
func bindGenericERC20(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := GenericERC20MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_GenericERC20 *GenericERC20Raw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _GenericERC20.Contract.GenericERC20Caller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_GenericERC20 *GenericERC20Raw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _GenericERC20.Contract.GenericERC20Transactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_GenericERC20 *GenericERC20Raw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _GenericERC20.Contract.GenericERC20Transactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_GenericERC20 *GenericERC20CallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _GenericERC20.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_GenericERC20 *GenericERC20TransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _GenericERC20.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_GenericERC20 *GenericERC20TransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _GenericERC20.Contract.contract.Transact(opts, method, params...)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address , address ) view returns(uint256)
func (_GenericERC20 *GenericERC20Caller) Allowance(opts *bind.CallOpts, arg0 common.Address, arg1 common.Address) (*big.Int, error) {
	var out []interface{}
	err := _GenericERC20.contract.Call(opts, &out, "allowance", arg0, arg1)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address , address ) view returns(uint256)
func (_GenericERC20 *GenericERC20Session) Allowance(arg0 common.Address, arg1 common.Address) (*big.Int, error) {
	return _GenericERC20.Contract.Allowance(&_GenericERC20.CallOpts, arg0, arg1)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address , address ) view returns(uint256)
func (_GenericERC20 *GenericERC20CallerSession) Allowance(arg0 common.Address, arg1 common.Address) (*big.Int, error) {
	return _GenericERC20.Contract.Allowance(&_GenericERC20.CallOpts, arg0, arg1)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address ) view returns(uint256)
func (_GenericERC20 *GenericERC20Caller) BalanceOf(opts *bind.CallOpts, arg0 common.Address) (*big.Int, error) {
	var out []interface{}
	err := _GenericERC20.contract.Call(opts, &out, "balanceOf", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address ) view returns(uint256)
func (_GenericERC20 *GenericERC20Session) BalanceOf(arg0 common.Address) (*big.Int, error) {
	return _GenericERC20.Contract.BalanceOf(&_GenericERC20.CallOpts, arg0)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address ) view returns(uint256)
func (_GenericERC20 *GenericERC20CallerSession) BalanceOf(arg0 common.Address) (*big.Int, error) {
	return _GenericERC20.Contract.BalanceOf(&_GenericERC20.CallOpts, arg0)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_GenericERC20 *GenericERC20Caller) Decimals(opts *bind.CallOpts) (uint8, error) {
	var out []interface{}
	err := _GenericERC20.contract.Call(opts, &out, "decimals")

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_GenericERC20 *GenericERC20Session) Decimals() (uint8, error) {
	return _GenericERC20.Contract.Decimals(&_GenericERC20.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_GenericERC20 *GenericERC20CallerSession) Decimals() (uint8, error) {
	return _GenericERC20.Contract.Decimals(&_GenericERC20.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_GenericERC20 *GenericERC20Caller) Name(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _GenericERC20.contract.Call(opts, &out, "name")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_GenericERC20 *GenericERC20Session) Name() (string, error) {
	return _GenericERC20.Contract.Name(&_GenericERC20.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_GenericERC20 *GenericERC20CallerSession) Name() (string, error) {
	return _GenericERC20.Contract.Name(&_GenericERC20.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_GenericERC20 *GenericERC20Caller) Symbol(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _GenericERC20.contract.Call(opts, &out, "symbol")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_GenericERC20 *GenericERC20Session) Symbol() (string, error) {
	return _GenericERC20.Contract.Symbol(&_GenericERC20.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_GenericERC20 *GenericERC20CallerSession) Symbol() (string, error) {
	return _GenericERC20.Contract.Symbol(&_GenericERC20.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_GenericERC20 *GenericERC20Caller) TotalSupply(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _GenericERC20.contract.Call(opts, &out, "totalSupply")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_GenericERC20 *GenericERC20Session) TotalSupply() (*big.Int, error) {
	return _GenericERC20.Contract.TotalSupply(&_GenericERC20.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_GenericERC20 *GenericERC20CallerSession) TotalSupply() (*big.Int, error) {
	return _GenericERC20.Contract.TotalSupply(&_GenericERC20.CallOpts)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_GenericERC20 *GenericERC20Transactor) Approve(opts *bind.TransactOpts, spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _GenericERC20.contract.Transact(opts, "approve", spender, value)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_GenericERC20 *GenericERC20Session) Approve(spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _GenericERC20.Contract.Approve(&_GenericERC20.TransactOpts, spender, value)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_GenericERC20 *GenericERC20TransactorSession) Approve(spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _GenericERC20.Contract.Approve(&_GenericERC20.TransactOpts, spender, value)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_GenericERC20 *GenericERC20Transactor) Transfer(opts *bind.TransactOpts, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _GenericERC20.contract.Transact(opts, "transfer", to, value)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_GenericERC20 *GenericERC20Session) Transfer(to common.Address, value *big.Int) (*types.Transaction, error) {
	return _GenericERC20.Contract.Transfer(&_GenericERC20.TransactOpts, to, value)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_GenericERC20 *GenericERC20TransactorSession) Transfer(to common.Address, value *big.Int) (*types.Transaction, error) {
	return _GenericERC20.Contract.Transfer(&_GenericERC20.TransactOpts, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_GenericERC20 *GenericERC20Transactor) TransferFrom(opts *bind.TransactOpts, from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _GenericERC20.contract.Transact(opts, "transferFrom", from, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_GenericERC20 *GenericERC20Session) TransferFrom(from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _GenericERC20.Contract.TransferFrom(&_GenericERC20.TransactOpts, from, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_GenericERC20 *GenericERC20TransactorSession) TransferFrom(from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _GenericERC20.Contract.TransferFrom(&_GenericERC20.TransactOpts, from, to, value)
}

// GenericERC20ApprovalIterator is returned from FilterApproval and is used to iterate over the raw logs and unpacked data for Approval events raised by the GenericERC20 contract.
type GenericERC20ApprovalIterator struct {
	Event *GenericERC20Approval // Event containing the contract specifics and raw log

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
func (it *GenericERC20ApprovalIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(GenericERC20Approval)
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
		it.Event = new(GenericERC20Approval)
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
func (it *GenericERC20ApprovalIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *GenericERC20ApprovalIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// GenericERC20Approval represents a Approval event raised by the GenericERC20 contract.
type GenericERC20Approval struct {
	Owner   common.Address
	Spender common.Address
	Value   *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterApproval is a free log retrieval operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_GenericERC20 *GenericERC20Filterer) FilterApproval(opts *bind.FilterOpts, owner []common.Address, spender []common.Address) (*GenericERC20ApprovalIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _GenericERC20.contract.FilterLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return &GenericERC20ApprovalIterator{contract: _GenericERC20.contract, event: "Approval", logs: logs, sub: sub}, nil
}

// WatchApproval is a free log subscription operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_GenericERC20 *GenericERC20Filterer) WatchApproval(opts *bind.WatchOpts, sink chan<- *GenericERC20Approval, owner []common.Address, spender []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _GenericERC20.contract.WatchLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(GenericERC20Approval)
				if err := _GenericERC20.contract.UnpackLog(event, "Approval", log); err != nil {
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

// ParseApproval is a log parse operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_GenericERC20 *GenericERC20Filterer) ParseApproval(log types.Log) (*GenericERC20Approval, error) {
	event := new(GenericERC20Approval)
	if err := _GenericERC20.contract.UnpackLog(event, "Approval", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// GenericERC20TransferIterator is returned from FilterTransfer and is used to iterate over the raw logs and unpacked data for Transfer events raised by the GenericERC20 contract.
type GenericERC20TransferIterator struct {
	Event *GenericERC20Transfer // Event containing the contract specifics and raw log

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
func (it *GenericERC20TransferIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(GenericERC20Transfer)
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
		it.Event = new(GenericERC20Transfer)
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
func (it *GenericERC20TransferIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *GenericERC20TransferIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// GenericERC20Transfer represents a Transfer event raised by the GenericERC20 contract.
type GenericERC20Transfer struct {
	From  common.Address
	To    common.Address
	Value *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterTransfer is a free log retrieval operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_GenericERC20 *GenericERC20Filterer) FilterTransfer(opts *bind.FilterOpts, from []common.Address, to []common.Address) (*GenericERC20TransferIterator, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _GenericERC20.contract.FilterLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return &GenericERC20TransferIterator{contract: _GenericERC20.contract, event: "Transfer", logs: logs, sub: sub}, nil
}

// WatchTransfer is a free log subscription operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_GenericERC20 *GenericERC20Filterer) WatchTransfer(opts *bind.WatchOpts, sink chan<- *GenericERC20Transfer, from []common.Address, to []common.Address) (event.Subscription, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _GenericERC20.contract.WatchLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(GenericERC20Transfer)
				if err := _GenericERC20.contract.UnpackLog(event, "Transfer", log); err != nil {
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

// ParseTransfer is a log parse operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_GenericERC20 *GenericERC20Filterer) ParseTransfer(log types.Log) (*GenericERC20Transfer, error) {
	event := new(GenericERC20Transfer)
	if err := _GenericERC20.contract.UnpackLog(event, "Transfer", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
