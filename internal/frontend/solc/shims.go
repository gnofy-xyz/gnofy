package solc

// BuiltinShims provee interfaces y librerías mínimas de los paquetes Solidity más comunes
// para poder analizar contratos independientes sin requerir node_modules instalados.
var BuiltinShims = map[string]string{
	"@openzeppelin/contracts/utils/ReentrancyGuard.sol": `// SPDX-License-Identifier: MIT
pragma solidity >=0.8.0;

abstract contract ReentrancyGuard {
    uint256 private _status;
    modifier nonReentrant() {
        _;
    }
}
`,
	"@openzeppelin/contracts/security/ReentrancyGuard.sol": `// SPDX-License-Identifier: MIT
pragma solidity >=0.8.0;

import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
`,
	"@openzeppelin/contracts/token/ERC20/IERC20.sol": `// SPDX-License-Identifier: MIT
pragma solidity >=0.8.0;

interface IERC20 {
    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);

    function totalSupply() external view returns (uint256);
    function balanceOf(address account) external view returns (uint256);
    function transfer(address to, uint256 value) external returns (bool);
    function allowance(address owner, address spender) external view returns (uint256);
    function approve(address spender, uint256 value) external returns (bool);
    function transferFrom(address from, address to, uint256 value) external returns (bool);
}
`,
	"@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol": `// SPDX-License-Identifier: MIT
pragma solidity >=0.8.0;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

library SafeERC20 {
    function safeTransfer(IERC20 token, address to, uint256 value) internal {
        require(token.transfer(to, value), "SafeERC20: transfer failed");
    }

    function safeTransferFrom(IERC20 token, address from, address to, uint256 value) internal {
        require(token.transferFrom(from, to, value), "SafeERC20: transferFrom failed");
    }

    function forceApprove(IERC20 token, address spender, uint256 value) internal {
        require(token.approve(spender, value), "SafeERC20: approve failed");
    }
}
`,
	"@openzeppelin/contracts/access/Ownable.sol": `// SPDX-License-Identifier: MIT
pragma solidity >=0.8.0;

abstract contract Ownable {
    address private _owner;
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);

    constructor(address initialOwner) {
        _owner = initialOwner;
    }

    modifier onlyOwner() {
        require(msg.sender == _owner, "Ownable: caller is not the owner");
        _;
    }

    function owner() public view virtual returns (address) {
        return _owner;
    }
}
`,
	"@openzeppelin/contracts/utils/Pausable.sol": `// SPDX-License-Identifier: MIT
pragma solidity >=0.8.0;

abstract contract Pausable {
    bool private _paused;
    modifier whenNotPaused() {
        require(!_paused, "Pausable: paused");
        _;
    }
    modifier whenPaused() {
        require(_paused, "Pausable: not paused");
        _;
    }
    function paused() public view virtual returns (bool) {
        return _paused;
    }
}
`,
	"@openzeppelin/contracts/security/Pausable.sol": `// SPDX-License-Identifier: MIT
pragma solidity >=0.8.0;

import "@openzeppelin/contracts/utils/Pausable.sol";
`,
	"@prb/math/contracts/PRBMath.sol": `// SPDX-License-Identifier: Unlicense
pragma solidity >=0.8.0;

library PRBMath {
    function mulDiv(uint256 x, uint256 y, uint256 denominator) internal pure returns (uint256 result) {
        require(denominator > 0, "PRBMath: division by zero");
        return (x * y) / denominator;
    }

    function mulDivFixedPoint(uint256 x, uint256 y) internal pure returns (uint256 result) {
        return mulDiv(x, y, 1e18);
    }
}
`,
}
