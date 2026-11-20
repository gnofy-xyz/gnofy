// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

abstract contract BaseA {
    uint64 public baseVal = 10;

    function compute() public view virtual returns (uint64) {
        return baseVal;
    }
}

abstract contract BaseB is BaseA {
    function compute() public view virtual override returns (uint64) {
        return super.compute() + 5;
    }
}

/// @title MultiInherit — Verifica linealización C3, override y super()
contract MultiInherit is BaseA, BaseB {
    function compute() public view override(BaseA, BaseB) returns (uint64) {
        return super.compute() + 1;
    }
}
