// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title WideSignedInt — Regresión Bug #3: int256/int128 deben marcarse como Hole, nunca recortarse a int64
contract WideSignedInt {
    int256 public delta;

    function applyDelta(int256 x) external returns (int256) {
        delta = delta + x;
        return delta;
    }
}
