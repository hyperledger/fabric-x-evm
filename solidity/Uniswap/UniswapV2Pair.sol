// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// Minimal ERC20 interface used by the pair
interface IERC20Minimal {
    function transfer(address to, uint256 value) external returns (bool);
    function transferFrom(address from, address to, uint256 value) external returns (bool);
    function balanceOf(address owner) external view returns (uint256);
}

/// Minimal UniswapV2-style pair that supports initialize, mint, swap, getReserves
contract UniswapV2Pair {
    address public token0;
    address public token1;

    uint112 private reserve0;
    uint112 private reserve1;
    bool private initialized;

    // LP token bookkeeping (simple)
    uint256 public totalSupply;
    mapping(address => uint256) public balanceOf;

    event Mint(address indexed sender, uint256 amount0, uint256 amount1);
    event Swap(address indexed sender, uint256 amount0Out, uint256 amount1Out, address indexed to);
    event Sync(uint112 reserve0, uint112 reserve1);

    function initialize(address _token0, address _token1) external {
        require(!initialized, "ALREADY_INITIALIZED");
        require(_token0 != _token1, "IDENTICAL_ADDRESSES");
        token0 = _token0;
        token1 = _token1;
        initialized = true;
    }

    function getReserves() external view returns (uint112, uint112) {
        return (reserve0, reserve1);
    }

    /// Mint LP tokens to `to`. Caller must have transferred tokens into this contract beforehand.
    function mint(address to) external returns (uint256 liquidity) {
        uint256 balance0 = IERC20Minimal(token0).balanceOf(address(this));
        uint256 balance1 = IERC20Minimal(token1).balanceOf(address(this));

        uint256 amount0 = 0;
        uint256 amount1 = 0;
        if (balance0 > uint256(reserve0)) amount0 = balance0 - uint256(reserve0);
        if (balance1 > uint256(reserve1)) amount1 = balance1 - uint256(reserve1);

        require(amount0 > 0 || amount1 > 0, "NO_LIQUIDITY_ADDED");

        if (totalSupply == 0) {
            // first liquidity provider: mint sqrt(amount0 * amount1)
            liquidity = _sqrt(amount0 * amount1);
            require(liquidity > 0, "INSUFFICIENT_LIQUIDITY_MINTED");
        } else {
            uint256 liquidity0 = (amount0 * totalSupply) / uint256(reserve0);
            uint256 liquidity1 = (amount1 * totalSupply) / uint256(reserve1);
            liquidity = liquidity0 < liquidity1 ? liquidity0 : liquidity1;
            require(liquidity > 0, "INSUFFICIENT_LIQUIDITY_MINTED");
        }

        totalSupply += liquidity;
        balanceOf[to] += liquidity;

        _update(uint112(balance0), uint112(balance1));
        emit Mint(msg.sender, amount0, amount1);
    }

    /// Swap: amounts out are specified (one must be zero). This is a simplified implementation:
    /// - sends tokens out, then checks constant product invariant (no fees)
    function swap(uint256 amount0Out, uint256 amount1Out, address to, bytes calldata /*data*/) external {
        require(amount0Out > 0 || amount1Out > 0, "INSUFFICIENT_OUTPUT_AMOUNT");
        require(amount0Out < reserve0 && amount1Out < reserve1, "INSUFFICIENT_LIQUIDITY");

        if (amount0Out > 0) {
            require(IERC20Minimal(token0).transfer(to, amount0Out), "TRANSFER_FAILED_TOKEN0");
        }
        if (amount1Out > 0) {
            require(IERC20Minimal(token1).transfer(to, amount1Out), "TRANSFER_FAILED_TOKEN1");
        }

        uint256 balance0 = IERC20Minimal(token0).balanceOf(address(this));
        uint256 balance1 = IERC20Minimal(token1).balanceOf(address(this));

        // require constant product invariant (balance0 * balance1 >= reserve0 * reserve1)
        require(balance0 * balance1 >= uint256(reserve0) * uint256(reserve1), "K");

        _update(uint112(balance0), uint112(balance1));
        emit Swap(msg.sender, amount0Out, amount1Out, to);
    }

    // ---- internal helpers ----

    function _update(uint112 _balance0, uint112 _balance1) internal {
        reserve0 = _balance0;
        reserve1 = _balance1;
        emit Sync(reserve0, reserve1);
    }

    // Babylonian method for sqrt
    function _sqrt(uint256 y) internal pure returns (uint256 z) {
        if (y == 0) return 0;
        uint256 x = y;
        z = y;
        uint256 tmp = (x + 1) / 2;
        while (tmp < z) {
            z = tmp;
            tmp = (x / tmp + tmp) / 2;
        }
    }
}
