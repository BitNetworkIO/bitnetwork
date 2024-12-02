// SPDX-License-Identifier: MIT
pragma solidity ^0.8.9;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";

contract BitTokenERC20 is ERC20 {

    mapping(address => uint256) public mintRecord;
    uint256 public mintGap;
    address public owner = msg.sender;

    constructor(string memory name, string memory symbol) ERC20(name, symbol) {
        // Mint 100 tokens to msg.sender
        // Similar to how
        // 1 dollar = 100 cents
        // 1 token = 1 * (10 ** decimals)
        _mint(msg.sender, 10000 * 10**uint(decimals()));
    }

    event MintAfterBlockHeight(uint256 height);

    modifier onlyOwner() {
        require(msg.sender == owner, "Function can only be called by the owner.");
        _;
    }

    function setMintableGap(uint256 _mintGap) public onlyOwner {
        mintGap = _mintGap;
    }

    // public mint for any user
    function mint(uint256 amount) external {
        require(msg.sender != address(0), "ERC20: mint to the zero address");
        require(amount <= 1000000000000000000000, "ERC20: mint amount too large, maximum is 1000000000000000000000");
        if((mintRecord[msg.sender] == 0 || block.number - mintRecord[msg.sender] > mintGap) || ERC20(this).balanceOf(msg.sender) < 1000000000000000000000) {
            mintRecord[msg.sender] = block.number;
            _mint(msg.sender, amount);
        }
        emit MintAfterBlockHeight(mintRecord[msg.sender] + mintGap);
    }
}
