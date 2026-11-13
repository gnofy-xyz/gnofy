// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title Counter — Contrato mínimo de referencia para el Hito v0.1 / v0.2
contract Counter {
    uint64 public count;

    event Incremented(address indexed caller, uint64 newCount);

    function increment() external {
        count += 1;
        emit Incremented(msg.sender, count);
    }

    function decrement() external {
        require(count > 0, "Counter: underflow");
        count -= 1;
    }

    function get() external view returns (uint64) {
        return count;
    }
}
