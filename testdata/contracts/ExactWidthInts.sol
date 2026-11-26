// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @title ExactWidthInts — Regresión Bugs #2 y #3: uint8/16/32, uint128 e int8/int64 checked
contract ExactWidthInts {
    uint8 public small = 250;
    uint16 public med = 65530;
    uint32 public big32 = 4294967290;
    uint128 public wide128;
    int8 public signed8 = 120;
    int64 public signed64 = -10;

    function addSmall(uint8 delta) external returns (uint8) {
        small += delta;
        return small;
    }

    function incSmall() external returns (uint8) {
        small++;
        return small;
    }

    function addMed(uint16 delta) external returns (uint16) {
        med += delta;
        return med;
    }

    function addBig32(uint32 delta) external returns (uint32) {
        big32 += delta;
        return big32;
    }

    function setAndAdd128(uint128 base, uint128 delta) external returns (uint128) {
        wide128 = base + delta;
        return wide128;
    }

    function addSigned8(int8 delta) external returns (int8) {
        signed8 = signed8 + delta;
        return signed8;
    }

    function subSigned64(int64 delta) external returns (int64) {
        signed64 = signed64 - delta;
        return signed64;
    }

    function divSigned64(int64 a, int64 b) external pure returns (int64) {
        return a / b;
    }
}
