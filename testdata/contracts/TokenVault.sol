// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "@prb/math/contracts/PRBMath.sol";

event VaultOpened();
event VaultClosed();
event Deposited(address indexed user, uint256 rawAmount, uint256 netAmount);
event Withdrawn(address indexed to, uint256 amount);
event OrderCancelled(uint128 indexed orderId);

/// @title TokenVault — Public DeFi fixture covering structs in mappings, storage refs,
///        dynamic arrays (.push/.pop), SafeERC20/IERC20, ReentrancyGuard and PRBMath.mulDiv.
contract TokenVault is ReentrancyGuard {
    enum VaultStatus {
        INACTIVE,
        OPEN,
        CLOSED
    }

    struct Position {
        uint256 rawAmount;
        uint256 netAmount;
        uint128 feeBps;
        bool withdrawn;
    }

    struct Order {
        address maker;
        uint256 amount;
    }

    VaultStatus public status;
    address public owner;
    address public tokenContract;
    uint256 public totalNet;
    uint256 public totalFees;
    uint256 public targetAmount;

    address[] depositors;
    mapping(address => Position) positions;

    uint128[] orderIds;
    mapping(uint128 => Order) orders;

    constructor(address _token, uint256 _target) {
        require(_token != address(0), "TokenVault: zero token");
        require(_target > 0, "TokenVault: zero target");
        owner = msg.sender;
        tokenContract = _token;
        targetAmount = _target;
        status = VaultStatus.INACTIVE;
    }

    modifier onlyOwner() {
        require(msg.sender == owner, "TokenVault: not owner");
        _;
    }

    modifier whenOpen() {
        require(status == VaultStatus.OPEN, "TokenVault: not open");
        _;
    }

    function openVault() external onlyOwner {
        require(status == VaultStatus.INACTIVE, "TokenVault: already opened");
        status = VaultStatus.OPEN;
        emit VaultOpened();
    }

    function closeVault() external onlyOwner whenOpen {
        status = VaultStatus.CLOSED;
        emit VaultClosed();
    }

    function getPosition(address user) external view returns (Position memory) {
        return positions[user];
    }

    function tokenBalance() public view returns (uint256) {
        if (tokenContract == address(0)) {
            return address(this).balance;
        }
        IERC20 erc20 = IERC20(tokenContract);
        return erc20.balanceOf(address(this));
    }

    function deposit(uint128 feeBps) external nonReentrant whenOpen {
        IERC20 erc20 = IERC20(tokenContract);
        uint256 allowance = erc20.allowance(msg.sender, address(this));
        require(allowance > 0, "TokenVault: zero allowance");

        uint256 fee = PRBMath.mulDiv(allowance, feeBps, 10000);
        uint256 net = allowance - fee;
        require(totalNet + net <= targetAmount, "TokenVault: cap exceeded");

        SafeERC20.safeTransferFrom(erc20, msg.sender, address(this), allowance);

        bool existed = positions[msg.sender].rawAmount > 0;
        Position storage pos = positions[msg.sender];
        pos.rawAmount += allowance;
        pos.netAmount += net;
        pos.feeBps = feeBps;

        totalNet += net;
        totalFees += fee;

        if (!existed) {
            depositors.push(msg.sender);
        }

        if (totalNet == targetAmount) {
            status = VaultStatus.CLOSED;
            emit VaultClosed();
        }
        emit Deposited(msg.sender, allowance, net);
    }

    function createOrder(uint128 orderId, uint256 amount) external nonReentrant {
        require(positions[msg.sender].netAmount >= amount, "TokenVault: insufficient position");
        require(orders[orderId].amount == 0, "TokenVault: order exists");
        orderIds.push(orderId);
        orders[orderId] = Order(msg.sender, amount);
    }

    function cancelOrder(uint128 orderId) external nonReentrant {
        Order storage ord = orders[orderId];
        require(ord.maker == msg.sender, "TokenVault: not maker");
        require(ord.amount > 0, "TokenVault: empty order");
        ord.amount = 0;

        uint128[] storage ids = orderIds;
        for (uint256 i = 0; i < ids.length; i++) {
            if (ids[i] == orderId) {
                ids[i] = ids[ids.length - 1];
                ids.pop();
                break;
            }
        }
        emit OrderCancelled(orderId);
    }

    function withdrawAll(address recipient) external nonReentrant onlyOwner {
        require(status == VaultStatus.CLOSED, "TokenVault: still open");
        require(recipient != address(0), "TokenVault: zero recipient");
        uint256 bal = this.tokenBalance();
        require(bal >= totalNet, "TokenVault: insufficient balance");
        IERC20 erc20 = IERC20(tokenContract);
        SafeERC20.safeTransfer(erc20, recipient, bal);
        emit Withdrawn(recipient, bal);
    }
}
