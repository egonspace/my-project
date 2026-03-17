// SPDX-License-Identifier: Apache-2.0

pragma solidity ^0.8.0;

import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { SafeMath } from "@openzeppelin/contracts/utils/math/SafeMath.sol";
import { SafeERC20 } from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import { IERC1271 } from "@openzeppelin/contracts/interfaces/IERC1271.sol";
import { EIP712 } from "@openzeppelin/contracts/utils/cryptography/EIP712.sol";

contract FiatToken {
    using SafeMath for uint256;
    using SafeERC20 for IERC20;

    address private _owner;

    address public pauser;
    bool public paused = false;

    address public blacklister;

    address private _rescuer;

    mapping(address => mapping(bytes32 => bool)) private _authorizationStates;
    mapping(address => uint256) private _permitNonces;

    string public name;
    string public symbol;
    uint8 public decimals;
    string public currency;
    address public masterMinter;
    mapping(address => uint256) internal balanceAndBlacklistStates;
    mapping(address => mapping(address => uint256)) internal allowed;
    uint256 internal totalSupply_ = 0;
    mapping(address => bool) internal minters;
    mapping(address => uint256) internal minterAllowed;

    bytes32 public constant TRANSFER_WITH_AUTHORIZATION_TYPEHASH = 0x7c7c6cdb67a18743f49ec6fa9b35f50d52ed05cbed4cc592e13b44501c1a2267;
    bytes32 public constant RECEIVE_WITH_AUTHORIZATION_TYPEHASH = 0xd099cc98ef71107a616c4f0f941f04c322d8e254fe26b3c6668db87aae413de8;
    bytes32 public constant CANCEL_AUTHORIZATION_TYPEHASH = 0x158b0a9edf7a828aad02f63cd515c68ef2f50ba807396f6d12842833a1597429;
    bytes32 public constant PERMIT_TYPEHASH = 0x6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9;

    event OwnershipTransferred(address previousOwner, address newOwner);
    event Pause();
    event Unpause();
    event PauserChanged(address indexed newAddress);
    event Blacklisted(address indexed _account);
    event UnBlacklisted(address indexed _account);
    event BlacklisterChanged(address indexed newBlacklister);
    event RescuerChanged(address indexed newRescuer);
    event Mint(address indexed minter, address indexed to, uint256 amount);
    event Burn(address indexed burner, uint256 amount);
    event MinterConfigured(address indexed minter, uint256 minterAllowedAmount);
    event MinterRemoved(address indexed oldMinter);
    event MasterMinterChanged(address indexed newMasterMinter);
    event AuthorizationUsed(address indexed authorizer, bytes32 indexed nonce);
    event AuthorizationCanceled(address indexed authorizer, bytes32 indexed nonce);
    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);

    modifier onlyOwner() {
        require(msg.sender == _owner, "Ownable: caller is not the owner");
        _;
    }

    modifier whenNotPaused() {
        require(!paused, "Pausable: paused");
        _;
    }

    modifier onlyPauser() {
        require(msg.sender == pauser, "Pausable: caller is not the pauser");
        _;
    }

    modifier onlyBlacklister() {
        require(msg.sender == blacklister, "Blacklistable: caller is not the blacklister");
        _;
    }

    modifier notBlacklisted(address _account) {
        require(!_isBlacklisted(_account), "Blacklistable: account is blacklisted");
        _;
    }

    modifier onlyRescuer() {
        require(msg.sender == _rescuer, "Rescuable: caller is not the rescuer");
        _;
    }

    modifier onlyMinters() {
        require(minters[msg.sender], "FiatToken: caller is not a minter");
        _;
    }

    modifier onlyMasterMinter() {
        require(msg.sender == masterMinter, "FiatToken: caller is not the masterMinter");
        _;
    }

    constructor(
        string memory tokenName,
        string memory tokenSymbol,
        string memory tokenCurrency,
        uint8 tokenDecimals,
        address newMasterMinter,
        address newPauser,
        address newBlacklister,
        address newOwner
    ) public {
        require(newMasterMinter != address(0), "FiatToken: new masterMinter is the zero address");
        require(newPauser != address(0), "FiatToken: new pauser is the zero address");
        require(newBlacklister != address(0), "FiatToken: new blacklister is the zero address");
        require(newOwner != address(0), "FiatToken: new owner is the zero address");

        name = tokenName;
        symbol = tokenSymbol;
        currency = tokenCurrency;
        decimals = tokenDecimals;
        masterMinter = newMasterMinter;
        pauser = newPauser;
        blacklister = newBlacklister;
        _setOwner(newOwner);
    }

    function owner() external view returns (address) {
        return _owner;
    }

    function _setOwner(address newOwner) internal {
        _owner = newOwner;
    }

    function transferOwnership(address newOwner) external onlyOwner {
        require(newOwner != address(0), "Ownable: new owner is the zero address");
        emit OwnershipTransferred(_owner, newOwner);
        _setOwner(newOwner);
    }

    function pause() external onlyPauser {
        paused = true;
        emit Pause();
    }

    function unpause() external onlyPauser {
        paused = false;
        emit Unpause();
    }

    function updatePauser(address _newPauser) external onlyOwner {
        require(_newPauser != address(0), "Pausable: new pauser is the zero address");
        pauser = _newPauser;
        emit PauserChanged(pauser);
    }

    function isBlacklisted(address _account) external view returns (bool) {
        return _isBlacklisted(_account);
    }

    function blacklist(address _account) external onlyBlacklister {
        _blacklist(_account);
        emit Blacklisted(_account);
    }

    function unBlacklist(address _account) external onlyBlacklister {
        _unBlacklist(_account);
        emit UnBlacklisted(_account);
    }

    function updateBlacklister(address _newBlacklister) external onlyOwner {
        require(_newBlacklister != address(0), "Blacklistable: new blacklister is the zero address");
        blacklister = _newBlacklister;
        emit BlacklisterChanged(blacklister);
    }

    function _isBlacklisted(address _account) internal view returns (bool) {
        return balanceAndBlacklistStates[_account] >> 255 == 1;
    }

    function _blacklist(address _account) internal {
        _setBlacklistState(_account, true);
    }

    function _unBlacklist(address _account) internal {
        _setBlacklistState(_account, false);
    }

    function _setBlacklistState(address _account, bool _shouldBlacklist) internal {
        balanceAndBlacklistStates[_account] = _shouldBlacklist
            ? balanceAndBlacklistStates[_account] | (1 << 255)
            : _balanceOf(_account);
    }

    function rescuer() external view returns (address) {
        return _rescuer;
    }

    function rescueERC20(IERC20 tokenContract, address to, uint256 amount) external onlyRescuer {
        tokenContract.safeTransfer(to, amount);
    }

    function updateRescuer(address newRescuer) external onlyOwner {
        require(newRescuer != address(0), "Rescuable: new rescuer is the zero address");
        _rescuer = newRescuer;
        emit RescuerChanged(newRescuer);
    }

    function DOMAIN_SEPARATOR() external view returns (bytes32) {
        return _domainSeparator();
    }

    function _domainSeparator() internal view returns (bytes32) {
        return keccak256(
            abi.encode(
                // keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)")
                0x8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f,
                keccak256(bytes(name)),
                keccak256(bytes("1")),
                _chainId(),
                address(this)
            )
        );
    }

    function _chainId() internal view returns (uint256) {
        uint256 chainId;
        assembly { chainId := chainid() }
        return chainId;
    }

    function totalSupply() external view returns (uint256) {
        return totalSupply_;
    }

    function balanceOf(address account) external view returns (uint256) {
        return _balanceOf(account);
    }

    function _balanceOf(address _account) internal view returns (uint256) {
        return balanceAndBlacklistStates[_account] & ((1 << 255) - 1);
    }

    function _setBalance(address _account, uint256 _balance) internal {
        require(_balance <= ((1 << 255) - 1), "FiatToken_2: Balance exceeds (2^255 - 1)");
        require(!_isBlacklisted(_account), "FiatToken_2: Account is blacklisted");
        balanceAndBlacklistStates[_account] = _balance;
    }

    function allowance(address owner, address spender) external view returns (uint256) {
        return allowed[owner][spender];
    }

    function transfer(address to, uint256 value)
    external
    whenNotPaused
    notBlacklisted(msg.sender)
    notBlacklisted(to)
    returns (bool)
    {
        _transfer(msg.sender, to, value);
        return true;
    }

    function transferFrom(address from, address to, uint256 value)
    external
    whenNotPaused
    notBlacklisted(msg.sender)
    notBlacklisted(from)
    notBlacklisted(to)
    returns (bool)
    {
        require(value <= allowed[from][msg.sender], "ERC20: transfer amount exceeds allowance");
        _transfer(from, to, value);
        allowed[from][msg.sender] = allowed[from][msg.sender].sub(value);
        return true;
    }

    function _transfer(address from, address to, uint256 value) internal {
        require(from != address(0), "ERC20: transfer from the zero address");
        require(to != address(0), "ERC20: transfer to the zero address");
        require(value <= _balanceOf(from), "ERC20: transfer amount exceeds balance");
        _setBalance(from, _balanceOf(from).sub(value));
        _setBalance(to, _balanceOf(to).add(value));
        emit Transfer(from, to, value);
    }

    function approve(address spender, uint256 value)
    external
    whenNotPaused
    returns (bool)
    {
        _approve(msg.sender, spender, value);
        return true;
    }

    function _approve(address owner, address spender, uint256 value) internal {
        require(owner != address(0), "ERC20: approve from the zero address");
        require(spender != address(0), "ERC20: approve to the zero address");
        allowed[owner][spender] = value;
        emit Approval(owner, spender, value);
    }

    function increaseAllowance(address spender, uint256 increment)
    external
    whenNotPaused
    returns (bool)
    {
        _increaseAllowance(msg.sender, spender, increment);
        return true;
    }

    function decreaseAllowance(address spender, uint256 decrement)
    external
    whenNotPaused
    returns (bool)
    {
        _decreaseAllowance(msg.sender, spender, decrement);
        return true;
    }

    function _increaseAllowance(address owner, address spender, uint256 increment) internal {
        _approve(owner, spender, allowed[owner][spender].add(increment));
    }

    function _decreaseAllowance(address owner, address spender, uint256 decrement) internal {
        _approve(owner, spender, allowed[owner][spender].sub(decrement, "ERC20: decreased allowance below zero"));
    }

    function mint(address _to, uint256 _amount)
    external
    whenNotPaused
    onlyMinters
    notBlacklisted(msg.sender)
    notBlacklisted(_to)
    returns (bool)
    {
        require(_to != address(0), "FiatToken: mint to the zero address");
        require(_amount > 0, "FiatToken: mint amount not greater than 0");
        uint256 mintingAllowedAmount = minterAllowed[msg.sender];
        require(_amount <= mintingAllowedAmount, "FiatToken: mint amount exceeds minterAllowance");
        totalSupply_ = totalSupply_.add(_amount);
        _setBalance(_to, _balanceOf(_to).add(_amount));
        minterAllowed[msg.sender] = mintingAllowedAmount.sub(_amount);
        emit Mint(msg.sender, _to, _amount);
        emit Transfer(address(0), _to, _amount);
        return true;
    }

    function burn(uint256 _amount)
    external
    whenNotPaused
    onlyMinters
    notBlacklisted(msg.sender)
    {
        uint256 balance = _balanceOf(msg.sender);
        require(_amount > 0, "FiatToken: burn amount not greater than 0");
        require(balance >= _amount, "FiatToken: burn amount exceeds balance");
        totalSupply_ = totalSupply_.sub(_amount);
        _setBalance(msg.sender, balance.sub(_amount));
        emit Burn(msg.sender, _amount);
        emit Transfer(msg.sender, address(0), _amount);
    }

    function minterAllowance(address minter) external view returns (uint256) {
        return minterAllowed[minter];
    }

    function isMinter(address account) external view returns (bool) {
        return minters[account];
    }

    function configureMinter(address minter, uint256 minterAllowedAmount)
    external
    whenNotPaused
    onlyMasterMinter
    returns (bool)
    {
        minters[minter] = true;
        minterAllowed[minter] = minterAllowedAmount;
        emit MinterConfigured(minter, minterAllowedAmount);
        return true;
    }

    function removeMinter(address minter) external onlyMasterMinter returns (bool) {
        minters[minter] = false;
        minterAllowed[minter] = 0;
        emit MinterRemoved(minter);
        return true;
    }

    function updateMasterMinter(address _newMasterMinter) external onlyOwner {
        require(_newMasterMinter != address(0), "FiatToken: new masterMinter is the zero address");
        masterMinter = _newMasterMinter;
        emit MasterMinterChanged(masterMinter);
    }

    function authorizationState(address authorizer, bytes32 nonce) external view returns (bool) {
        return _authorizationStates[authorizer][nonce];
    }

    function transferWithAuthorization(
        address from,
        address to,
        uint256 value,
        uint256 validAfter,
        uint256 validBefore,
        bytes32 nonce,
        uint8 v,
        bytes32 r,
        bytes32 s
    ) external whenNotPaused notBlacklisted(from) notBlacklisted(to) {
        _transferWithAuthorization(from, to, value, validAfter, validBefore, nonce, abi.encodePacked(r, s, v));
    }

    function transferWithAuthorization(
        address from,
        address to,
        uint256 value,
        uint256 validAfter,
        uint256 validBefore,
        bytes32 nonce,
        bytes memory signature
    ) external whenNotPaused notBlacklisted(from) notBlacklisted(to) {
        _transferWithAuthorization(from, to, value, validAfter, validBefore, nonce, signature);
    }

    function _transferWithAuthorization(
        address from,
        address to,
        uint256 value,
        uint256 validAfter,
        uint256 validBefore,
        bytes32 nonce,
        bytes memory signature
    ) internal {
        _requireValidAuthorization(from, nonce, validAfter, validBefore);
        _requireValidSignature(
            from,
            keccak256(abi.encode(TRANSFER_WITH_AUTHORIZATION_TYPEHASH, from, to, value, validAfter, validBefore, nonce)),
            signature
        );
        _markAuthorizationAsUsed(from, nonce);
        _transfer(from, to, value);
    }

    function receiveWithAuthorization(
        address from,
        address to,
        uint256 value,
        uint256 validAfter,
        uint256 validBefore,
        bytes32 nonce,
        uint8 v,
        bytes32 r,
        bytes32 s
    ) external whenNotPaused notBlacklisted(from) notBlacklisted(to) {
        _receiveWithAuthorization(from, to, value, validAfter, validBefore, nonce, abi.encodePacked(r, s, v));
    }

    function receiveWithAuthorization(
        address from,
        address to,
        uint256 value,
        uint256 validAfter,
        uint256 validBefore,
        bytes32 nonce,
        bytes memory signature
    ) external whenNotPaused notBlacklisted(from) notBlacklisted(to) {
        _receiveWithAuthorization(from, to, value, validAfter, validBefore, nonce, signature);
    }

    function _receiveWithAuthorization(
        address from,
        address to,
        uint256 value,
        uint256 validAfter,
        uint256 validBefore,
        bytes32 nonce,
        bytes memory signature
    ) internal {
        require(to == msg.sender, "FiatToken: caller must be the payee");
        _requireValidAuthorization(from, nonce, validAfter, validBefore);
        _requireValidSignature(
            from,
            keccak256(abi.encode(RECEIVE_WITH_AUTHORIZATION_TYPEHASH, from, to, value, validAfter, validBefore, nonce)),
            signature
        );
        _markAuthorizationAsUsed(from, nonce);
        _transfer(from, to, value);
    }

    function cancelAuthorization(
        address authorizer,
        bytes32 nonce,
        uint8 v,
        bytes32 r,
        bytes32 s
    ) external whenNotPaused {
        _cancelAuthorization(authorizer, nonce, abi.encodePacked(r, s, v));
    }

    function cancelAuthorization(
        address authorizer,
        bytes32 nonce,
        bytes memory signature
    ) external whenNotPaused {
        _cancelAuthorization(authorizer, nonce, signature);
    }

    function _cancelAuthorization(
        address authorizer,
        bytes32 nonce,
        bytes memory signature
    ) internal {
        _requireUnusedAuthorization(authorizer, nonce);
        _requireValidSignature(
            authorizer,
            keccak256(abi.encode(CANCEL_AUTHORIZATION_TYPEHASH, authorizer, nonce)),
            signature
        );
        _authorizationStates[authorizer][nonce] = true;
        emit AuthorizationCanceled(authorizer, nonce);
    }

    function _requireValidSignature(
        address signer,
        bytes32 dataHash,
        bytes memory signature
    ) private view {
        require(
            isValidSignatureNow(
                signer,
                toTypedDataHash(_domainSeparator(), dataHash),
                signature
            ),
            "FiatToken: invalid signature"
        );
    }

    function _requireUnusedAuthorization(address authorizer, bytes32 nonce) private view {
        require(!_authorizationStates[authorizer][nonce], "FiatToken: authorization is used or canceled");
    }

    function _requireValidAuthorization(
        address authorizer,
        bytes32 nonce,
        uint256 validAfter,
        uint256 validBefore
    ) private view {
        require(block.timestamp > validAfter, "FiatToken: authorization is not yet valid");
        require(block.timestamp < validBefore, "FiatToken: authorization is expired");
        _requireUnusedAuthorization(authorizer, nonce);
    }

    function _markAuthorizationAsUsed(address authorizer, bytes32 nonce) private {
        _authorizationStates[authorizer][nonce] = true;
        emit AuthorizationUsed(authorizer, nonce);
    }

    function nonces(address owner) external view returns (uint256) {
        return _permitNonces[owner];
    }

    function permit(
        address owner,
        address spender,
        uint256 value,
        uint256 deadline,
        uint8 v,
        bytes32 r,
        bytes32 s
    ) external whenNotPaused {
        _permit(owner, spender, value, deadline, abi.encodePacked(r, s, v));
    }

    function permit(
        address owner,
        address spender,
        uint256 value,
        uint256 deadline,
        bytes memory signature
    ) external whenNotPaused {
        _permit(owner, spender, value, deadline, signature);
    }

    function _permit(
        address owner,
        address spender,
        uint256 value,
        uint256 deadline,
        bytes memory signature
    ) internal {
        require(deadline == type(uint256).max || deadline >= block.timestamp, "FiatToken: permit is expired");
        bytes32 typedDataHash = toTypedDataHash(
            _domainSeparator(),
            keccak256(abi.encode(PERMIT_TYPEHASH, owner, spender, value, _permitNonces[owner]++, deadline))
        );
        require(isValidSignatureNow(owner, typedDataHash, signature), "EIP2612: invalid signature");
        _approve(owner, spender, value);
    }

    function version() external pure returns (string memory) {
        return "1";
    }

    function toTypedDataHash(bytes32 domainSeparator, bytes32 structHash)
    internal
    pure
    returns (bytes32 digest)
    {
        assembly {
            let ptr := mload(0x40)
            mstore(ptr, "\x19\x01")
            mstore(add(ptr, 0x02), domainSeparator)
            mstore(add(ptr, 0x22), structHash)
            digest := keccak256(ptr, 0x42)
        }
    }

    function isValidSignatureNow(
        address signer,
        bytes32 digest,
        bytes memory signature
    ) public view returns (bool) {
        if (!isContract(signer)) {
            return recover(digest, signature) == signer;
        }
        return isValidERC1271SignatureNow(signer, digest, signature);
    }

    function isValidERC1271SignatureNow(
        address signer,
        bytes32 digest,
        bytes memory signature
    ) internal view returns (bool) {
        (bool success, bytes memory result) = signer.staticcall(
            abi.encodeWithSelector(
                IERC1271.isValidSignature.selector,
                digest,
                signature
            )
        );
        return (success &&
        result.length >= 32 &&
            abi.decode(result, (bytes32)) ==
            bytes32(IERC1271.isValidSignature.selector));
    }

    function isContract(address addr) internal view returns (bool) {
        uint256 size;
        assembly {
            size := extcodesize(addr)
        }
        return size > 0;
    }

    function recover(
        bytes32 digest,
        uint8 v,
        bytes32 r,
        bytes32 s
    ) internal pure returns (address) {
        if (
            uint256(s) >
            0x7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF5D576E7357A4501DDFE92F46681B20A0
        ) {
            revert("invalid signature 's' value");
        }

        if (v != 27 && v != 28) {
            revert("invalid signature 'v' value");
        }

        address signer = ecrecover(digest, v, r, s);
        require(signer != address(0), "invalid signature");

        return signer;
    }

    function recover(bytes32 digest, bytes memory signature)
    internal
    pure
    returns (address)
    {
        require(signature.length == 65, "invalid signature length");

        bytes32 r;
        bytes32 s;
        uint8 v;

        assembly {
            r := mload(add(signature, 0x20))
            s := mload(add(signature, 0x40))
            v := byte(0, mload(add(signature, 0x60)))
        }
        return recover(digest, v, r, s);
    }
}
