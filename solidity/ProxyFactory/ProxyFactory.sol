pragma solidity ^0.8.0;

contract ProxyFactory {
    function createContract(bytes memory code) public returns (address addr) {
        assembly {
            addr := create(0, add(code, 0x20), mload(code))
            if iszero(extcodesize(addr)) { revert(0, 0) }
        }
    }
}
