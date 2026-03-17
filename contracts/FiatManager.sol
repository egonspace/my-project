// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.0;

import {FiatToken} from "./FiatToken.sol";
import "@openzeppelin/contracts/proxy/utils/UUPSUpgradeable.sol";
import "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";

contract FiatManager is OwnableUpgradeable, UUPSUpgradeable {
    FiatToken public fiat;
    address public admin;
    uint256 public totalAccumulatedMinted;
    uint256 public totalAccumulatedBurnt;
    mapping(uint256 => bool) public usedTxId;
    mapping(address => bool) public authorized;
    mapping(address => uint256) public accumulatedMinted;
    mapping(address => uint256) public accumulatedBurnt;

    event UpgradeImplementation(address indexed _implementation);
    event NewAdminSet(address _old, address indexed _new);
    event NewUserAuthorized(address _user);
    event UserDeauthorized(address _user);

    modifier useTxId(uint256 _txId) {
        require(!usedTxId[_txId], "FiatManager: txId was already used");
        usedTxId[_txId] = true;
        _;
    }

    modifier onlyAdmin() {
        require(admin != address(0), "FiatManager: admin is not set yet");
        require(msg.sender == admin, "FiatManager: only admin can do the operation");
        _;
    }

    modifier onlyAuthorized(address _user) {
        require(authorized[_user], "FiatManager: token owner is not authorized");
        _;
    }

    function _authorizeUpgrade(address newImplementation) internal override onlyOwner{}

    function initialize(
        address _fiatTokenAddress,
        address _admin
    ) public initializer {
        require(_fiatTokenAddress != address(0), "FiatManager: fiat token address cannot be zero");
        require(_admin != address(0), "FiatManager: gateway master address cannot be zero");

        fiat = FiatToken(_fiatTokenAddress);
        admin = _admin;

        emit NewAdminSet(address(0), admin);

        __Ownable_init();
    }

    function upgradeTo(address newImplementation) public override onlyOwner {
        _upgradeToAndCallUUPS(newImplementation, '', false);
        emit UpgradeImplementation(newImplementation);
    }

    function setAdmin(address _newAdmin) external onlyOwner {
        emit NewAdminSet(admin, _newAdmin);

        admin = _newAdmin;
    }

    function authorize(address _user) external onlyAdmin {
        require(!authorized[_user], "FiatManager: _user is already authorized");
        authorized[_user] = true;
        emit NewUserAuthorized(_user);
    }

    function deauthorize(address _user) external onlyAdmin {
        require(authorized[_user], "FiatManager: _user is not authorized");
        authorized[_user] = false;
        emit UserDeauthorized(_user);
    }

    event FiatTokenMinted(uint256 indexed _txId, address indexed _minter, uint256 _amount);
    event FiatTokenBurnt(uint256 indexed _txId, address indexed _minter, uint256 _amount);
    event FiatTokenTransferred(uint256 indexed _txId, address indexed _minter, address _to, uint256 _amount);

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function mintFromFiat(
        address _to,
        uint256 _amount,
        uint256 _txId) external onlyAdmin onlyAuthorized(_to) useTxId(_txId) {

        fiat.mint(_to, _amount);

        accumulatedMinted[_to] += _amount;
        totalAccumulatedMinted += _amount;
        
        emit FiatTokenMinted(_txId, _to, _amount);
    }

    function burnForFiat(
        address _owner,
        uint256 _amount,
        uint256 _permitDeadline,
        bytes memory _permitSignature,
        uint256 _txId) external onlyAdmin onlyAuthorized(_owner) useTxId(_txId) {

        require(_amount % (10**fiat.decimals()) == 0, "FiatGateway: only whole token amounts can be burned");

        fiat.permit(_owner, address(this), _amount, _permitDeadline, _permitSignature);
        fiat.transferFrom(_owner, address(this), _amount);
        fiat.burn(_amount);

        accumulatedBurnt[_owner] += _amount;
        totalAccumulatedBurnt += _amount;

        emit FiatTokenBurnt(_txId, _owner, _amount);
    }

    function transferFrom(
        address _from,
        address _to,
        uint256 _amount,
        uint256 _validAfter,
        uint256 _validBefore,
        bytes32 _nonce,
        bytes memory _signature,
        uint256 _txId) external onlyAdmin useTxId(_txId) {

        fiat.transferWithAuthorization(_from, _to, _amount, _validAfter, _validBefore, _nonce, _signature);

        emit FiatTokenTransferred(_txId, _from, _to, _amount);
    }
}
