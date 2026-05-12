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

// FiatTokenV22MetaData contains all meta data concerning the FiatTokenV22 contract.
var FiatTokenV22MetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Approval\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"authorizer\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"nonce\",\"type\":\"bytes32\"}],\"name\":\"AuthorizationCanceled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"authorizer\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"nonce\",\"type\":\"bytes32\"}],\"name\":\"AuthorizationUsed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"_account\",\"type\":\"address\"}],\"name\":\"Blacklisted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newBlacklister\",\"type\":\"address\"}],\"name\":\"BlacklisterChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"burner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"Burn\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newMasterMinter\",\"type\":\"address\"}],\"name\":\"MasterMinterChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"minter\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"Mint\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"minter\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"minterAllowedAmount\",\"type\":\"uint256\"}],\"name\":\"MinterConfigured\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"oldMinter\",\"type\":\"address\"}],\"name\":\"MinterRemoved\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[],\"name\":\"Pause\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newAddress\",\"type\":\"address\"}],\"name\":\"PauserChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newRescuer\",\"type\":\"address\"}],\"name\":\"RescuerChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Transfer\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"_account\",\"type\":\"address\"}],\"name\":\"UnBlacklisted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[],\"name\":\"Unpause\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"CANCEL_AUTHORIZATION_TYPEHASH\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"DOMAIN_SEPARATOR\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"PERMIT_TYPEHASH\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"RECEIVE_WITH_AUTHORIZATION_TYPEHASH\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"TRANSFER_WITH_AUTHORIZATION_TYPEHASH\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"}],\"name\":\"allowance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"approve\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"authorizer\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"nonce\",\"type\":\"bytes32\"}],\"name\":\"authorizationState\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"balanceOf\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_account\",\"type\":\"address\"}],\"name\":\"blacklist\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"blacklister\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"_amount\",\"type\":\"uint256\"}],\"name\":\"burn\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"authorizer\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"nonce\",\"type\":\"bytes32\"},{\"internalType\":\"uint8\",\"name\":\"v\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"r\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"s\",\"type\":\"bytes32\"}],\"name\":\"cancelAuthorization\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"authorizer\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"nonce\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"name\":\"cancelAuthorization\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"minter\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"minterAllowedAmount\",\"type\":\"uint256\"}],\"name\":\"configureMinter\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"currency\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"decimals\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"decrement\",\"type\":\"uint256\"}],\"name\":\"decreaseAllowance\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"increment\",\"type\":\"uint256\"}],\"name\":\"increaseAllowance\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"string\",\"name\":\"tokenName\",\"type\":\"string\"},{\"internalType\":\"string\",\"name\":\"tokenSymbol\",\"type\":\"string\"},{\"internalType\":\"string\",\"name\":\"tokenCurrency\",\"type\":\"string\"},{\"internalType\":\"uint8\",\"name\":\"tokenDecimals\",\"type\":\"uint8\"},{\"internalType\":\"address\",\"name\":\"newMasterMinter\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"newPauser\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"newBlacklister\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"initialize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"string\",\"name\":\"newName\",\"type\":\"string\"}],\"name\":\"initializeV2\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"lostAndFound\",\"type\":\"address\"}],\"name\":\"initializeV2_1\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address[]\",\"name\":\"accountsToBlacklist\",\"type\":\"address[]\"},{\"internalType\":\"string\",\"name\":\"newSymbol\",\"type\":\"string\"}],\"name\":\"initializeV2_2\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_account\",\"type\":\"address\"}],\"name\":\"isBlacklisted\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"isMinter\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"masterMinter\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"_amount\",\"type\":\"uint256\"}],\"name\":\"mint\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"minter\",\"type\":\"address\"}],\"name\":\"minterAllowance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"name\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"}],\"name\":\"nonces\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"pause\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"paused\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"pauser\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"deadline\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"name\":\"permit\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"deadline\",\"type\":\"uint256\"},{\"internalType\":\"uint8\",\"name\":\"v\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"r\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"s\",\"type\":\"bytes32\"}],\"name\":\"permit\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"validAfter\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"validBefore\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"nonce\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"name\":\"receiveWithAuthorization\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"validAfter\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"validBefore\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"nonce\",\"type\":\"bytes32\"},{\"internalType\":\"uint8\",\"name\":\"v\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"r\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"s\",\"type\":\"bytes32\"}],\"name\":\"receiveWithAuthorization\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"minter\",\"type\":\"address\"}],\"name\":\"removeMinter\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"contractIERC20\",\"name\":\"tokenContract\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"rescueERC20\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"rescuer\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"symbol\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"totalSupply\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"transfer\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"transferFrom\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"validAfter\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"validBefore\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"nonce\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"name\":\"transferWithAuthorization\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"validAfter\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"validBefore\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"nonce\",\"type\":\"bytes32\"},{\"internalType\":\"uint8\",\"name\":\"v\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"r\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"s\",\"type\":\"bytes32\"}],\"name\":\"transferWithAuthorization\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_account\",\"type\":\"address\"}],\"name\":\"unBlacklist\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"unpause\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_newBlacklister\",\"type\":\"address\"}],\"name\":\"updateBlacklister\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_newMasterMinter\",\"type\":\"address\"}],\"name\":\"updateMasterMinter\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_newPauser\",\"type\":\"address\"}],\"name\":\"updatePauser\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newRescuer\",\"type\":\"address\"}],\"name\":\"updateRescuer\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"version\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"pure\",\"type\":\"function\"}]",
}

// FiatTokenV22ABI is the input ABI used to generate the binding from.
// Deprecated: Use FiatTokenV22MetaData.ABI instead.
var FiatTokenV22ABI = FiatTokenV22MetaData.ABI

// FiatTokenV22 is an auto generated Go binding around an Ethereum contract.
type FiatTokenV22 struct {
	FiatTokenV22Caller     // Read-only binding to the contract
	FiatTokenV22Transactor // Write-only binding to the contract
	FiatTokenV22Filterer   // Log filterer for contract events
}

// FiatTokenV22Caller is an auto generated read-only Go binding around an Ethereum contract.
type FiatTokenV22Caller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FiatTokenV22Transactor is an auto generated write-only Go binding around an Ethereum contract.
type FiatTokenV22Transactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FiatTokenV22Filterer is an auto generated log filtering Go binding around an Ethereum contract events.
type FiatTokenV22Filterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FiatTokenV22Session is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type FiatTokenV22Session struct {
	Contract     *FiatTokenV22     // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// FiatTokenV22CallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type FiatTokenV22CallerSession struct {
	Contract *FiatTokenV22Caller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts       // Call options to use throughout this session
}

// FiatTokenV22TransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type FiatTokenV22TransactorSession struct {
	Contract     *FiatTokenV22Transactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts       // Transaction auth options to use throughout this session
}

// FiatTokenV22Raw is an auto generated low-level Go binding around an Ethereum contract.
type FiatTokenV22Raw struct {
	Contract *FiatTokenV22 // Generic contract binding to access the raw methods on
}

// FiatTokenV22CallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type FiatTokenV22CallerRaw struct {
	Contract *FiatTokenV22Caller // Generic read-only contract binding to access the raw methods on
}

// FiatTokenV22TransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type FiatTokenV22TransactorRaw struct {
	Contract *FiatTokenV22Transactor // Generic write-only contract binding to access the raw methods on
}

// NewFiatTokenV22 creates a new instance of FiatTokenV22, bound to a specific deployed contract.
func NewFiatTokenV22(address common.Address, backend bind.ContractBackend) (*FiatTokenV22, error) {
	contract, err := bindFiatTokenV22(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22{FiatTokenV22Caller: FiatTokenV22Caller{contract: contract}, FiatTokenV22Transactor: FiatTokenV22Transactor{contract: contract}, FiatTokenV22Filterer: FiatTokenV22Filterer{contract: contract}}, nil
}

// NewFiatTokenV22Caller creates a new read-only instance of FiatTokenV22, bound to a specific deployed contract.
func NewFiatTokenV22Caller(address common.Address, caller bind.ContractCaller) (*FiatTokenV22Caller, error) {
	contract, err := bindFiatTokenV22(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22Caller{contract: contract}, nil
}

// NewFiatTokenV22Transactor creates a new write-only instance of FiatTokenV22, bound to a specific deployed contract.
func NewFiatTokenV22Transactor(address common.Address, transactor bind.ContractTransactor) (*FiatTokenV22Transactor, error) {
	contract, err := bindFiatTokenV22(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22Transactor{contract: contract}, nil
}

// NewFiatTokenV22Filterer creates a new log filterer instance of FiatTokenV22, bound to a specific deployed contract.
func NewFiatTokenV22Filterer(address common.Address, filterer bind.ContractFilterer) (*FiatTokenV22Filterer, error) {
	contract, err := bindFiatTokenV22(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22Filterer{contract: contract}, nil
}

// bindFiatTokenV22 binds a generic wrapper to an already deployed contract.
func bindFiatTokenV22(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := FiatTokenV22MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FiatTokenV22 *FiatTokenV22Raw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FiatTokenV22.Contract.FiatTokenV22Caller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FiatTokenV22 *FiatTokenV22Raw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.FiatTokenV22Transactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FiatTokenV22 *FiatTokenV22Raw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.FiatTokenV22Transactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FiatTokenV22 *FiatTokenV22CallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FiatTokenV22.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FiatTokenV22 *FiatTokenV22TransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FiatTokenV22 *FiatTokenV22TransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.contract.Transact(opts, method, params...)
}

// CANCELAUTHORIZATIONTYPEHASH is a free data retrieval call binding the contract method 0xd9169487.
//
// Solidity: function CANCEL_AUTHORIZATION_TYPEHASH() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22Caller) CANCELAUTHORIZATIONTYPEHASH(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "CANCEL_AUTHORIZATION_TYPEHASH")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// CANCELAUTHORIZATIONTYPEHASH is a free data retrieval call binding the contract method 0xd9169487.
//
// Solidity: function CANCEL_AUTHORIZATION_TYPEHASH() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22Session) CANCELAUTHORIZATIONTYPEHASH() ([32]byte, error) {
	return _FiatTokenV22.Contract.CANCELAUTHORIZATIONTYPEHASH(&_FiatTokenV22.CallOpts)
}

// CANCELAUTHORIZATIONTYPEHASH is a free data retrieval call binding the contract method 0xd9169487.
//
// Solidity: function CANCEL_AUTHORIZATION_TYPEHASH() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22CallerSession) CANCELAUTHORIZATIONTYPEHASH() ([32]byte, error) {
	return _FiatTokenV22.Contract.CANCELAUTHORIZATIONTYPEHASH(&_FiatTokenV22.CallOpts)
}

// DOMAINSEPARATOR is a free data retrieval call binding the contract method 0x3644e515.
//
// Solidity: function DOMAIN_SEPARATOR() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22Caller) DOMAINSEPARATOR(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "DOMAIN_SEPARATOR")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// DOMAINSEPARATOR is a free data retrieval call binding the contract method 0x3644e515.
//
// Solidity: function DOMAIN_SEPARATOR() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22Session) DOMAINSEPARATOR() ([32]byte, error) {
	return _FiatTokenV22.Contract.DOMAINSEPARATOR(&_FiatTokenV22.CallOpts)
}

// DOMAINSEPARATOR is a free data retrieval call binding the contract method 0x3644e515.
//
// Solidity: function DOMAIN_SEPARATOR() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22CallerSession) DOMAINSEPARATOR() ([32]byte, error) {
	return _FiatTokenV22.Contract.DOMAINSEPARATOR(&_FiatTokenV22.CallOpts)
}

// PERMITTYPEHASH is a free data retrieval call binding the contract method 0x30adf81f.
//
// Solidity: function PERMIT_TYPEHASH() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22Caller) PERMITTYPEHASH(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "PERMIT_TYPEHASH")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// PERMITTYPEHASH is a free data retrieval call binding the contract method 0x30adf81f.
//
// Solidity: function PERMIT_TYPEHASH() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22Session) PERMITTYPEHASH() ([32]byte, error) {
	return _FiatTokenV22.Contract.PERMITTYPEHASH(&_FiatTokenV22.CallOpts)
}

// PERMITTYPEHASH is a free data retrieval call binding the contract method 0x30adf81f.
//
// Solidity: function PERMIT_TYPEHASH() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22CallerSession) PERMITTYPEHASH() ([32]byte, error) {
	return _FiatTokenV22.Contract.PERMITTYPEHASH(&_FiatTokenV22.CallOpts)
}

// RECEIVEWITHAUTHORIZATIONTYPEHASH is a free data retrieval call binding the contract method 0x7f2eecc3.
//
// Solidity: function RECEIVE_WITH_AUTHORIZATION_TYPEHASH() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22Caller) RECEIVEWITHAUTHORIZATIONTYPEHASH(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "RECEIVE_WITH_AUTHORIZATION_TYPEHASH")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// RECEIVEWITHAUTHORIZATIONTYPEHASH is a free data retrieval call binding the contract method 0x7f2eecc3.
//
// Solidity: function RECEIVE_WITH_AUTHORIZATION_TYPEHASH() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22Session) RECEIVEWITHAUTHORIZATIONTYPEHASH() ([32]byte, error) {
	return _FiatTokenV22.Contract.RECEIVEWITHAUTHORIZATIONTYPEHASH(&_FiatTokenV22.CallOpts)
}

// RECEIVEWITHAUTHORIZATIONTYPEHASH is a free data retrieval call binding the contract method 0x7f2eecc3.
//
// Solidity: function RECEIVE_WITH_AUTHORIZATION_TYPEHASH() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22CallerSession) RECEIVEWITHAUTHORIZATIONTYPEHASH() ([32]byte, error) {
	return _FiatTokenV22.Contract.RECEIVEWITHAUTHORIZATIONTYPEHASH(&_FiatTokenV22.CallOpts)
}

// TRANSFERWITHAUTHORIZATIONTYPEHASH is a free data retrieval call binding the contract method 0xa0cc6a68.
//
// Solidity: function TRANSFER_WITH_AUTHORIZATION_TYPEHASH() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22Caller) TRANSFERWITHAUTHORIZATIONTYPEHASH(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "TRANSFER_WITH_AUTHORIZATION_TYPEHASH")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// TRANSFERWITHAUTHORIZATIONTYPEHASH is a free data retrieval call binding the contract method 0xa0cc6a68.
//
// Solidity: function TRANSFER_WITH_AUTHORIZATION_TYPEHASH() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22Session) TRANSFERWITHAUTHORIZATIONTYPEHASH() ([32]byte, error) {
	return _FiatTokenV22.Contract.TRANSFERWITHAUTHORIZATIONTYPEHASH(&_FiatTokenV22.CallOpts)
}

// TRANSFERWITHAUTHORIZATIONTYPEHASH is a free data retrieval call binding the contract method 0xa0cc6a68.
//
// Solidity: function TRANSFER_WITH_AUTHORIZATION_TYPEHASH() view returns(bytes32)
func (_FiatTokenV22 *FiatTokenV22CallerSession) TRANSFERWITHAUTHORIZATIONTYPEHASH() ([32]byte, error) {
	return _FiatTokenV22.Contract.TRANSFERWITHAUTHORIZATIONTYPEHASH(&_FiatTokenV22.CallOpts)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22Caller) Allowance(opts *bind.CallOpts, owner common.Address, spender common.Address) (*big.Int, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "allowance", owner, spender)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22Session) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _FiatTokenV22.Contract.Allowance(&_FiatTokenV22.CallOpts, owner, spender)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22CallerSession) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _FiatTokenV22.Contract.Allowance(&_FiatTokenV22.CallOpts, owner, spender)
}

// AuthorizationState is a free data retrieval call binding the contract method 0xe94a0102.
//
// Solidity: function authorizationState(address authorizer, bytes32 nonce) view returns(bool)
func (_FiatTokenV22 *FiatTokenV22Caller) AuthorizationState(opts *bind.CallOpts, authorizer common.Address, nonce [32]byte) (bool, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "authorizationState", authorizer, nonce)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// AuthorizationState is a free data retrieval call binding the contract method 0xe94a0102.
//
// Solidity: function authorizationState(address authorizer, bytes32 nonce) view returns(bool)
func (_FiatTokenV22 *FiatTokenV22Session) AuthorizationState(authorizer common.Address, nonce [32]byte) (bool, error) {
	return _FiatTokenV22.Contract.AuthorizationState(&_FiatTokenV22.CallOpts, authorizer, nonce)
}

// AuthorizationState is a free data retrieval call binding the contract method 0xe94a0102.
//
// Solidity: function authorizationState(address authorizer, bytes32 nonce) view returns(bool)
func (_FiatTokenV22 *FiatTokenV22CallerSession) AuthorizationState(authorizer common.Address, nonce [32]byte) (bool, error) {
	return _FiatTokenV22.Contract.AuthorizationState(&_FiatTokenV22.CallOpts, authorizer, nonce)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22Caller) BalanceOf(opts *bind.CallOpts, account common.Address) (*big.Int, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "balanceOf", account)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22Session) BalanceOf(account common.Address) (*big.Int, error) {
	return _FiatTokenV22.Contract.BalanceOf(&_FiatTokenV22.CallOpts, account)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22CallerSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _FiatTokenV22.Contract.BalanceOf(&_FiatTokenV22.CallOpts, account)
}

// Blacklister is a free data retrieval call binding the contract method 0xbd102430.
//
// Solidity: function blacklister() view returns(address)
func (_FiatTokenV22 *FiatTokenV22Caller) Blacklister(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "blacklister")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Blacklister is a free data retrieval call binding the contract method 0xbd102430.
//
// Solidity: function blacklister() view returns(address)
func (_FiatTokenV22 *FiatTokenV22Session) Blacklister() (common.Address, error) {
	return _FiatTokenV22.Contract.Blacklister(&_FiatTokenV22.CallOpts)
}

// Blacklister is a free data retrieval call binding the contract method 0xbd102430.
//
// Solidity: function blacklister() view returns(address)
func (_FiatTokenV22 *FiatTokenV22CallerSession) Blacklister() (common.Address, error) {
	return _FiatTokenV22.Contract.Blacklister(&_FiatTokenV22.CallOpts)
}

// Currency is a free data retrieval call binding the contract method 0xe5a6b10f.
//
// Solidity: function currency() view returns(string)
func (_FiatTokenV22 *FiatTokenV22Caller) Currency(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "currency")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Currency is a free data retrieval call binding the contract method 0xe5a6b10f.
//
// Solidity: function currency() view returns(string)
func (_FiatTokenV22 *FiatTokenV22Session) Currency() (string, error) {
	return _FiatTokenV22.Contract.Currency(&_FiatTokenV22.CallOpts)
}

// Currency is a free data retrieval call binding the contract method 0xe5a6b10f.
//
// Solidity: function currency() view returns(string)
func (_FiatTokenV22 *FiatTokenV22CallerSession) Currency() (string, error) {
	return _FiatTokenV22.Contract.Currency(&_FiatTokenV22.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_FiatTokenV22 *FiatTokenV22Caller) Decimals(opts *bind.CallOpts) (uint8, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "decimals")

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_FiatTokenV22 *FiatTokenV22Session) Decimals() (uint8, error) {
	return _FiatTokenV22.Contract.Decimals(&_FiatTokenV22.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_FiatTokenV22 *FiatTokenV22CallerSession) Decimals() (uint8, error) {
	return _FiatTokenV22.Contract.Decimals(&_FiatTokenV22.CallOpts)
}

// IsBlacklisted is a free data retrieval call binding the contract method 0xfe575a87.
//
// Solidity: function isBlacklisted(address _account) view returns(bool)
func (_FiatTokenV22 *FiatTokenV22Caller) IsBlacklisted(opts *bind.CallOpts, _account common.Address) (bool, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "isBlacklisted", _account)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsBlacklisted is a free data retrieval call binding the contract method 0xfe575a87.
//
// Solidity: function isBlacklisted(address _account) view returns(bool)
func (_FiatTokenV22 *FiatTokenV22Session) IsBlacklisted(_account common.Address) (bool, error) {
	return _FiatTokenV22.Contract.IsBlacklisted(&_FiatTokenV22.CallOpts, _account)
}

// IsBlacklisted is a free data retrieval call binding the contract method 0xfe575a87.
//
// Solidity: function isBlacklisted(address _account) view returns(bool)
func (_FiatTokenV22 *FiatTokenV22CallerSession) IsBlacklisted(_account common.Address) (bool, error) {
	return _FiatTokenV22.Contract.IsBlacklisted(&_FiatTokenV22.CallOpts, _account)
}

// IsMinter is a free data retrieval call binding the contract method 0xaa271e1a.
//
// Solidity: function isMinter(address account) view returns(bool)
func (_FiatTokenV22 *FiatTokenV22Caller) IsMinter(opts *bind.CallOpts, account common.Address) (bool, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "isMinter", account)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsMinter is a free data retrieval call binding the contract method 0xaa271e1a.
//
// Solidity: function isMinter(address account) view returns(bool)
func (_FiatTokenV22 *FiatTokenV22Session) IsMinter(account common.Address) (bool, error) {
	return _FiatTokenV22.Contract.IsMinter(&_FiatTokenV22.CallOpts, account)
}

// IsMinter is a free data retrieval call binding the contract method 0xaa271e1a.
//
// Solidity: function isMinter(address account) view returns(bool)
func (_FiatTokenV22 *FiatTokenV22CallerSession) IsMinter(account common.Address) (bool, error) {
	return _FiatTokenV22.Contract.IsMinter(&_FiatTokenV22.CallOpts, account)
}

// MasterMinter is a free data retrieval call binding the contract method 0x35d99f35.
//
// Solidity: function masterMinter() view returns(address)
func (_FiatTokenV22 *FiatTokenV22Caller) MasterMinter(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "masterMinter")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// MasterMinter is a free data retrieval call binding the contract method 0x35d99f35.
//
// Solidity: function masterMinter() view returns(address)
func (_FiatTokenV22 *FiatTokenV22Session) MasterMinter() (common.Address, error) {
	return _FiatTokenV22.Contract.MasterMinter(&_FiatTokenV22.CallOpts)
}

// MasterMinter is a free data retrieval call binding the contract method 0x35d99f35.
//
// Solidity: function masterMinter() view returns(address)
func (_FiatTokenV22 *FiatTokenV22CallerSession) MasterMinter() (common.Address, error) {
	return _FiatTokenV22.Contract.MasterMinter(&_FiatTokenV22.CallOpts)
}

// MinterAllowance is a free data retrieval call binding the contract method 0x8a6db9c3.
//
// Solidity: function minterAllowance(address minter) view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22Caller) MinterAllowance(opts *bind.CallOpts, minter common.Address) (*big.Int, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "minterAllowance", minter)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MinterAllowance is a free data retrieval call binding the contract method 0x8a6db9c3.
//
// Solidity: function minterAllowance(address minter) view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22Session) MinterAllowance(minter common.Address) (*big.Int, error) {
	return _FiatTokenV22.Contract.MinterAllowance(&_FiatTokenV22.CallOpts, minter)
}

// MinterAllowance is a free data retrieval call binding the contract method 0x8a6db9c3.
//
// Solidity: function minterAllowance(address minter) view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22CallerSession) MinterAllowance(minter common.Address) (*big.Int, error) {
	return _FiatTokenV22.Contract.MinterAllowance(&_FiatTokenV22.CallOpts, minter)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_FiatTokenV22 *FiatTokenV22Caller) Name(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "name")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_FiatTokenV22 *FiatTokenV22Session) Name() (string, error) {
	return _FiatTokenV22.Contract.Name(&_FiatTokenV22.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_FiatTokenV22 *FiatTokenV22CallerSession) Name() (string, error) {
	return _FiatTokenV22.Contract.Name(&_FiatTokenV22.CallOpts)
}

// Nonces is a free data retrieval call binding the contract method 0x7ecebe00.
//
// Solidity: function nonces(address owner) view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22Caller) Nonces(opts *bind.CallOpts, owner common.Address) (*big.Int, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "nonces", owner)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Nonces is a free data retrieval call binding the contract method 0x7ecebe00.
//
// Solidity: function nonces(address owner) view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22Session) Nonces(owner common.Address) (*big.Int, error) {
	return _FiatTokenV22.Contract.Nonces(&_FiatTokenV22.CallOpts, owner)
}

// Nonces is a free data retrieval call binding the contract method 0x7ecebe00.
//
// Solidity: function nonces(address owner) view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22CallerSession) Nonces(owner common.Address) (*big.Int, error) {
	return _FiatTokenV22.Contract.Nonces(&_FiatTokenV22.CallOpts, owner)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_FiatTokenV22 *FiatTokenV22Caller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_FiatTokenV22 *FiatTokenV22Session) Owner() (common.Address, error) {
	return _FiatTokenV22.Contract.Owner(&_FiatTokenV22.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_FiatTokenV22 *FiatTokenV22CallerSession) Owner() (common.Address, error) {
	return _FiatTokenV22.Contract.Owner(&_FiatTokenV22.CallOpts)
}

// Paused is a free data retrieval call binding the contract method 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (_FiatTokenV22 *FiatTokenV22Caller) Paused(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "paused")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// Paused is a free data retrieval call binding the contract method 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (_FiatTokenV22 *FiatTokenV22Session) Paused() (bool, error) {
	return _FiatTokenV22.Contract.Paused(&_FiatTokenV22.CallOpts)
}

// Paused is a free data retrieval call binding the contract method 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (_FiatTokenV22 *FiatTokenV22CallerSession) Paused() (bool, error) {
	return _FiatTokenV22.Contract.Paused(&_FiatTokenV22.CallOpts)
}

// Pauser is a free data retrieval call binding the contract method 0x9fd0506d.
//
// Solidity: function pauser() view returns(address)
func (_FiatTokenV22 *FiatTokenV22Caller) Pauser(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "pauser")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Pauser is a free data retrieval call binding the contract method 0x9fd0506d.
//
// Solidity: function pauser() view returns(address)
func (_FiatTokenV22 *FiatTokenV22Session) Pauser() (common.Address, error) {
	return _FiatTokenV22.Contract.Pauser(&_FiatTokenV22.CallOpts)
}

// Pauser is a free data retrieval call binding the contract method 0x9fd0506d.
//
// Solidity: function pauser() view returns(address)
func (_FiatTokenV22 *FiatTokenV22CallerSession) Pauser() (common.Address, error) {
	return _FiatTokenV22.Contract.Pauser(&_FiatTokenV22.CallOpts)
}

// Rescuer is a free data retrieval call binding the contract method 0x38a63183.
//
// Solidity: function rescuer() view returns(address)
func (_FiatTokenV22 *FiatTokenV22Caller) Rescuer(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "rescuer")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Rescuer is a free data retrieval call binding the contract method 0x38a63183.
//
// Solidity: function rescuer() view returns(address)
func (_FiatTokenV22 *FiatTokenV22Session) Rescuer() (common.Address, error) {
	return _FiatTokenV22.Contract.Rescuer(&_FiatTokenV22.CallOpts)
}

// Rescuer is a free data retrieval call binding the contract method 0x38a63183.
//
// Solidity: function rescuer() view returns(address)
func (_FiatTokenV22 *FiatTokenV22CallerSession) Rescuer() (common.Address, error) {
	return _FiatTokenV22.Contract.Rescuer(&_FiatTokenV22.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_FiatTokenV22 *FiatTokenV22Caller) Symbol(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "symbol")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_FiatTokenV22 *FiatTokenV22Session) Symbol() (string, error) {
	return _FiatTokenV22.Contract.Symbol(&_FiatTokenV22.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_FiatTokenV22 *FiatTokenV22CallerSession) Symbol() (string, error) {
	return _FiatTokenV22.Contract.Symbol(&_FiatTokenV22.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22Caller) TotalSupply(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "totalSupply")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22Session) TotalSupply() (*big.Int, error) {
	return _FiatTokenV22.Contract.TotalSupply(&_FiatTokenV22.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_FiatTokenV22 *FiatTokenV22CallerSession) TotalSupply() (*big.Int, error) {
	return _FiatTokenV22.Contract.TotalSupply(&_FiatTokenV22.CallOpts)
}

// Version is a free data retrieval call binding the contract method 0x54fd4d50.
//
// Solidity: function version() pure returns(string)
func (_FiatTokenV22 *FiatTokenV22Caller) Version(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _FiatTokenV22.contract.Call(opts, &out, "version")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Version is a free data retrieval call binding the contract method 0x54fd4d50.
//
// Solidity: function version() pure returns(string)
func (_FiatTokenV22 *FiatTokenV22Session) Version() (string, error) {
	return _FiatTokenV22.Contract.Version(&_FiatTokenV22.CallOpts)
}

// Version is a free data retrieval call binding the contract method 0x54fd4d50.
//
// Solidity: function version() pure returns(string)
func (_FiatTokenV22 *FiatTokenV22CallerSession) Version() (string, error) {
	return _FiatTokenV22.Contract.Version(&_FiatTokenV22.CallOpts)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Transactor) Approve(opts *bind.TransactOpts, spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "approve", spender, value)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Session) Approve(spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Approve(&_FiatTokenV22.TransactOpts, spender, value)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_FiatTokenV22 *FiatTokenV22TransactorSession) Approve(spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Approve(&_FiatTokenV22.TransactOpts, spender, value)
}

// Blacklist is a paid mutator transaction binding the contract method 0xf9f92be4.
//
// Solidity: function blacklist(address _account) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) Blacklist(opts *bind.TransactOpts, _account common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "blacklist", _account)
}

// Blacklist is a paid mutator transaction binding the contract method 0xf9f92be4.
//
// Solidity: function blacklist(address _account) returns()
func (_FiatTokenV22 *FiatTokenV22Session) Blacklist(_account common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Blacklist(&_FiatTokenV22.TransactOpts, _account)
}

// Blacklist is a paid mutator transaction binding the contract method 0xf9f92be4.
//
// Solidity: function blacklist(address _account) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) Blacklist(_account common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Blacklist(&_FiatTokenV22.TransactOpts, _account)
}

// Burn is a paid mutator transaction binding the contract method 0x42966c68.
//
// Solidity: function burn(uint256 _amount) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) Burn(opts *bind.TransactOpts, _amount *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "burn", _amount)
}

// Burn is a paid mutator transaction binding the contract method 0x42966c68.
//
// Solidity: function burn(uint256 _amount) returns()
func (_FiatTokenV22 *FiatTokenV22Session) Burn(_amount *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Burn(&_FiatTokenV22.TransactOpts, _amount)
}

// Burn is a paid mutator transaction binding the contract method 0x42966c68.
//
// Solidity: function burn(uint256 _amount) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) Burn(_amount *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Burn(&_FiatTokenV22.TransactOpts, _amount)
}

// CancelAuthorization is a paid mutator transaction binding the contract method 0x5a049a70.
//
// Solidity: function cancelAuthorization(address authorizer, bytes32 nonce, uint8 v, bytes32 r, bytes32 s) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) CancelAuthorization(opts *bind.TransactOpts, authorizer common.Address, nonce [32]byte, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "cancelAuthorization", authorizer, nonce, v, r, s)
}

// CancelAuthorization is a paid mutator transaction binding the contract method 0x5a049a70.
//
// Solidity: function cancelAuthorization(address authorizer, bytes32 nonce, uint8 v, bytes32 r, bytes32 s) returns()
func (_FiatTokenV22 *FiatTokenV22Session) CancelAuthorization(authorizer common.Address, nonce [32]byte, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.CancelAuthorization(&_FiatTokenV22.TransactOpts, authorizer, nonce, v, r, s)
}

// CancelAuthorization is a paid mutator transaction binding the contract method 0x5a049a70.
//
// Solidity: function cancelAuthorization(address authorizer, bytes32 nonce, uint8 v, bytes32 r, bytes32 s) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) CancelAuthorization(authorizer common.Address, nonce [32]byte, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.CancelAuthorization(&_FiatTokenV22.TransactOpts, authorizer, nonce, v, r, s)
}

// CancelAuthorization0 is a paid mutator transaction binding the contract method 0xb7b72899.
//
// Solidity: function cancelAuthorization(address authorizer, bytes32 nonce, bytes signature) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) CancelAuthorization0(opts *bind.TransactOpts, authorizer common.Address, nonce [32]byte, signature []byte) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "cancelAuthorization0", authorizer, nonce, signature)
}

// CancelAuthorization0 is a paid mutator transaction binding the contract method 0xb7b72899.
//
// Solidity: function cancelAuthorization(address authorizer, bytes32 nonce, bytes signature) returns()
func (_FiatTokenV22 *FiatTokenV22Session) CancelAuthorization0(authorizer common.Address, nonce [32]byte, signature []byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.CancelAuthorization0(&_FiatTokenV22.TransactOpts, authorizer, nonce, signature)
}

// CancelAuthorization0 is a paid mutator transaction binding the contract method 0xb7b72899.
//
// Solidity: function cancelAuthorization(address authorizer, bytes32 nonce, bytes signature) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) CancelAuthorization0(authorizer common.Address, nonce [32]byte, signature []byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.CancelAuthorization0(&_FiatTokenV22.TransactOpts, authorizer, nonce, signature)
}

// ConfigureMinter is a paid mutator transaction binding the contract method 0x4e44d956.
//
// Solidity: function configureMinter(address minter, uint256 minterAllowedAmount) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Transactor) ConfigureMinter(opts *bind.TransactOpts, minter common.Address, minterAllowedAmount *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "configureMinter", minter, minterAllowedAmount)
}

// ConfigureMinter is a paid mutator transaction binding the contract method 0x4e44d956.
//
// Solidity: function configureMinter(address minter, uint256 minterAllowedAmount) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Session) ConfigureMinter(minter common.Address, minterAllowedAmount *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.ConfigureMinter(&_FiatTokenV22.TransactOpts, minter, minterAllowedAmount)
}

// ConfigureMinter is a paid mutator transaction binding the contract method 0x4e44d956.
//
// Solidity: function configureMinter(address minter, uint256 minterAllowedAmount) returns(bool)
func (_FiatTokenV22 *FiatTokenV22TransactorSession) ConfigureMinter(minter common.Address, minterAllowedAmount *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.ConfigureMinter(&_FiatTokenV22.TransactOpts, minter, minterAllowedAmount)
}

// DecreaseAllowance is a paid mutator transaction binding the contract method 0xa457c2d7.
//
// Solidity: function decreaseAllowance(address spender, uint256 decrement) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Transactor) DecreaseAllowance(opts *bind.TransactOpts, spender common.Address, decrement *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "decreaseAllowance", spender, decrement)
}

// DecreaseAllowance is a paid mutator transaction binding the contract method 0xa457c2d7.
//
// Solidity: function decreaseAllowance(address spender, uint256 decrement) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Session) DecreaseAllowance(spender common.Address, decrement *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.DecreaseAllowance(&_FiatTokenV22.TransactOpts, spender, decrement)
}

// DecreaseAllowance is a paid mutator transaction binding the contract method 0xa457c2d7.
//
// Solidity: function decreaseAllowance(address spender, uint256 decrement) returns(bool)
func (_FiatTokenV22 *FiatTokenV22TransactorSession) DecreaseAllowance(spender common.Address, decrement *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.DecreaseAllowance(&_FiatTokenV22.TransactOpts, spender, decrement)
}

// IncreaseAllowance is a paid mutator transaction binding the contract method 0x39509351.
//
// Solidity: function increaseAllowance(address spender, uint256 increment) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Transactor) IncreaseAllowance(opts *bind.TransactOpts, spender common.Address, increment *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "increaseAllowance", spender, increment)
}

// IncreaseAllowance is a paid mutator transaction binding the contract method 0x39509351.
//
// Solidity: function increaseAllowance(address spender, uint256 increment) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Session) IncreaseAllowance(spender common.Address, increment *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.IncreaseAllowance(&_FiatTokenV22.TransactOpts, spender, increment)
}

// IncreaseAllowance is a paid mutator transaction binding the contract method 0x39509351.
//
// Solidity: function increaseAllowance(address spender, uint256 increment) returns(bool)
func (_FiatTokenV22 *FiatTokenV22TransactorSession) IncreaseAllowance(spender common.Address, increment *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.IncreaseAllowance(&_FiatTokenV22.TransactOpts, spender, increment)
}

// Initialize is a paid mutator transaction binding the contract method 0x3357162b.
//
// Solidity: function initialize(string tokenName, string tokenSymbol, string tokenCurrency, uint8 tokenDecimals, address newMasterMinter, address newPauser, address newBlacklister, address newOwner) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) Initialize(opts *bind.TransactOpts, tokenName string, tokenSymbol string, tokenCurrency string, tokenDecimals uint8, newMasterMinter common.Address, newPauser common.Address, newBlacklister common.Address, newOwner common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "initialize", tokenName, tokenSymbol, tokenCurrency, tokenDecimals, newMasterMinter, newPauser, newBlacklister, newOwner)
}

// Initialize is a paid mutator transaction binding the contract method 0x3357162b.
//
// Solidity: function initialize(string tokenName, string tokenSymbol, string tokenCurrency, uint8 tokenDecimals, address newMasterMinter, address newPauser, address newBlacklister, address newOwner) returns()
func (_FiatTokenV22 *FiatTokenV22Session) Initialize(tokenName string, tokenSymbol string, tokenCurrency string, tokenDecimals uint8, newMasterMinter common.Address, newPauser common.Address, newBlacklister common.Address, newOwner common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Initialize(&_FiatTokenV22.TransactOpts, tokenName, tokenSymbol, tokenCurrency, tokenDecimals, newMasterMinter, newPauser, newBlacklister, newOwner)
}

// Initialize is a paid mutator transaction binding the contract method 0x3357162b.
//
// Solidity: function initialize(string tokenName, string tokenSymbol, string tokenCurrency, uint8 tokenDecimals, address newMasterMinter, address newPauser, address newBlacklister, address newOwner) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) Initialize(tokenName string, tokenSymbol string, tokenCurrency string, tokenDecimals uint8, newMasterMinter common.Address, newPauser common.Address, newBlacklister common.Address, newOwner common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Initialize(&_FiatTokenV22.TransactOpts, tokenName, tokenSymbol, tokenCurrency, tokenDecimals, newMasterMinter, newPauser, newBlacklister, newOwner)
}

// InitializeV2 is a paid mutator transaction binding the contract method 0xd608ea64.
//
// Solidity: function initializeV2(string newName) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) InitializeV2(opts *bind.TransactOpts, newName string) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "initializeV2", newName)
}

// InitializeV2 is a paid mutator transaction binding the contract method 0xd608ea64.
//
// Solidity: function initializeV2(string newName) returns()
func (_FiatTokenV22 *FiatTokenV22Session) InitializeV2(newName string) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.InitializeV2(&_FiatTokenV22.TransactOpts, newName)
}

// InitializeV2 is a paid mutator transaction binding the contract method 0xd608ea64.
//
// Solidity: function initializeV2(string newName) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) InitializeV2(newName string) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.InitializeV2(&_FiatTokenV22.TransactOpts, newName)
}

// InitializeV21 is a paid mutator transaction binding the contract method 0x2fc81e09.
//
// Solidity: function initializeV2_1(address lostAndFound) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) InitializeV21(opts *bind.TransactOpts, lostAndFound common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "initializeV2_1", lostAndFound)
}

// InitializeV21 is a paid mutator transaction binding the contract method 0x2fc81e09.
//
// Solidity: function initializeV2_1(address lostAndFound) returns()
func (_FiatTokenV22 *FiatTokenV22Session) InitializeV21(lostAndFound common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.InitializeV21(&_FiatTokenV22.TransactOpts, lostAndFound)
}

// InitializeV21 is a paid mutator transaction binding the contract method 0x2fc81e09.
//
// Solidity: function initializeV2_1(address lostAndFound) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) InitializeV21(lostAndFound common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.InitializeV21(&_FiatTokenV22.TransactOpts, lostAndFound)
}

// InitializeV22 is a paid mutator transaction binding the contract method 0x430239b4.
//
// Solidity: function initializeV2_2(address[] accountsToBlacklist, string newSymbol) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) InitializeV22(opts *bind.TransactOpts, accountsToBlacklist []common.Address, newSymbol string) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "initializeV2_2", accountsToBlacklist, newSymbol)
}

// InitializeV22 is a paid mutator transaction binding the contract method 0x430239b4.
//
// Solidity: function initializeV2_2(address[] accountsToBlacklist, string newSymbol) returns()
func (_FiatTokenV22 *FiatTokenV22Session) InitializeV22(accountsToBlacklist []common.Address, newSymbol string) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.InitializeV22(&_FiatTokenV22.TransactOpts, accountsToBlacklist, newSymbol)
}

// InitializeV22 is a paid mutator transaction binding the contract method 0x430239b4.
//
// Solidity: function initializeV2_2(address[] accountsToBlacklist, string newSymbol) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) InitializeV22(accountsToBlacklist []common.Address, newSymbol string) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.InitializeV22(&_FiatTokenV22.TransactOpts, accountsToBlacklist, newSymbol)
}

// Mint is a paid mutator transaction binding the contract method 0x40c10f19.
//
// Solidity: function mint(address _to, uint256 _amount) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Transactor) Mint(opts *bind.TransactOpts, _to common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "mint", _to, _amount)
}

// Mint is a paid mutator transaction binding the contract method 0x40c10f19.
//
// Solidity: function mint(address _to, uint256 _amount) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Session) Mint(_to common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Mint(&_FiatTokenV22.TransactOpts, _to, _amount)
}

// Mint is a paid mutator transaction binding the contract method 0x40c10f19.
//
// Solidity: function mint(address _to, uint256 _amount) returns(bool)
func (_FiatTokenV22 *FiatTokenV22TransactorSession) Mint(_to common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Mint(&_FiatTokenV22.TransactOpts, _to, _amount)
}

// Pause is a paid mutator transaction binding the contract method 0x8456cb59.
//
// Solidity: function pause() returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) Pause(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "pause")
}

// Pause is a paid mutator transaction binding the contract method 0x8456cb59.
//
// Solidity: function pause() returns()
func (_FiatTokenV22 *FiatTokenV22Session) Pause() (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Pause(&_FiatTokenV22.TransactOpts)
}

// Pause is a paid mutator transaction binding the contract method 0x8456cb59.
//
// Solidity: function pause() returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) Pause() (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Pause(&_FiatTokenV22.TransactOpts)
}

// Permit is a paid mutator transaction binding the contract method 0x9fd5a6cf.
//
// Solidity: function permit(address owner, address spender, uint256 value, uint256 deadline, bytes signature) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) Permit(opts *bind.TransactOpts, owner common.Address, spender common.Address, value *big.Int, deadline *big.Int, signature []byte) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "permit", owner, spender, value, deadline, signature)
}

// Permit is a paid mutator transaction binding the contract method 0x9fd5a6cf.
//
// Solidity: function permit(address owner, address spender, uint256 value, uint256 deadline, bytes signature) returns()
func (_FiatTokenV22 *FiatTokenV22Session) Permit(owner common.Address, spender common.Address, value *big.Int, deadline *big.Int, signature []byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Permit(&_FiatTokenV22.TransactOpts, owner, spender, value, deadline, signature)
}

// Permit is a paid mutator transaction binding the contract method 0x9fd5a6cf.
//
// Solidity: function permit(address owner, address spender, uint256 value, uint256 deadline, bytes signature) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) Permit(owner common.Address, spender common.Address, value *big.Int, deadline *big.Int, signature []byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Permit(&_FiatTokenV22.TransactOpts, owner, spender, value, deadline, signature)
}

// Permit0 is a paid mutator transaction binding the contract method 0xd505accf.
//
// Solidity: function permit(address owner, address spender, uint256 value, uint256 deadline, uint8 v, bytes32 r, bytes32 s) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) Permit0(opts *bind.TransactOpts, owner common.Address, spender common.Address, value *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "permit0", owner, spender, value, deadline, v, r, s)
}

// Permit0 is a paid mutator transaction binding the contract method 0xd505accf.
//
// Solidity: function permit(address owner, address spender, uint256 value, uint256 deadline, uint8 v, bytes32 r, bytes32 s) returns()
func (_FiatTokenV22 *FiatTokenV22Session) Permit0(owner common.Address, spender common.Address, value *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Permit0(&_FiatTokenV22.TransactOpts, owner, spender, value, deadline, v, r, s)
}

// Permit0 is a paid mutator transaction binding the contract method 0xd505accf.
//
// Solidity: function permit(address owner, address spender, uint256 value, uint256 deadline, uint8 v, bytes32 r, bytes32 s) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) Permit0(owner common.Address, spender common.Address, value *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Permit0(&_FiatTokenV22.TransactOpts, owner, spender, value, deadline, v, r, s)
}

// ReceiveWithAuthorization is a paid mutator transaction binding the contract method 0x88b7ab63.
//
// Solidity: function receiveWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce, bytes signature) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) ReceiveWithAuthorization(opts *bind.TransactOpts, from common.Address, to common.Address, value *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, signature []byte) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "receiveWithAuthorization", from, to, value, validAfter, validBefore, nonce, signature)
}

// ReceiveWithAuthorization is a paid mutator transaction binding the contract method 0x88b7ab63.
//
// Solidity: function receiveWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce, bytes signature) returns()
func (_FiatTokenV22 *FiatTokenV22Session) ReceiveWithAuthorization(from common.Address, to common.Address, value *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, signature []byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.ReceiveWithAuthorization(&_FiatTokenV22.TransactOpts, from, to, value, validAfter, validBefore, nonce, signature)
}

// ReceiveWithAuthorization is a paid mutator transaction binding the contract method 0x88b7ab63.
//
// Solidity: function receiveWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce, bytes signature) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) ReceiveWithAuthorization(from common.Address, to common.Address, value *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, signature []byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.ReceiveWithAuthorization(&_FiatTokenV22.TransactOpts, from, to, value, validAfter, validBefore, nonce, signature)
}

// ReceiveWithAuthorization0 is a paid mutator transaction binding the contract method 0xef55bec6.
//
// Solidity: function receiveWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) ReceiveWithAuthorization0(opts *bind.TransactOpts, from common.Address, to common.Address, value *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "receiveWithAuthorization0", from, to, value, validAfter, validBefore, nonce, v, r, s)
}

// ReceiveWithAuthorization0 is a paid mutator transaction binding the contract method 0xef55bec6.
//
// Solidity: function receiveWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s) returns()
func (_FiatTokenV22 *FiatTokenV22Session) ReceiveWithAuthorization0(from common.Address, to common.Address, value *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.ReceiveWithAuthorization0(&_FiatTokenV22.TransactOpts, from, to, value, validAfter, validBefore, nonce, v, r, s)
}

// ReceiveWithAuthorization0 is a paid mutator transaction binding the contract method 0xef55bec6.
//
// Solidity: function receiveWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) ReceiveWithAuthorization0(from common.Address, to common.Address, value *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.ReceiveWithAuthorization0(&_FiatTokenV22.TransactOpts, from, to, value, validAfter, validBefore, nonce, v, r, s)
}

// RemoveMinter is a paid mutator transaction binding the contract method 0x3092afd5.
//
// Solidity: function removeMinter(address minter) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Transactor) RemoveMinter(opts *bind.TransactOpts, minter common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "removeMinter", minter)
}

// RemoveMinter is a paid mutator transaction binding the contract method 0x3092afd5.
//
// Solidity: function removeMinter(address minter) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Session) RemoveMinter(minter common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.RemoveMinter(&_FiatTokenV22.TransactOpts, minter)
}

// RemoveMinter is a paid mutator transaction binding the contract method 0x3092afd5.
//
// Solidity: function removeMinter(address minter) returns(bool)
func (_FiatTokenV22 *FiatTokenV22TransactorSession) RemoveMinter(minter common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.RemoveMinter(&_FiatTokenV22.TransactOpts, minter)
}

// RescueERC20 is a paid mutator transaction binding the contract method 0xb2118a8d.
//
// Solidity: function rescueERC20(address tokenContract, address to, uint256 amount) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) RescueERC20(opts *bind.TransactOpts, tokenContract common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "rescueERC20", tokenContract, to, amount)
}

// RescueERC20 is a paid mutator transaction binding the contract method 0xb2118a8d.
//
// Solidity: function rescueERC20(address tokenContract, address to, uint256 amount) returns()
func (_FiatTokenV22 *FiatTokenV22Session) RescueERC20(tokenContract common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.RescueERC20(&_FiatTokenV22.TransactOpts, tokenContract, to, amount)
}

// RescueERC20 is a paid mutator transaction binding the contract method 0xb2118a8d.
//
// Solidity: function rescueERC20(address tokenContract, address to, uint256 amount) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) RescueERC20(tokenContract common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.RescueERC20(&_FiatTokenV22.TransactOpts, tokenContract, to, amount)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Transactor) Transfer(opts *bind.TransactOpts, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "transfer", to, value)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Session) Transfer(to common.Address, value *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Transfer(&_FiatTokenV22.TransactOpts, to, value)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_FiatTokenV22 *FiatTokenV22TransactorSession) Transfer(to common.Address, value *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Transfer(&_FiatTokenV22.TransactOpts, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Transactor) TransferFrom(opts *bind.TransactOpts, from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "transferFrom", from, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_FiatTokenV22 *FiatTokenV22Session) TransferFrom(from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.TransferFrom(&_FiatTokenV22.TransactOpts, from, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_FiatTokenV22 *FiatTokenV22TransactorSession) TransferFrom(from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.TransferFrom(&_FiatTokenV22.TransactOpts, from, to, value)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_FiatTokenV22 *FiatTokenV22Session) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.TransferOwnership(&_FiatTokenV22.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.TransferOwnership(&_FiatTokenV22.TransactOpts, newOwner)
}

// TransferWithAuthorization is a paid mutator transaction binding the contract method 0xcf092995.
//
// Solidity: function transferWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce, bytes signature) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) TransferWithAuthorization(opts *bind.TransactOpts, from common.Address, to common.Address, value *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, signature []byte) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "transferWithAuthorization", from, to, value, validAfter, validBefore, nonce, signature)
}

// TransferWithAuthorization is a paid mutator transaction binding the contract method 0xcf092995.
//
// Solidity: function transferWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce, bytes signature) returns()
func (_FiatTokenV22 *FiatTokenV22Session) TransferWithAuthorization(from common.Address, to common.Address, value *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, signature []byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.TransferWithAuthorization(&_FiatTokenV22.TransactOpts, from, to, value, validAfter, validBefore, nonce, signature)
}

// TransferWithAuthorization is a paid mutator transaction binding the contract method 0xcf092995.
//
// Solidity: function transferWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce, bytes signature) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) TransferWithAuthorization(from common.Address, to common.Address, value *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, signature []byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.TransferWithAuthorization(&_FiatTokenV22.TransactOpts, from, to, value, validAfter, validBefore, nonce, signature)
}

// TransferWithAuthorization0 is a paid mutator transaction binding the contract method 0xe3ee160e.
//
// Solidity: function transferWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) TransferWithAuthorization0(opts *bind.TransactOpts, from common.Address, to common.Address, value *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "transferWithAuthorization0", from, to, value, validAfter, validBefore, nonce, v, r, s)
}

// TransferWithAuthorization0 is a paid mutator transaction binding the contract method 0xe3ee160e.
//
// Solidity: function transferWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s) returns()
func (_FiatTokenV22 *FiatTokenV22Session) TransferWithAuthorization0(from common.Address, to common.Address, value *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.TransferWithAuthorization0(&_FiatTokenV22.TransactOpts, from, to, value, validAfter, validBefore, nonce, v, r, s)
}

// TransferWithAuthorization0 is a paid mutator transaction binding the contract method 0xe3ee160e.
//
// Solidity: function transferWithAuthorization(address from, address to, uint256 value, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) TransferWithAuthorization0(from common.Address, to common.Address, value *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.TransferWithAuthorization0(&_FiatTokenV22.TransactOpts, from, to, value, validAfter, validBefore, nonce, v, r, s)
}

// UnBlacklist is a paid mutator transaction binding the contract method 0x1a895266.
//
// Solidity: function unBlacklist(address _account) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) UnBlacklist(opts *bind.TransactOpts, _account common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "unBlacklist", _account)
}

// UnBlacklist is a paid mutator transaction binding the contract method 0x1a895266.
//
// Solidity: function unBlacklist(address _account) returns()
func (_FiatTokenV22 *FiatTokenV22Session) UnBlacklist(_account common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.UnBlacklist(&_FiatTokenV22.TransactOpts, _account)
}

// UnBlacklist is a paid mutator transaction binding the contract method 0x1a895266.
//
// Solidity: function unBlacklist(address _account) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) UnBlacklist(_account common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.UnBlacklist(&_FiatTokenV22.TransactOpts, _account)
}

// Unpause is a paid mutator transaction binding the contract method 0x3f4ba83a.
//
// Solidity: function unpause() returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) Unpause(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "unpause")
}

// Unpause is a paid mutator transaction binding the contract method 0x3f4ba83a.
//
// Solidity: function unpause() returns()
func (_FiatTokenV22 *FiatTokenV22Session) Unpause() (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Unpause(&_FiatTokenV22.TransactOpts)
}

// Unpause is a paid mutator transaction binding the contract method 0x3f4ba83a.
//
// Solidity: function unpause() returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) Unpause() (*types.Transaction, error) {
	return _FiatTokenV22.Contract.Unpause(&_FiatTokenV22.TransactOpts)
}

// UpdateBlacklister is a paid mutator transaction binding the contract method 0xad38bf22.
//
// Solidity: function updateBlacklister(address _newBlacklister) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) UpdateBlacklister(opts *bind.TransactOpts, _newBlacklister common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "updateBlacklister", _newBlacklister)
}

// UpdateBlacklister is a paid mutator transaction binding the contract method 0xad38bf22.
//
// Solidity: function updateBlacklister(address _newBlacklister) returns()
func (_FiatTokenV22 *FiatTokenV22Session) UpdateBlacklister(_newBlacklister common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.UpdateBlacklister(&_FiatTokenV22.TransactOpts, _newBlacklister)
}

// UpdateBlacklister is a paid mutator transaction binding the contract method 0xad38bf22.
//
// Solidity: function updateBlacklister(address _newBlacklister) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) UpdateBlacklister(_newBlacklister common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.UpdateBlacklister(&_FiatTokenV22.TransactOpts, _newBlacklister)
}

// UpdateMasterMinter is a paid mutator transaction binding the contract method 0xaa20e1e4.
//
// Solidity: function updateMasterMinter(address _newMasterMinter) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) UpdateMasterMinter(opts *bind.TransactOpts, _newMasterMinter common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "updateMasterMinter", _newMasterMinter)
}

// UpdateMasterMinter is a paid mutator transaction binding the contract method 0xaa20e1e4.
//
// Solidity: function updateMasterMinter(address _newMasterMinter) returns()
func (_FiatTokenV22 *FiatTokenV22Session) UpdateMasterMinter(_newMasterMinter common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.UpdateMasterMinter(&_FiatTokenV22.TransactOpts, _newMasterMinter)
}

// UpdateMasterMinter is a paid mutator transaction binding the contract method 0xaa20e1e4.
//
// Solidity: function updateMasterMinter(address _newMasterMinter) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) UpdateMasterMinter(_newMasterMinter common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.UpdateMasterMinter(&_FiatTokenV22.TransactOpts, _newMasterMinter)
}

// UpdatePauser is a paid mutator transaction binding the contract method 0x554bab3c.
//
// Solidity: function updatePauser(address _newPauser) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) UpdatePauser(opts *bind.TransactOpts, _newPauser common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "updatePauser", _newPauser)
}

// UpdatePauser is a paid mutator transaction binding the contract method 0x554bab3c.
//
// Solidity: function updatePauser(address _newPauser) returns()
func (_FiatTokenV22 *FiatTokenV22Session) UpdatePauser(_newPauser common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.UpdatePauser(&_FiatTokenV22.TransactOpts, _newPauser)
}

// UpdatePauser is a paid mutator transaction binding the contract method 0x554bab3c.
//
// Solidity: function updatePauser(address _newPauser) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) UpdatePauser(_newPauser common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.UpdatePauser(&_FiatTokenV22.TransactOpts, _newPauser)
}

// UpdateRescuer is a paid mutator transaction binding the contract method 0x2ab60045.
//
// Solidity: function updateRescuer(address newRescuer) returns()
func (_FiatTokenV22 *FiatTokenV22Transactor) UpdateRescuer(opts *bind.TransactOpts, newRescuer common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.contract.Transact(opts, "updateRescuer", newRescuer)
}

// UpdateRescuer is a paid mutator transaction binding the contract method 0x2ab60045.
//
// Solidity: function updateRescuer(address newRescuer) returns()
func (_FiatTokenV22 *FiatTokenV22Session) UpdateRescuer(newRescuer common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.UpdateRescuer(&_FiatTokenV22.TransactOpts, newRescuer)
}

// UpdateRescuer is a paid mutator transaction binding the contract method 0x2ab60045.
//
// Solidity: function updateRescuer(address newRescuer) returns()
func (_FiatTokenV22 *FiatTokenV22TransactorSession) UpdateRescuer(newRescuer common.Address) (*types.Transaction, error) {
	return _FiatTokenV22.Contract.UpdateRescuer(&_FiatTokenV22.TransactOpts, newRescuer)
}

// FiatTokenV22ApprovalIterator is returned from FilterApproval and is used to iterate over the raw logs and unpacked data for Approval events raised by the FiatTokenV22 contract.
type FiatTokenV22ApprovalIterator struct {
	Event *FiatTokenV22Approval // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22ApprovalIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22Approval)
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
		it.Event = new(FiatTokenV22Approval)
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
func (it *FiatTokenV22ApprovalIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22ApprovalIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22Approval represents a Approval event raised by the FiatTokenV22 contract.
type FiatTokenV22Approval struct {
	Owner   common.Address
	Spender common.Address
	Value   *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterApproval is a free log retrieval operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterApproval(opts *bind.FilterOpts, owner []common.Address, spender []common.Address) (*FiatTokenV22ApprovalIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22ApprovalIterator{contract: _FiatTokenV22.contract, event: "Approval", logs: logs, sub: sub}, nil
}

// WatchApproval is a free log subscription operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchApproval(opts *bind.WatchOpts, sink chan<- *FiatTokenV22Approval, owner []common.Address, spender []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22Approval)
				if err := _FiatTokenV22.contract.UnpackLog(event, "Approval", log); err != nil {
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
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseApproval(log types.Log) (*FiatTokenV22Approval, error) {
	event := new(FiatTokenV22Approval)
	if err := _FiatTokenV22.contract.UnpackLog(event, "Approval", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22AuthorizationCanceledIterator is returned from FilterAuthorizationCanceled and is used to iterate over the raw logs and unpacked data for AuthorizationCanceled events raised by the FiatTokenV22 contract.
type FiatTokenV22AuthorizationCanceledIterator struct {
	Event *FiatTokenV22AuthorizationCanceled // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22AuthorizationCanceledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22AuthorizationCanceled)
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
		it.Event = new(FiatTokenV22AuthorizationCanceled)
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
func (it *FiatTokenV22AuthorizationCanceledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22AuthorizationCanceledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22AuthorizationCanceled represents a AuthorizationCanceled event raised by the FiatTokenV22 contract.
type FiatTokenV22AuthorizationCanceled struct {
	Authorizer common.Address
	Nonce      [32]byte
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterAuthorizationCanceled is a free log retrieval operation binding the contract event 0x1cdd46ff242716cdaa72d159d339a485b3438398348d68f09d7c8c0a59353d81.
//
// Solidity: event AuthorizationCanceled(address indexed authorizer, bytes32 indexed nonce)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterAuthorizationCanceled(opts *bind.FilterOpts, authorizer []common.Address, nonce [][32]byte) (*FiatTokenV22AuthorizationCanceledIterator, error) {

	var authorizerRule []interface{}
	for _, authorizerItem := range authorizer {
		authorizerRule = append(authorizerRule, authorizerItem)
	}
	var nonceRule []interface{}
	for _, nonceItem := range nonce {
		nonceRule = append(nonceRule, nonceItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "AuthorizationCanceled", authorizerRule, nonceRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22AuthorizationCanceledIterator{contract: _FiatTokenV22.contract, event: "AuthorizationCanceled", logs: logs, sub: sub}, nil
}

// WatchAuthorizationCanceled is a free log subscription operation binding the contract event 0x1cdd46ff242716cdaa72d159d339a485b3438398348d68f09d7c8c0a59353d81.
//
// Solidity: event AuthorizationCanceled(address indexed authorizer, bytes32 indexed nonce)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchAuthorizationCanceled(opts *bind.WatchOpts, sink chan<- *FiatTokenV22AuthorizationCanceled, authorizer []common.Address, nonce [][32]byte) (event.Subscription, error) {

	var authorizerRule []interface{}
	for _, authorizerItem := range authorizer {
		authorizerRule = append(authorizerRule, authorizerItem)
	}
	var nonceRule []interface{}
	for _, nonceItem := range nonce {
		nonceRule = append(nonceRule, nonceItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "AuthorizationCanceled", authorizerRule, nonceRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22AuthorizationCanceled)
				if err := _FiatTokenV22.contract.UnpackLog(event, "AuthorizationCanceled", log); err != nil {
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

// ParseAuthorizationCanceled is a log parse operation binding the contract event 0x1cdd46ff242716cdaa72d159d339a485b3438398348d68f09d7c8c0a59353d81.
//
// Solidity: event AuthorizationCanceled(address indexed authorizer, bytes32 indexed nonce)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseAuthorizationCanceled(log types.Log) (*FiatTokenV22AuthorizationCanceled, error) {
	event := new(FiatTokenV22AuthorizationCanceled)
	if err := _FiatTokenV22.contract.UnpackLog(event, "AuthorizationCanceled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22AuthorizationUsedIterator is returned from FilterAuthorizationUsed and is used to iterate over the raw logs and unpacked data for AuthorizationUsed events raised by the FiatTokenV22 contract.
type FiatTokenV22AuthorizationUsedIterator struct {
	Event *FiatTokenV22AuthorizationUsed // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22AuthorizationUsedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22AuthorizationUsed)
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
		it.Event = new(FiatTokenV22AuthorizationUsed)
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
func (it *FiatTokenV22AuthorizationUsedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22AuthorizationUsedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22AuthorizationUsed represents a AuthorizationUsed event raised by the FiatTokenV22 contract.
type FiatTokenV22AuthorizationUsed struct {
	Authorizer common.Address
	Nonce      [32]byte
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterAuthorizationUsed is a free log retrieval operation binding the contract event 0x98de503528ee59b575ef0c0a2576a82497bfc029a5685b209e9ec333479b10a5.
//
// Solidity: event AuthorizationUsed(address indexed authorizer, bytes32 indexed nonce)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterAuthorizationUsed(opts *bind.FilterOpts, authorizer []common.Address, nonce [][32]byte) (*FiatTokenV22AuthorizationUsedIterator, error) {

	var authorizerRule []interface{}
	for _, authorizerItem := range authorizer {
		authorizerRule = append(authorizerRule, authorizerItem)
	}
	var nonceRule []interface{}
	for _, nonceItem := range nonce {
		nonceRule = append(nonceRule, nonceItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "AuthorizationUsed", authorizerRule, nonceRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22AuthorizationUsedIterator{contract: _FiatTokenV22.contract, event: "AuthorizationUsed", logs: logs, sub: sub}, nil
}

// WatchAuthorizationUsed is a free log subscription operation binding the contract event 0x98de503528ee59b575ef0c0a2576a82497bfc029a5685b209e9ec333479b10a5.
//
// Solidity: event AuthorizationUsed(address indexed authorizer, bytes32 indexed nonce)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchAuthorizationUsed(opts *bind.WatchOpts, sink chan<- *FiatTokenV22AuthorizationUsed, authorizer []common.Address, nonce [][32]byte) (event.Subscription, error) {

	var authorizerRule []interface{}
	for _, authorizerItem := range authorizer {
		authorizerRule = append(authorizerRule, authorizerItem)
	}
	var nonceRule []interface{}
	for _, nonceItem := range nonce {
		nonceRule = append(nonceRule, nonceItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "AuthorizationUsed", authorizerRule, nonceRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22AuthorizationUsed)
				if err := _FiatTokenV22.contract.UnpackLog(event, "AuthorizationUsed", log); err != nil {
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

// ParseAuthorizationUsed is a log parse operation binding the contract event 0x98de503528ee59b575ef0c0a2576a82497bfc029a5685b209e9ec333479b10a5.
//
// Solidity: event AuthorizationUsed(address indexed authorizer, bytes32 indexed nonce)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseAuthorizationUsed(log types.Log) (*FiatTokenV22AuthorizationUsed, error) {
	event := new(FiatTokenV22AuthorizationUsed)
	if err := _FiatTokenV22.contract.UnpackLog(event, "AuthorizationUsed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22BlacklistedIterator is returned from FilterBlacklisted and is used to iterate over the raw logs and unpacked data for Blacklisted events raised by the FiatTokenV22 contract.
type FiatTokenV22BlacklistedIterator struct {
	Event *FiatTokenV22Blacklisted // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22BlacklistedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22Blacklisted)
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
		it.Event = new(FiatTokenV22Blacklisted)
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
func (it *FiatTokenV22BlacklistedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22BlacklistedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22Blacklisted represents a Blacklisted event raised by the FiatTokenV22 contract.
type FiatTokenV22Blacklisted struct {
	Account common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterBlacklisted is a free log retrieval operation binding the contract event 0xffa4e6181777692565cf28528fc88fd1516ea86b56da075235fa575af6a4b855.
//
// Solidity: event Blacklisted(address indexed _account)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterBlacklisted(opts *bind.FilterOpts, _account []common.Address) (*FiatTokenV22BlacklistedIterator, error) {

	var _accountRule []interface{}
	for _, _accountItem := range _account {
		_accountRule = append(_accountRule, _accountItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "Blacklisted", _accountRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22BlacklistedIterator{contract: _FiatTokenV22.contract, event: "Blacklisted", logs: logs, sub: sub}, nil
}

// WatchBlacklisted is a free log subscription operation binding the contract event 0xffa4e6181777692565cf28528fc88fd1516ea86b56da075235fa575af6a4b855.
//
// Solidity: event Blacklisted(address indexed _account)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchBlacklisted(opts *bind.WatchOpts, sink chan<- *FiatTokenV22Blacklisted, _account []common.Address) (event.Subscription, error) {

	var _accountRule []interface{}
	for _, _accountItem := range _account {
		_accountRule = append(_accountRule, _accountItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "Blacklisted", _accountRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22Blacklisted)
				if err := _FiatTokenV22.contract.UnpackLog(event, "Blacklisted", log); err != nil {
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

// ParseBlacklisted is a log parse operation binding the contract event 0xffa4e6181777692565cf28528fc88fd1516ea86b56da075235fa575af6a4b855.
//
// Solidity: event Blacklisted(address indexed _account)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseBlacklisted(log types.Log) (*FiatTokenV22Blacklisted, error) {
	event := new(FiatTokenV22Blacklisted)
	if err := _FiatTokenV22.contract.UnpackLog(event, "Blacklisted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22BlacklisterChangedIterator is returned from FilterBlacklisterChanged and is used to iterate over the raw logs and unpacked data for BlacklisterChanged events raised by the FiatTokenV22 contract.
type FiatTokenV22BlacklisterChangedIterator struct {
	Event *FiatTokenV22BlacklisterChanged // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22BlacklisterChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22BlacklisterChanged)
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
		it.Event = new(FiatTokenV22BlacklisterChanged)
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
func (it *FiatTokenV22BlacklisterChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22BlacklisterChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22BlacklisterChanged represents a BlacklisterChanged event raised by the FiatTokenV22 contract.
type FiatTokenV22BlacklisterChanged struct {
	NewBlacklister common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterBlacklisterChanged is a free log retrieval operation binding the contract event 0xc67398012c111ce95ecb7429b933096c977380ee6c421175a71a4a4c6c88c06e.
//
// Solidity: event BlacklisterChanged(address indexed newBlacklister)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterBlacklisterChanged(opts *bind.FilterOpts, newBlacklister []common.Address) (*FiatTokenV22BlacklisterChangedIterator, error) {

	var newBlacklisterRule []interface{}
	for _, newBlacklisterItem := range newBlacklister {
		newBlacklisterRule = append(newBlacklisterRule, newBlacklisterItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "BlacklisterChanged", newBlacklisterRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22BlacklisterChangedIterator{contract: _FiatTokenV22.contract, event: "BlacklisterChanged", logs: logs, sub: sub}, nil
}

// WatchBlacklisterChanged is a free log subscription operation binding the contract event 0xc67398012c111ce95ecb7429b933096c977380ee6c421175a71a4a4c6c88c06e.
//
// Solidity: event BlacklisterChanged(address indexed newBlacklister)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchBlacklisterChanged(opts *bind.WatchOpts, sink chan<- *FiatTokenV22BlacklisterChanged, newBlacklister []common.Address) (event.Subscription, error) {

	var newBlacklisterRule []interface{}
	for _, newBlacklisterItem := range newBlacklister {
		newBlacklisterRule = append(newBlacklisterRule, newBlacklisterItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "BlacklisterChanged", newBlacklisterRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22BlacklisterChanged)
				if err := _FiatTokenV22.contract.UnpackLog(event, "BlacklisterChanged", log); err != nil {
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

// ParseBlacklisterChanged is a log parse operation binding the contract event 0xc67398012c111ce95ecb7429b933096c977380ee6c421175a71a4a4c6c88c06e.
//
// Solidity: event BlacklisterChanged(address indexed newBlacklister)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseBlacklisterChanged(log types.Log) (*FiatTokenV22BlacklisterChanged, error) {
	event := new(FiatTokenV22BlacklisterChanged)
	if err := _FiatTokenV22.contract.UnpackLog(event, "BlacklisterChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22BurnIterator is returned from FilterBurn and is used to iterate over the raw logs and unpacked data for Burn events raised by the FiatTokenV22 contract.
type FiatTokenV22BurnIterator struct {
	Event *FiatTokenV22Burn // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22BurnIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22Burn)
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
		it.Event = new(FiatTokenV22Burn)
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
func (it *FiatTokenV22BurnIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22BurnIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22Burn represents a Burn event raised by the FiatTokenV22 contract.
type FiatTokenV22Burn struct {
	Burner common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterBurn is a free log retrieval operation binding the contract event 0xcc16f5dbb4873280815c1ee09dbd06736cffcc184412cf7a71a0fdb75d397ca5.
//
// Solidity: event Burn(address indexed burner, uint256 amount)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterBurn(opts *bind.FilterOpts, burner []common.Address) (*FiatTokenV22BurnIterator, error) {

	var burnerRule []interface{}
	for _, burnerItem := range burner {
		burnerRule = append(burnerRule, burnerItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "Burn", burnerRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22BurnIterator{contract: _FiatTokenV22.contract, event: "Burn", logs: logs, sub: sub}, nil
}

// WatchBurn is a free log subscription operation binding the contract event 0xcc16f5dbb4873280815c1ee09dbd06736cffcc184412cf7a71a0fdb75d397ca5.
//
// Solidity: event Burn(address indexed burner, uint256 amount)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchBurn(opts *bind.WatchOpts, sink chan<- *FiatTokenV22Burn, burner []common.Address) (event.Subscription, error) {

	var burnerRule []interface{}
	for _, burnerItem := range burner {
		burnerRule = append(burnerRule, burnerItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "Burn", burnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22Burn)
				if err := _FiatTokenV22.contract.UnpackLog(event, "Burn", log); err != nil {
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

// ParseBurn is a log parse operation binding the contract event 0xcc16f5dbb4873280815c1ee09dbd06736cffcc184412cf7a71a0fdb75d397ca5.
//
// Solidity: event Burn(address indexed burner, uint256 amount)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseBurn(log types.Log) (*FiatTokenV22Burn, error) {
	event := new(FiatTokenV22Burn)
	if err := _FiatTokenV22.contract.UnpackLog(event, "Burn", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22MasterMinterChangedIterator is returned from FilterMasterMinterChanged and is used to iterate over the raw logs and unpacked data for MasterMinterChanged events raised by the FiatTokenV22 contract.
type FiatTokenV22MasterMinterChangedIterator struct {
	Event *FiatTokenV22MasterMinterChanged // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22MasterMinterChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22MasterMinterChanged)
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
		it.Event = new(FiatTokenV22MasterMinterChanged)
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
func (it *FiatTokenV22MasterMinterChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22MasterMinterChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22MasterMinterChanged represents a MasterMinterChanged event raised by the FiatTokenV22 contract.
type FiatTokenV22MasterMinterChanged struct {
	NewMasterMinter common.Address
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterMasterMinterChanged is a free log retrieval operation binding the contract event 0xdb66dfa9c6b8f5226fe9aac7e51897ae8ee94ac31dc70bb6c9900b2574b707e6.
//
// Solidity: event MasterMinterChanged(address indexed newMasterMinter)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterMasterMinterChanged(opts *bind.FilterOpts, newMasterMinter []common.Address) (*FiatTokenV22MasterMinterChangedIterator, error) {

	var newMasterMinterRule []interface{}
	for _, newMasterMinterItem := range newMasterMinter {
		newMasterMinterRule = append(newMasterMinterRule, newMasterMinterItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "MasterMinterChanged", newMasterMinterRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22MasterMinterChangedIterator{contract: _FiatTokenV22.contract, event: "MasterMinterChanged", logs: logs, sub: sub}, nil
}

// WatchMasterMinterChanged is a free log subscription operation binding the contract event 0xdb66dfa9c6b8f5226fe9aac7e51897ae8ee94ac31dc70bb6c9900b2574b707e6.
//
// Solidity: event MasterMinterChanged(address indexed newMasterMinter)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchMasterMinterChanged(opts *bind.WatchOpts, sink chan<- *FiatTokenV22MasterMinterChanged, newMasterMinter []common.Address) (event.Subscription, error) {

	var newMasterMinterRule []interface{}
	for _, newMasterMinterItem := range newMasterMinter {
		newMasterMinterRule = append(newMasterMinterRule, newMasterMinterItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "MasterMinterChanged", newMasterMinterRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22MasterMinterChanged)
				if err := _FiatTokenV22.contract.UnpackLog(event, "MasterMinterChanged", log); err != nil {
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

// ParseMasterMinterChanged is a log parse operation binding the contract event 0xdb66dfa9c6b8f5226fe9aac7e51897ae8ee94ac31dc70bb6c9900b2574b707e6.
//
// Solidity: event MasterMinterChanged(address indexed newMasterMinter)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseMasterMinterChanged(log types.Log) (*FiatTokenV22MasterMinterChanged, error) {
	event := new(FiatTokenV22MasterMinterChanged)
	if err := _FiatTokenV22.contract.UnpackLog(event, "MasterMinterChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22MintIterator is returned from FilterMint and is used to iterate over the raw logs and unpacked data for Mint events raised by the FiatTokenV22 contract.
type FiatTokenV22MintIterator struct {
	Event *FiatTokenV22Mint // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22MintIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22Mint)
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
		it.Event = new(FiatTokenV22Mint)
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
func (it *FiatTokenV22MintIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22MintIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22Mint represents a Mint event raised by the FiatTokenV22 contract.
type FiatTokenV22Mint struct {
	Minter common.Address
	To     common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterMint is a free log retrieval operation binding the contract event 0xab8530f87dc9b59234c4623bf917212bb2536d647574c8e7e5da92c2ede0c9f8.
//
// Solidity: event Mint(address indexed minter, address indexed to, uint256 amount)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterMint(opts *bind.FilterOpts, minter []common.Address, to []common.Address) (*FiatTokenV22MintIterator, error) {

	var minterRule []interface{}
	for _, minterItem := range minter {
		minterRule = append(minterRule, minterItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "Mint", minterRule, toRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22MintIterator{contract: _FiatTokenV22.contract, event: "Mint", logs: logs, sub: sub}, nil
}

// WatchMint is a free log subscription operation binding the contract event 0xab8530f87dc9b59234c4623bf917212bb2536d647574c8e7e5da92c2ede0c9f8.
//
// Solidity: event Mint(address indexed minter, address indexed to, uint256 amount)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchMint(opts *bind.WatchOpts, sink chan<- *FiatTokenV22Mint, minter []common.Address, to []common.Address) (event.Subscription, error) {

	var minterRule []interface{}
	for _, minterItem := range minter {
		minterRule = append(minterRule, minterItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "Mint", minterRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22Mint)
				if err := _FiatTokenV22.contract.UnpackLog(event, "Mint", log); err != nil {
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

// ParseMint is a log parse operation binding the contract event 0xab8530f87dc9b59234c4623bf917212bb2536d647574c8e7e5da92c2ede0c9f8.
//
// Solidity: event Mint(address indexed minter, address indexed to, uint256 amount)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseMint(log types.Log) (*FiatTokenV22Mint, error) {
	event := new(FiatTokenV22Mint)
	if err := _FiatTokenV22.contract.UnpackLog(event, "Mint", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22MinterConfiguredIterator is returned from FilterMinterConfigured and is used to iterate over the raw logs and unpacked data for MinterConfigured events raised by the FiatTokenV22 contract.
type FiatTokenV22MinterConfiguredIterator struct {
	Event *FiatTokenV22MinterConfigured // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22MinterConfiguredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22MinterConfigured)
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
		it.Event = new(FiatTokenV22MinterConfigured)
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
func (it *FiatTokenV22MinterConfiguredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22MinterConfiguredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22MinterConfigured represents a MinterConfigured event raised by the FiatTokenV22 contract.
type FiatTokenV22MinterConfigured struct {
	Minter              common.Address
	MinterAllowedAmount *big.Int
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterMinterConfigured is a free log retrieval operation binding the contract event 0x46980fca912ef9bcdbd36877427b6b90e860769f604e89c0e67720cece530d20.
//
// Solidity: event MinterConfigured(address indexed minter, uint256 minterAllowedAmount)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterMinterConfigured(opts *bind.FilterOpts, minter []common.Address) (*FiatTokenV22MinterConfiguredIterator, error) {

	var minterRule []interface{}
	for _, minterItem := range minter {
		minterRule = append(minterRule, minterItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "MinterConfigured", minterRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22MinterConfiguredIterator{contract: _FiatTokenV22.contract, event: "MinterConfigured", logs: logs, sub: sub}, nil
}

// WatchMinterConfigured is a free log subscription operation binding the contract event 0x46980fca912ef9bcdbd36877427b6b90e860769f604e89c0e67720cece530d20.
//
// Solidity: event MinterConfigured(address indexed minter, uint256 minterAllowedAmount)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchMinterConfigured(opts *bind.WatchOpts, sink chan<- *FiatTokenV22MinterConfigured, minter []common.Address) (event.Subscription, error) {

	var minterRule []interface{}
	for _, minterItem := range minter {
		minterRule = append(minterRule, minterItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "MinterConfigured", minterRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22MinterConfigured)
				if err := _FiatTokenV22.contract.UnpackLog(event, "MinterConfigured", log); err != nil {
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

// ParseMinterConfigured is a log parse operation binding the contract event 0x46980fca912ef9bcdbd36877427b6b90e860769f604e89c0e67720cece530d20.
//
// Solidity: event MinterConfigured(address indexed minter, uint256 minterAllowedAmount)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseMinterConfigured(log types.Log) (*FiatTokenV22MinterConfigured, error) {
	event := new(FiatTokenV22MinterConfigured)
	if err := _FiatTokenV22.contract.UnpackLog(event, "MinterConfigured", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22MinterRemovedIterator is returned from FilterMinterRemoved and is used to iterate over the raw logs and unpacked data for MinterRemoved events raised by the FiatTokenV22 contract.
type FiatTokenV22MinterRemovedIterator struct {
	Event *FiatTokenV22MinterRemoved // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22MinterRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22MinterRemoved)
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
		it.Event = new(FiatTokenV22MinterRemoved)
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
func (it *FiatTokenV22MinterRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22MinterRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22MinterRemoved represents a MinterRemoved event raised by the FiatTokenV22 contract.
type FiatTokenV22MinterRemoved struct {
	OldMinter common.Address
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterMinterRemoved is a free log retrieval operation binding the contract event 0xe94479a9f7e1952cc78f2d6baab678adc1b772d936c6583def489e524cb66692.
//
// Solidity: event MinterRemoved(address indexed oldMinter)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterMinterRemoved(opts *bind.FilterOpts, oldMinter []common.Address) (*FiatTokenV22MinterRemovedIterator, error) {

	var oldMinterRule []interface{}
	for _, oldMinterItem := range oldMinter {
		oldMinterRule = append(oldMinterRule, oldMinterItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "MinterRemoved", oldMinterRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22MinterRemovedIterator{contract: _FiatTokenV22.contract, event: "MinterRemoved", logs: logs, sub: sub}, nil
}

// WatchMinterRemoved is a free log subscription operation binding the contract event 0xe94479a9f7e1952cc78f2d6baab678adc1b772d936c6583def489e524cb66692.
//
// Solidity: event MinterRemoved(address indexed oldMinter)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchMinterRemoved(opts *bind.WatchOpts, sink chan<- *FiatTokenV22MinterRemoved, oldMinter []common.Address) (event.Subscription, error) {

	var oldMinterRule []interface{}
	for _, oldMinterItem := range oldMinter {
		oldMinterRule = append(oldMinterRule, oldMinterItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "MinterRemoved", oldMinterRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22MinterRemoved)
				if err := _FiatTokenV22.contract.UnpackLog(event, "MinterRemoved", log); err != nil {
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

// ParseMinterRemoved is a log parse operation binding the contract event 0xe94479a9f7e1952cc78f2d6baab678adc1b772d936c6583def489e524cb66692.
//
// Solidity: event MinterRemoved(address indexed oldMinter)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseMinterRemoved(log types.Log) (*FiatTokenV22MinterRemoved, error) {
	event := new(FiatTokenV22MinterRemoved)
	if err := _FiatTokenV22.contract.UnpackLog(event, "MinterRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22OwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the FiatTokenV22 contract.
type FiatTokenV22OwnershipTransferredIterator struct {
	Event *FiatTokenV22OwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22OwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22OwnershipTransferred)
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
		it.Event = new(FiatTokenV22OwnershipTransferred)
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
func (it *FiatTokenV22OwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22OwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22OwnershipTransferred represents a OwnershipTransferred event raised by the FiatTokenV22 contract.
type FiatTokenV22OwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address previousOwner, address newOwner)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterOwnershipTransferred(opts *bind.FilterOpts) (*FiatTokenV22OwnershipTransferredIterator, error) {

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "OwnershipTransferred")
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22OwnershipTransferredIterator{contract: _FiatTokenV22.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address previousOwner, address newOwner)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *FiatTokenV22OwnershipTransferred) (event.Subscription, error) {

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "OwnershipTransferred")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22OwnershipTransferred)
				if err := _FiatTokenV22.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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

// ParseOwnershipTransferred is a log parse operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address previousOwner, address newOwner)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseOwnershipTransferred(log types.Log) (*FiatTokenV22OwnershipTransferred, error) {
	event := new(FiatTokenV22OwnershipTransferred)
	if err := _FiatTokenV22.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22PauseIterator is returned from FilterPause and is used to iterate over the raw logs and unpacked data for Pause events raised by the FiatTokenV22 contract.
type FiatTokenV22PauseIterator struct {
	Event *FiatTokenV22Pause // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22PauseIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22Pause)
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
		it.Event = new(FiatTokenV22Pause)
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
func (it *FiatTokenV22PauseIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22PauseIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22Pause represents a Pause event raised by the FiatTokenV22 contract.
type FiatTokenV22Pause struct {
	Raw types.Log // Blockchain specific contextual infos
}

// FilterPause is a free log retrieval operation binding the contract event 0x6985a02210a168e66602d3235cb6db0e70f92b3ba4d376a33c0f3d9434bff625.
//
// Solidity: event Pause()
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterPause(opts *bind.FilterOpts) (*FiatTokenV22PauseIterator, error) {

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "Pause")
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22PauseIterator{contract: _FiatTokenV22.contract, event: "Pause", logs: logs, sub: sub}, nil
}

// WatchPause is a free log subscription operation binding the contract event 0x6985a02210a168e66602d3235cb6db0e70f92b3ba4d376a33c0f3d9434bff625.
//
// Solidity: event Pause()
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchPause(opts *bind.WatchOpts, sink chan<- *FiatTokenV22Pause) (event.Subscription, error) {

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "Pause")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22Pause)
				if err := _FiatTokenV22.contract.UnpackLog(event, "Pause", log); err != nil {
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

// ParsePause is a log parse operation binding the contract event 0x6985a02210a168e66602d3235cb6db0e70f92b3ba4d376a33c0f3d9434bff625.
//
// Solidity: event Pause()
func (_FiatTokenV22 *FiatTokenV22Filterer) ParsePause(log types.Log) (*FiatTokenV22Pause, error) {
	event := new(FiatTokenV22Pause)
	if err := _FiatTokenV22.contract.UnpackLog(event, "Pause", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22PauserChangedIterator is returned from FilterPauserChanged and is used to iterate over the raw logs and unpacked data for PauserChanged events raised by the FiatTokenV22 contract.
type FiatTokenV22PauserChangedIterator struct {
	Event *FiatTokenV22PauserChanged // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22PauserChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22PauserChanged)
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
		it.Event = new(FiatTokenV22PauserChanged)
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
func (it *FiatTokenV22PauserChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22PauserChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22PauserChanged represents a PauserChanged event raised by the FiatTokenV22 contract.
type FiatTokenV22PauserChanged struct {
	NewAddress common.Address
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterPauserChanged is a free log retrieval operation binding the contract event 0xb80482a293ca2e013eda8683c9bd7fc8347cfdaeea5ede58cba46df502c2a604.
//
// Solidity: event PauserChanged(address indexed newAddress)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterPauserChanged(opts *bind.FilterOpts, newAddress []common.Address) (*FiatTokenV22PauserChangedIterator, error) {

	var newAddressRule []interface{}
	for _, newAddressItem := range newAddress {
		newAddressRule = append(newAddressRule, newAddressItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "PauserChanged", newAddressRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22PauserChangedIterator{contract: _FiatTokenV22.contract, event: "PauserChanged", logs: logs, sub: sub}, nil
}

// WatchPauserChanged is a free log subscription operation binding the contract event 0xb80482a293ca2e013eda8683c9bd7fc8347cfdaeea5ede58cba46df502c2a604.
//
// Solidity: event PauserChanged(address indexed newAddress)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchPauserChanged(opts *bind.WatchOpts, sink chan<- *FiatTokenV22PauserChanged, newAddress []common.Address) (event.Subscription, error) {

	var newAddressRule []interface{}
	for _, newAddressItem := range newAddress {
		newAddressRule = append(newAddressRule, newAddressItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "PauserChanged", newAddressRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22PauserChanged)
				if err := _FiatTokenV22.contract.UnpackLog(event, "PauserChanged", log); err != nil {
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

// ParsePauserChanged is a log parse operation binding the contract event 0xb80482a293ca2e013eda8683c9bd7fc8347cfdaeea5ede58cba46df502c2a604.
//
// Solidity: event PauserChanged(address indexed newAddress)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParsePauserChanged(log types.Log) (*FiatTokenV22PauserChanged, error) {
	event := new(FiatTokenV22PauserChanged)
	if err := _FiatTokenV22.contract.UnpackLog(event, "PauserChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22RescuerChangedIterator is returned from FilterRescuerChanged and is used to iterate over the raw logs and unpacked data for RescuerChanged events raised by the FiatTokenV22 contract.
type FiatTokenV22RescuerChangedIterator struct {
	Event *FiatTokenV22RescuerChanged // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22RescuerChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22RescuerChanged)
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
		it.Event = new(FiatTokenV22RescuerChanged)
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
func (it *FiatTokenV22RescuerChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22RescuerChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22RescuerChanged represents a RescuerChanged event raised by the FiatTokenV22 contract.
type FiatTokenV22RescuerChanged struct {
	NewRescuer common.Address
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterRescuerChanged is a free log retrieval operation binding the contract event 0xe475e580d85111348e40d8ca33cfdd74c30fe1655c2d8537a13abc10065ffa5a.
//
// Solidity: event RescuerChanged(address indexed newRescuer)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterRescuerChanged(opts *bind.FilterOpts, newRescuer []common.Address) (*FiatTokenV22RescuerChangedIterator, error) {

	var newRescuerRule []interface{}
	for _, newRescuerItem := range newRescuer {
		newRescuerRule = append(newRescuerRule, newRescuerItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "RescuerChanged", newRescuerRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22RescuerChangedIterator{contract: _FiatTokenV22.contract, event: "RescuerChanged", logs: logs, sub: sub}, nil
}

// WatchRescuerChanged is a free log subscription operation binding the contract event 0xe475e580d85111348e40d8ca33cfdd74c30fe1655c2d8537a13abc10065ffa5a.
//
// Solidity: event RescuerChanged(address indexed newRescuer)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchRescuerChanged(opts *bind.WatchOpts, sink chan<- *FiatTokenV22RescuerChanged, newRescuer []common.Address) (event.Subscription, error) {

	var newRescuerRule []interface{}
	for _, newRescuerItem := range newRescuer {
		newRescuerRule = append(newRescuerRule, newRescuerItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "RescuerChanged", newRescuerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22RescuerChanged)
				if err := _FiatTokenV22.contract.UnpackLog(event, "RescuerChanged", log); err != nil {
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

// ParseRescuerChanged is a log parse operation binding the contract event 0xe475e580d85111348e40d8ca33cfdd74c30fe1655c2d8537a13abc10065ffa5a.
//
// Solidity: event RescuerChanged(address indexed newRescuer)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseRescuerChanged(log types.Log) (*FiatTokenV22RescuerChanged, error) {
	event := new(FiatTokenV22RescuerChanged)
	if err := _FiatTokenV22.contract.UnpackLog(event, "RescuerChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22TransferIterator is returned from FilterTransfer and is used to iterate over the raw logs and unpacked data for Transfer events raised by the FiatTokenV22 contract.
type FiatTokenV22TransferIterator struct {
	Event *FiatTokenV22Transfer // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22TransferIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22Transfer)
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
		it.Event = new(FiatTokenV22Transfer)
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
func (it *FiatTokenV22TransferIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22TransferIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22Transfer represents a Transfer event raised by the FiatTokenV22 contract.
type FiatTokenV22Transfer struct {
	From  common.Address
	To    common.Address
	Value *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterTransfer is a free log retrieval operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterTransfer(opts *bind.FilterOpts, from []common.Address, to []common.Address) (*FiatTokenV22TransferIterator, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22TransferIterator{contract: _FiatTokenV22.contract, event: "Transfer", logs: logs, sub: sub}, nil
}

// WatchTransfer is a free log subscription operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchTransfer(opts *bind.WatchOpts, sink chan<- *FiatTokenV22Transfer, from []common.Address, to []common.Address) (event.Subscription, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22Transfer)
				if err := _FiatTokenV22.contract.UnpackLog(event, "Transfer", log); err != nil {
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
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseTransfer(log types.Log) (*FiatTokenV22Transfer, error) {
	event := new(FiatTokenV22Transfer)
	if err := _FiatTokenV22.contract.UnpackLog(event, "Transfer", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22UnBlacklistedIterator is returned from FilterUnBlacklisted and is used to iterate over the raw logs and unpacked data for UnBlacklisted events raised by the FiatTokenV22 contract.
type FiatTokenV22UnBlacklistedIterator struct {
	Event *FiatTokenV22UnBlacklisted // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22UnBlacklistedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22UnBlacklisted)
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
		it.Event = new(FiatTokenV22UnBlacklisted)
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
func (it *FiatTokenV22UnBlacklistedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22UnBlacklistedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22UnBlacklisted represents a UnBlacklisted event raised by the FiatTokenV22 contract.
type FiatTokenV22UnBlacklisted struct {
	Account common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterUnBlacklisted is a free log retrieval operation binding the contract event 0x117e3210bb9aa7d9baff172026820255c6f6c30ba8999d1c2fd88e2848137c4e.
//
// Solidity: event UnBlacklisted(address indexed _account)
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterUnBlacklisted(opts *bind.FilterOpts, _account []common.Address) (*FiatTokenV22UnBlacklistedIterator, error) {

	var _accountRule []interface{}
	for _, _accountItem := range _account {
		_accountRule = append(_accountRule, _accountItem)
	}

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "UnBlacklisted", _accountRule)
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22UnBlacklistedIterator{contract: _FiatTokenV22.contract, event: "UnBlacklisted", logs: logs, sub: sub}, nil
}

// WatchUnBlacklisted is a free log subscription operation binding the contract event 0x117e3210bb9aa7d9baff172026820255c6f6c30ba8999d1c2fd88e2848137c4e.
//
// Solidity: event UnBlacklisted(address indexed _account)
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchUnBlacklisted(opts *bind.WatchOpts, sink chan<- *FiatTokenV22UnBlacklisted, _account []common.Address) (event.Subscription, error) {

	var _accountRule []interface{}
	for _, _accountItem := range _account {
		_accountRule = append(_accountRule, _accountItem)
	}

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "UnBlacklisted", _accountRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22UnBlacklisted)
				if err := _FiatTokenV22.contract.UnpackLog(event, "UnBlacklisted", log); err != nil {
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

// ParseUnBlacklisted is a log parse operation binding the contract event 0x117e3210bb9aa7d9baff172026820255c6f6c30ba8999d1c2fd88e2848137c4e.
//
// Solidity: event UnBlacklisted(address indexed _account)
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseUnBlacklisted(log types.Log) (*FiatTokenV22UnBlacklisted, error) {
	event := new(FiatTokenV22UnBlacklisted)
	if err := _FiatTokenV22.contract.UnpackLog(event, "UnBlacklisted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FiatTokenV22UnpauseIterator is returned from FilterUnpause and is used to iterate over the raw logs and unpacked data for Unpause events raised by the FiatTokenV22 contract.
type FiatTokenV22UnpauseIterator struct {
	Event *FiatTokenV22Unpause // Event containing the contract specifics and raw log

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
func (it *FiatTokenV22UnpauseIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FiatTokenV22Unpause)
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
		it.Event = new(FiatTokenV22Unpause)
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
func (it *FiatTokenV22UnpauseIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FiatTokenV22UnpauseIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FiatTokenV22Unpause represents a Unpause event raised by the FiatTokenV22 contract.
type FiatTokenV22Unpause struct {
	Raw types.Log // Blockchain specific contextual infos
}

// FilterUnpause is a free log retrieval operation binding the contract event 0x7805862f689e2f13df9f062ff482ad3ad112aca9e0847911ed832e158c525b33.
//
// Solidity: event Unpause()
func (_FiatTokenV22 *FiatTokenV22Filterer) FilterUnpause(opts *bind.FilterOpts) (*FiatTokenV22UnpauseIterator, error) {

	logs, sub, err := _FiatTokenV22.contract.FilterLogs(opts, "Unpause")
	if err != nil {
		return nil, err
	}
	return &FiatTokenV22UnpauseIterator{contract: _FiatTokenV22.contract, event: "Unpause", logs: logs, sub: sub}, nil
}

// WatchUnpause is a free log subscription operation binding the contract event 0x7805862f689e2f13df9f062ff482ad3ad112aca9e0847911ed832e158c525b33.
//
// Solidity: event Unpause()
func (_FiatTokenV22 *FiatTokenV22Filterer) WatchUnpause(opts *bind.WatchOpts, sink chan<- *FiatTokenV22Unpause) (event.Subscription, error) {

	logs, sub, err := _FiatTokenV22.contract.WatchLogs(opts, "Unpause")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FiatTokenV22Unpause)
				if err := _FiatTokenV22.contract.UnpackLog(event, "Unpause", log); err != nil {
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

// ParseUnpause is a log parse operation binding the contract event 0x7805862f689e2f13df9f062ff482ad3ad112aca9e0847911ed832e158c525b33.
//
// Solidity: event Unpause()
func (_FiatTokenV22 *FiatTokenV22Filterer) ParseUnpause(log types.Log) (*FiatTokenV22Unpause, error) {
	event := new(FiatTokenV22Unpause)
	if err := _FiatTokenV22.contract.UnpackLog(event, "Unpause", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
