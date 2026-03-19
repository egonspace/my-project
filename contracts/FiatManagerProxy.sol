// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.9;

import "@openzeppelin/contracts/proxy/Proxy.sol";

contract FiatManagerProxy is Proxy {
    // keccak-256("FiatManagerProxy.implementation.slot") - 1
    bytes32 internal constant _IMPLEMENTATION_SLOT = 0x980fc3257973fa83878408e45df08d2443420e5e752a486e9fdbdf6c6dde49ad;

    constructor(address imp) {
        assembly {
            sstore(_IMPLEMENTATION_SLOT, imp)
        }
    }

    function _implementation() internal view virtual override returns (address impAddress) {
        assembly {
            impAddress := sload(_IMPLEMENTATION_SLOT)
        }
    }
}
