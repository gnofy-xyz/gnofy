// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

abstract contract Owned {
    address public owner;

    constructor() {
        owner = msg.sender;
    }

    modifier onlyOwner() {
        require(msg.sender == owner, "Owned: not owner");
        _;
    }
}

abstract contract Tagged {
    string public tag;

    constructor(string memory _tag) {
        tag = _tag;
    }
}

/// @title OwnedVault — Regresión Bug #1: preserva constructores base (0-arg y parametrizados)
contract OwnedVault is Owned, Tagged {
    uint256 public cap;

    constructor(uint256 c) Tagged("vault-v1") {
        cap = c;
    }

    function setCap(uint256 newCap) external onlyOwner {
        cap = newCap;
    }
}
