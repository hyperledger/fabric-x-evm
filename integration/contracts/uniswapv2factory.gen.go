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

// UniswapV2FactoryMetaData contains all meta data concerning the UniswapV2Factory contract.
var UniswapV2FactoryMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_feeToSetter\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token0\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token1\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"pair\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"PairCreated\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"allPairs\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"allPairsLength\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"tokenA\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"tokenB\",\"type\":\"address\"}],\"name\":\"createPair\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"pair\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"feeToSetter\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"getPair\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b506040516122d73803806122d7833981810160405281019061003291906100db565b806000806101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555050610108565b600080fd5b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b60006100a88261007d565b9050919050565b6100b88161009d565b81146100c357600080fd5b50565b6000815190506100d5816100af565b92915050565b6000602082840312156100f1576100f0610078565b5b60006100ff848285016100c6565b91505092915050565b6121c0806101176000396000f3fe608060405234801561001057600080fd5b50600436106100575760003560e01c8063094b74151461005c5780631e3dd18b1461007a578063574f2ba3146100aa578063c9c65396146100c8578063e6a43905146100f8575b600080fd5b610064610128565b6040516100719190610704565b60405180910390f35b610094600480360381019061008f919061075a565b61014c565b6040516100a19190610704565b60405180910390f35b6100b261018b565b6040516100bf9190610796565b60405180910390f35b6100e260048036038101906100dd91906107dd565b610198565b6040516100ef9190610704565b60405180910390f35b610112600480360381019061010d91906107dd565b610674565b60405161011f9190610704565b60405180910390f35b60008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b6002818154811061015c57600080fd5b906000526020600020016000915054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b6000600280549050905090565b60008173ffffffffffffffffffffffffffffffffffffffff168373ffffffffffffffffffffffffffffffffffffffff1603610208576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101ff9061087a565b60405180910390fd5b6000808373ffffffffffffffffffffffffffffffffffffffff168573ffffffffffffffffffffffffffffffffffffffff1610610245578385610248565b84845b91509150600073ffffffffffffffffffffffffffffffffffffffff16600160008473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1614610357576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161034e906108e6565b60405180910390fd5b600060405180602001610369906106b6565b6020820181038252601f19601f8201166040525090506000838360405160200161039492919061094e565b604051602081830303815290604052805190602001209050808251602084016000f594508473ffffffffffffffffffffffffffffffffffffffff1663485cc95585856040518363ffffffff1660e01b81526004016103f392919061097a565b600060405180830381600087803b15801561040d57600080fd5b505af1158015610421573d6000803e3d6000fd5b5050505084600160008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060006101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555084600160008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060006101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055506002859080600181540180825580915050600190039060005260206000200160009091909190916101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff1602179055508273ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff167f0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9876002805490506040516106629291906109a3565b60405180910390a35050505092915050565b60016020528160005260406000206020528060005260406000206000915091509054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b6117be806109cd83390190565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b60006106ee826106c3565b9050919050565b6106fe816106e3565b82525050565b600060208201905061071960008301846106f5565b92915050565b600080fd5b6000819050919050565b61073781610724565b811461074257600080fd5b50565b6000813590506107548161072e565b92915050565b6000602082840312156107705761076f61071f565b5b600061077e84828501610745565b91505092915050565b61079081610724565b82525050565b60006020820190506107ab6000830184610787565b92915050565b6107ba816106e3565b81146107c557600080fd5b50565b6000813590506107d7816107b1565b92915050565b600080604083850312156107f4576107f361071f565b5b6000610802858286016107c8565b9250506020610813858286016107c8565b9150509250929050565b600082825260208201905092915050565b7f4944454e544943414c5f41444452455353455300000000000000000000000000600082015250565b600061086460138361081d565b915061086f8261082e565b602082019050919050565b6000602082019050818103600083015261089381610857565b9050919050565b7f504149525f455849535453000000000000000000000000000000000000000000600082015250565b60006108d0600b8361081d565b91506108db8261089a565b602082019050919050565b600060208201905081810360008301526108ff816108c3565b9050919050565b60008160601b9050919050565b600061091e82610906565b9050919050565b600061093082610913565b9050919050565b610948610943826106e3565b610925565b82525050565b600061095a8285610937565b60148201915061096a8284610937565b6014820191508190509392505050565b600060408201905061098f60008301856106f5565b61099c60208301846106f5565b9392505050565b60006040820190506109b860008301856106f5565b6109c56020830184610787565b939250505056fe608060405234801561001057600080fd5b5061179e806100206000396000f3fe608060405234801561001057600080fd5b50600436106100885760003560e01c8063485cc9551161005b578063485cc955146101045780636a6278421461012057806370a0823114610150578063d21220a71461018057610088565b8063022c0d9f1461008d5780630902f1ac146100a95780630dfe1681146100c857806318160ddd146100e6575b600080fd5b6100a760048036038101906100a29190610fbe565b61019e565b005b6100b16106cb565b6040516100bf92919061106f565b60405180910390f35b6100d0610710565b6040516100dd91906110a7565b60405180910390f35b6100ee610734565b6040516100fb91906110d1565b60405180910390f35b61011e600480360381019061011991906110ec565b61073a565b005b61013a6004803603810190610135919061112c565b610898565b60405161014791906110d1565b60405180910390f35b61016a6004803603810190610165919061112c565b610d29565b60405161017791906110d1565b60405180910390f35b610188610d41565b60405161019591906110a7565b60405180910390f35b60008511806101ad5750600084115b6101ec576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016101e3906111b6565b60405180910390fd5b600260009054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff168510801561025257506002600e9054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff1684105b610291576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161028890611222565b60405180910390fd5b60008511156103785760008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663a9059cbb84876040518363ffffffff1660e01b81526004016102f5929190611242565b6020604051808303816000875af1158015610314573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061033891906112a3565b610377576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161036e9061131c565b60405180910390fd5b5b600084111561046157600160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1663a9059cbb84866040518363ffffffff1660e01b81526004016103de929190611242565b6020604051808303816000875af11580156103fd573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061042191906112a3565b610460576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161045790611388565b60405180910390fd5b5b60008060009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff166370a08231306040518263ffffffff1660e01b81526004016104bd91906110a7565b602060405180830381865afa1580156104da573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906104fe91906113bd565b90506000600160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff166370a08231306040518263ffffffff1660e01b815260040161055d91906110a7565b602060405180830381865afa15801561057a573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061059e91906113bd565b90506002600e9054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff16600260009054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff166106049190611419565b81836106109190611419565b1015610651576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610648906114a7565b60405180910390fd5b61065b8282610d67565b8473ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167f2a9237ff5aa599ef4c5ee4b1142b53429d5755e2685fe6288b2e3320202115f589896040516106ba9291906114c7565b60405180910390a350505050505050565b600080600260009054906101000a90046dffffffffffffffffffffffffffff166002600e9054906101000a90046dffffffffffffffffffffffffffff16915091509091565b60008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b60035481565b6002601c9054906101000a900460ff161561078a576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016107819061153c565b60405180910390fd5b8073ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff16036107f8576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016107ef906115a8565b60405180910390fd5b816000806101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555080600160006101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555060016002601c6101000a81548160ff0219169083151502179055505050565b60008060008054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff166370a08231306040518263ffffffff1660e01b81526004016108f491906110a7565b602060405180830381865afa158015610911573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061093591906113bd565b90506000600160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff166370a08231306040518263ffffffff1660e01b815260040161099491906110a7565b602060405180830381865afa1580156109b1573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906109d591906113bd565b9050600080600260009054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff16841115610a4957600260009054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff1684610a4691906115c8565b91505b6002600e9054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff16831115610ab8576002600e9054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff1683610ab591906115c8565b90505b6000821180610ac75750600081115b610b06576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610afd90611648565b60405180910390fd5b600060035403610b6e57610b248183610b1f9190611419565b610e46565b945060008511610b69576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610b60906116b4565b60405180910390fd5b610c57565b6000600260009054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff1660035484610bab9190611419565b610bb59190611703565b905060006002600e9054906101000a90046dffffffffffffffffffffffffffff166dffffffffffffffffffffffffffff1660035484610bf49190611419565b610bfe9190611703565b9050808210610c0d5780610c0f565b815b965060008711610c54576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610c4b906116b4565b60405180910390fd5b50505b8460036000828254610c699190611734565b9250508190555084600460008873ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000206000828254610cbf9190611734565b92505081905550610cd08484610d67565b3373ffffffffffffffffffffffffffffffffffffffff167f4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f8383604051610d189291906114c7565b60405180910390a250505050919050565b60046020528060005260406000206000915090505481565b600160009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1681565b81600260006101000a8154816dffffffffffffffffffffffffffff02191690836dffffffffffffffffffffffffffff160217905550806002600e6101000a8154816dffffffffffffffffffffffffffff02191690836dffffffffffffffffffffffffffff1602179055507f1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1600260009054906101000a90046dffffffffffffffffffffffffffff166002600e9054906101000a90046dffffffffffffffffffffffffffff16604051610e3a92919061106f565b60405180910390a15050565b6000808203610e585760009050610eb6565b600082905082915060006002600183610e719190611734565b610e7b9190611703565b90505b82811015610eb3578092506002818284610e989190611703565b610ea29190611734565b610eac9190611703565b9050610e7e565b50505b919050565b600080fd5b600080fd5b6000819050919050565b610ed881610ec5565b8114610ee357600080fd5b50565b600081359050610ef581610ecf565b92915050565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b6000610f2682610efb565b9050919050565b610f3681610f1b565b8114610f4157600080fd5b50565b600081359050610f5381610f2d565b92915050565b600080fd5b600080fd5b600080fd5b60008083601f840112610f7e57610f7d610f59565b5b8235905067ffffffffffffffff811115610f9b57610f9a610f5e565b5b602083019150836001820283011115610fb757610fb6610f63565b5b9250929050565b600080600080600060808688031215610fda57610fd9610ebb565b5b6000610fe888828901610ee6565b9550506020610ff988828901610ee6565b945050604061100a88828901610f44565b935050606086013567ffffffffffffffff81111561102b5761102a610ec0565b5b61103788828901610f68565b92509250509295509295909350565b60006dffffffffffffffffffffffffffff82169050919050565b61106981611046565b82525050565b60006040820190506110846000830185611060565b6110916020830184611060565b9392505050565b6110a181610f1b565b82525050565b60006020820190506110bc6000830184611098565b92915050565b6110cb81610ec5565b82525050565b60006020820190506110e660008301846110c2565b92915050565b6000806040838503121561110357611102610ebb565b5b600061111185828601610f44565b925050602061112285828601610f44565b9150509250929050565b60006020828403121561114257611141610ebb565b5b600061115084828501610f44565b91505092915050565b600082825260208201905092915050565b7f494e53554646494349454e545f4f55545055545f414d4f554e54000000000000600082015250565b60006111a0601a83611159565b91506111ab8261116a565b602082019050919050565b600060208201905081810360008301526111cf81611193565b9050919050565b7f494e53554646494349454e545f4c495155494449545900000000000000000000600082015250565b600061120c601683611159565b9150611217826111d6565b602082019050919050565b6000602082019050818103600083015261123b816111ff565b9050919050565b60006040820190506112576000830185611098565b61126460208301846110c2565b9392505050565b60008115159050919050565b6112808161126b565b811461128b57600080fd5b50565b60008151905061129d81611277565b92915050565b6000602082840312156112b9576112b8610ebb565b5b60006112c78482850161128e565b91505092915050565b7f5452414e534645525f4641494c45445f544f4b454e3000000000000000000000600082015250565b6000611306601683611159565b9150611311826112d0565b602082019050919050565b60006020820190508181036000830152611335816112f9565b9050919050565b7f5452414e534645525f4641494c45445f544f4b454e3100000000000000000000600082015250565b6000611372601683611159565b915061137d8261133c565b602082019050919050565b600060208201905081810360008301526113a181611365565b9050919050565b6000815190506113b781610ecf565b92915050565b6000602082840312156113d3576113d2610ebb565b5b60006113e1848285016113a8565b91505092915050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b600061142482610ec5565b915061142f83610ec5565b925082820261143d81610ec5565b91508282048414831517611454576114536113ea565b5b5092915050565b7f4b00000000000000000000000000000000000000000000000000000000000000600082015250565b6000611491600183611159565b915061149c8261145b565b602082019050919050565b600060208201905081810360008301526114c081611484565b9050919050565b60006040820190506114dc60008301856110c2565b6114e960208301846110c2565b9392505050565b7f414c52454144595f494e495449414c495a454400000000000000000000000000600082015250565b6000611526601383611159565b9150611531826114f0565b602082019050919050565b6000602082019050818103600083015261155581611519565b9050919050565b7f4944454e544943414c5f41444452455353455300000000000000000000000000600082015250565b6000611592601383611159565b915061159d8261155c565b602082019050919050565b600060208201905081810360008301526115c181611585565b9050919050565b60006115d382610ec5565b91506115de83610ec5565b92508282039050818111156115f6576115f56113ea565b5b92915050565b7f4e4f5f4c49515549444954595f41444445440000000000000000000000000000600082015250565b6000611632601283611159565b915061163d826115fc565b602082019050919050565b6000602082019050818103600083015261166181611625565b9050919050565b7f494e53554646494349454e545f4c49515549444954595f4d494e544544000000600082015250565b600061169e601d83611159565b91506116a982611668565b602082019050919050565b600060208201905081810360008301526116cd81611691565b9050919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601260045260246000fd5b600061170e82610ec5565b915061171983610ec5565b925082611729576117286116d4565b5b828204905092915050565b600061173f82610ec5565b915061174a83610ec5565b9250828201905080821115611762576117616113ea565b5b9291505056fea2646970667358221220b99a911e223eed73f0a0895ab485ffc5a0f9e87996f3afd72ca09650cbf10c9264736f6c63430008140033a2646970667358221220e3c0c14e8d38659e00b2a0f7f0ffd398e7d011e202249c5f7c669613824884fc64736f6c63430008140033",
}

// UniswapV2FactoryABI is the input ABI used to generate the binding from.
// Deprecated: Use UniswapV2FactoryMetaData.ABI instead.
var UniswapV2FactoryABI = UniswapV2FactoryMetaData.ABI

// UniswapV2FactoryBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use UniswapV2FactoryMetaData.Bin instead.
var UniswapV2FactoryBin = UniswapV2FactoryMetaData.Bin

// DeployUniswapV2Factory deploys a new Ethereum contract, binding an instance of UniswapV2Factory to it.
func DeployUniswapV2Factory(auth *bind.TransactOpts, backend bind.ContractBackend, _feeToSetter common.Address) (common.Address, *types.Transaction, *UniswapV2Factory, error) {
	parsed, err := UniswapV2FactoryMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(UniswapV2FactoryBin), backend, _feeToSetter)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &UniswapV2Factory{UniswapV2FactoryCaller: UniswapV2FactoryCaller{contract: contract}, UniswapV2FactoryTransactor: UniswapV2FactoryTransactor{contract: contract}, UniswapV2FactoryFilterer: UniswapV2FactoryFilterer{contract: contract}}, nil
}

// UniswapV2Factory is an auto generated Go binding around an Ethereum contract.
type UniswapV2Factory struct {
	UniswapV2FactoryCaller     // Read-only binding to the contract
	UniswapV2FactoryTransactor // Write-only binding to the contract
	UniswapV2FactoryFilterer   // Log filterer for contract events
}

// UniswapV2FactoryCaller is an auto generated read-only Go binding around an Ethereum contract.
type UniswapV2FactoryCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// UniswapV2FactoryTransactor is an auto generated write-only Go binding around an Ethereum contract.
type UniswapV2FactoryTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// UniswapV2FactoryFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type UniswapV2FactoryFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// UniswapV2FactorySession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type UniswapV2FactorySession struct {
	Contract     *UniswapV2Factory // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// UniswapV2FactoryCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type UniswapV2FactoryCallerSession struct {
	Contract *UniswapV2FactoryCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts           // Call options to use throughout this session
}

// UniswapV2FactoryTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type UniswapV2FactoryTransactorSession struct {
	Contract     *UniswapV2FactoryTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts           // Transaction auth options to use throughout this session
}

// UniswapV2FactoryRaw is an auto generated low-level Go binding around an Ethereum contract.
type UniswapV2FactoryRaw struct {
	Contract *UniswapV2Factory // Generic contract binding to access the raw methods on
}

// UniswapV2FactoryCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type UniswapV2FactoryCallerRaw struct {
	Contract *UniswapV2FactoryCaller // Generic read-only contract binding to access the raw methods on
}

// UniswapV2FactoryTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type UniswapV2FactoryTransactorRaw struct {
	Contract *UniswapV2FactoryTransactor // Generic write-only contract binding to access the raw methods on
}

// NewUniswapV2Factory creates a new instance of UniswapV2Factory, bound to a specific deployed contract.
func NewUniswapV2Factory(address common.Address, backend bind.ContractBackend) (*UniswapV2Factory, error) {
	contract, err := bindUniswapV2Factory(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &UniswapV2Factory{UniswapV2FactoryCaller: UniswapV2FactoryCaller{contract: contract}, UniswapV2FactoryTransactor: UniswapV2FactoryTransactor{contract: contract}, UniswapV2FactoryFilterer: UniswapV2FactoryFilterer{contract: contract}}, nil
}

// NewUniswapV2FactoryCaller creates a new read-only instance of UniswapV2Factory, bound to a specific deployed contract.
func NewUniswapV2FactoryCaller(address common.Address, caller bind.ContractCaller) (*UniswapV2FactoryCaller, error) {
	contract, err := bindUniswapV2Factory(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &UniswapV2FactoryCaller{contract: contract}, nil
}

// NewUniswapV2FactoryTransactor creates a new write-only instance of UniswapV2Factory, bound to a specific deployed contract.
func NewUniswapV2FactoryTransactor(address common.Address, transactor bind.ContractTransactor) (*UniswapV2FactoryTransactor, error) {
	contract, err := bindUniswapV2Factory(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &UniswapV2FactoryTransactor{contract: contract}, nil
}

// NewUniswapV2FactoryFilterer creates a new log filterer instance of UniswapV2Factory, bound to a specific deployed contract.
func NewUniswapV2FactoryFilterer(address common.Address, filterer bind.ContractFilterer) (*UniswapV2FactoryFilterer, error) {
	contract, err := bindUniswapV2Factory(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &UniswapV2FactoryFilterer{contract: contract}, nil
}

// bindUniswapV2Factory binds a generic wrapper to an already deployed contract.
func bindUniswapV2Factory(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := UniswapV2FactoryMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_UniswapV2Factory *UniswapV2FactoryRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _UniswapV2Factory.Contract.UniswapV2FactoryCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_UniswapV2Factory *UniswapV2FactoryRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _UniswapV2Factory.Contract.UniswapV2FactoryTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_UniswapV2Factory *UniswapV2FactoryRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _UniswapV2Factory.Contract.UniswapV2FactoryTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_UniswapV2Factory *UniswapV2FactoryCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _UniswapV2Factory.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_UniswapV2Factory *UniswapV2FactoryTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _UniswapV2Factory.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_UniswapV2Factory *UniswapV2FactoryTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _UniswapV2Factory.Contract.contract.Transact(opts, method, params...)
}

// AllPairs is a free data retrieval call binding the contract method 0x1e3dd18b.
//
// Solidity: function allPairs(uint256 ) view returns(address)
func (_UniswapV2Factory *UniswapV2FactoryCaller) AllPairs(opts *bind.CallOpts, arg0 *big.Int) (common.Address, error) {
	var out []interface{}
	err := _UniswapV2Factory.contract.Call(opts, &out, "allPairs", arg0)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// AllPairs is a free data retrieval call binding the contract method 0x1e3dd18b.
//
// Solidity: function allPairs(uint256 ) view returns(address)
func (_UniswapV2Factory *UniswapV2FactorySession) AllPairs(arg0 *big.Int) (common.Address, error) {
	return _UniswapV2Factory.Contract.AllPairs(&_UniswapV2Factory.CallOpts, arg0)
}

// AllPairs is a free data retrieval call binding the contract method 0x1e3dd18b.
//
// Solidity: function allPairs(uint256 ) view returns(address)
func (_UniswapV2Factory *UniswapV2FactoryCallerSession) AllPairs(arg0 *big.Int) (common.Address, error) {
	return _UniswapV2Factory.Contract.AllPairs(&_UniswapV2Factory.CallOpts, arg0)
}

// AllPairsLength is a free data retrieval call binding the contract method 0x574f2ba3.
//
// Solidity: function allPairsLength() view returns(uint256)
func (_UniswapV2Factory *UniswapV2FactoryCaller) AllPairsLength(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _UniswapV2Factory.contract.Call(opts, &out, "allPairsLength")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// AllPairsLength is a free data retrieval call binding the contract method 0x574f2ba3.
//
// Solidity: function allPairsLength() view returns(uint256)
func (_UniswapV2Factory *UniswapV2FactorySession) AllPairsLength() (*big.Int, error) {
	return _UniswapV2Factory.Contract.AllPairsLength(&_UniswapV2Factory.CallOpts)
}

// AllPairsLength is a free data retrieval call binding the contract method 0x574f2ba3.
//
// Solidity: function allPairsLength() view returns(uint256)
func (_UniswapV2Factory *UniswapV2FactoryCallerSession) AllPairsLength() (*big.Int, error) {
	return _UniswapV2Factory.Contract.AllPairsLength(&_UniswapV2Factory.CallOpts)
}

// FeeToSetter is a free data retrieval call binding the contract method 0x094b7415.
//
// Solidity: function feeToSetter() view returns(address)
func (_UniswapV2Factory *UniswapV2FactoryCaller) FeeToSetter(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _UniswapV2Factory.contract.Call(opts, &out, "feeToSetter")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// FeeToSetter is a free data retrieval call binding the contract method 0x094b7415.
//
// Solidity: function feeToSetter() view returns(address)
func (_UniswapV2Factory *UniswapV2FactorySession) FeeToSetter() (common.Address, error) {
	return _UniswapV2Factory.Contract.FeeToSetter(&_UniswapV2Factory.CallOpts)
}

// FeeToSetter is a free data retrieval call binding the contract method 0x094b7415.
//
// Solidity: function feeToSetter() view returns(address)
func (_UniswapV2Factory *UniswapV2FactoryCallerSession) FeeToSetter() (common.Address, error) {
	return _UniswapV2Factory.Contract.FeeToSetter(&_UniswapV2Factory.CallOpts)
}

// GetPair is a free data retrieval call binding the contract method 0xe6a43905.
//
// Solidity: function getPair(address , address ) view returns(address)
func (_UniswapV2Factory *UniswapV2FactoryCaller) GetPair(opts *bind.CallOpts, arg0 common.Address, arg1 common.Address) (common.Address, error) {
	var out []interface{}
	err := _UniswapV2Factory.contract.Call(opts, &out, "getPair", arg0, arg1)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// GetPair is a free data retrieval call binding the contract method 0xe6a43905.
//
// Solidity: function getPair(address , address ) view returns(address)
func (_UniswapV2Factory *UniswapV2FactorySession) GetPair(arg0 common.Address, arg1 common.Address) (common.Address, error) {
	return _UniswapV2Factory.Contract.GetPair(&_UniswapV2Factory.CallOpts, arg0, arg1)
}

// GetPair is a free data retrieval call binding the contract method 0xe6a43905.
//
// Solidity: function getPair(address , address ) view returns(address)
func (_UniswapV2Factory *UniswapV2FactoryCallerSession) GetPair(arg0 common.Address, arg1 common.Address) (common.Address, error) {
	return _UniswapV2Factory.Contract.GetPair(&_UniswapV2Factory.CallOpts, arg0, arg1)
}

// CreatePair is a paid mutator transaction binding the contract method 0xc9c65396.
//
// Solidity: function createPair(address tokenA, address tokenB) returns(address pair)
func (_UniswapV2Factory *UniswapV2FactoryTransactor) CreatePair(opts *bind.TransactOpts, tokenA common.Address, tokenB common.Address) (*types.Transaction, error) {
	return _UniswapV2Factory.contract.Transact(opts, "createPair", tokenA, tokenB)
}

// CreatePair is a paid mutator transaction binding the contract method 0xc9c65396.
//
// Solidity: function createPair(address tokenA, address tokenB) returns(address pair)
func (_UniswapV2Factory *UniswapV2FactorySession) CreatePair(tokenA common.Address, tokenB common.Address) (*types.Transaction, error) {
	return _UniswapV2Factory.Contract.CreatePair(&_UniswapV2Factory.TransactOpts, tokenA, tokenB)
}

// CreatePair is a paid mutator transaction binding the contract method 0xc9c65396.
//
// Solidity: function createPair(address tokenA, address tokenB) returns(address pair)
func (_UniswapV2Factory *UniswapV2FactoryTransactorSession) CreatePair(tokenA common.Address, tokenB common.Address) (*types.Transaction, error) {
	return _UniswapV2Factory.Contract.CreatePair(&_UniswapV2Factory.TransactOpts, tokenA, tokenB)
}

// UniswapV2FactoryPairCreatedIterator is returned from FilterPairCreated and is used to iterate over the raw logs and unpacked data for PairCreated events raised by the UniswapV2Factory contract.
type UniswapV2FactoryPairCreatedIterator struct {
	Event *UniswapV2FactoryPairCreated // Event containing the contract specifics and raw log

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
func (it *UniswapV2FactoryPairCreatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(UniswapV2FactoryPairCreated)
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
		it.Event = new(UniswapV2FactoryPairCreated)
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
func (it *UniswapV2FactoryPairCreatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *UniswapV2FactoryPairCreatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// UniswapV2FactoryPairCreated represents a PairCreated event raised by the UniswapV2Factory contract.
type UniswapV2FactoryPairCreated struct {
	Token0 common.Address
	Token1 common.Address
	Pair   common.Address
	Arg3   *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterPairCreated is a free log retrieval operation binding the contract event 0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9.
//
// Solidity: event PairCreated(address indexed token0, address indexed token1, address pair, uint256 arg3)
func (_UniswapV2Factory *UniswapV2FactoryFilterer) FilterPairCreated(opts *bind.FilterOpts, token0 []common.Address, token1 []common.Address) (*UniswapV2FactoryPairCreatedIterator, error) {

	var token0Rule []interface{}
	for _, token0Item := range token0 {
		token0Rule = append(token0Rule, token0Item)
	}
	var token1Rule []interface{}
	for _, token1Item := range token1 {
		token1Rule = append(token1Rule, token1Item)
	}

	logs, sub, err := _UniswapV2Factory.contract.FilterLogs(opts, "PairCreated", token0Rule, token1Rule)
	if err != nil {
		return nil, err
	}
	return &UniswapV2FactoryPairCreatedIterator{contract: _UniswapV2Factory.contract, event: "PairCreated", logs: logs, sub: sub}, nil
}

// WatchPairCreated is a free log subscription operation binding the contract event 0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9.
//
// Solidity: event PairCreated(address indexed token0, address indexed token1, address pair, uint256 arg3)
func (_UniswapV2Factory *UniswapV2FactoryFilterer) WatchPairCreated(opts *bind.WatchOpts, sink chan<- *UniswapV2FactoryPairCreated, token0 []common.Address, token1 []common.Address) (event.Subscription, error) {

	var token0Rule []interface{}
	for _, token0Item := range token0 {
		token0Rule = append(token0Rule, token0Item)
	}
	var token1Rule []interface{}
	for _, token1Item := range token1 {
		token1Rule = append(token1Rule, token1Item)
	}

	logs, sub, err := _UniswapV2Factory.contract.WatchLogs(opts, "PairCreated", token0Rule, token1Rule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(UniswapV2FactoryPairCreated)
				if err := _UniswapV2Factory.contract.UnpackLog(event, "PairCreated", log); err != nil {
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

// ParsePairCreated is a log parse operation binding the contract event 0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9.
//
// Solidity: event PairCreated(address indexed token0, address indexed token1, address pair, uint256 arg3)
func (_UniswapV2Factory *UniswapV2FactoryFilterer) ParsePairCreated(log types.Log) (*UniswapV2FactoryPairCreated, error) {
	event := new(UniswapV2FactoryPairCreated)
	if err := _UniswapV2Factory.contract.UnpackLog(event, "PairCreated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
