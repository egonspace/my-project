// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.9;

import "@openzeppelin/contracts/proxy/Proxy.sol";

contract FiatManagerProxy is Proxy {
    // EIP-1967 표준 슬롯: bytes32(uint256(keccak256("eip1967.proxy.implementation")) - 1)
    // UUPSUpgradeable.upgradeTo()가 이 슬롯에 새 주소를 기록하므로 반드시 일치해야 함
    bytes32 internal constant _IMPLEMENTATION_SLOT = 0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc;

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
