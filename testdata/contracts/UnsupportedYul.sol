// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title UnsupportedYul — Contrato de referencia para verificar detección de nodos HoleIR
contract UnsupportedYul {
    function getChainIdAsm() external view returns (uint256 id) {
        assembly {
            id := chainid()
        }
    }

    function proxyForward(address target, bytes calldata data) external returns (bytes memory) {
        (bool ok, bytes memory ret) = target.delegatecall(data);
        require(ok, "delegatecall failed");
        return ret;
    }
}
