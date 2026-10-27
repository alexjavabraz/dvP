// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @notice O que o NucleaDvP precisa do CCBToken.
interface ICCBTokenDvP {
    function ownerOf(uint256 tokenId) external view returns (address);
    function ccbData(uint256 tokenId) external view returns (string memory ipoc, uint256 faceValue, uint64 maturity);
    function locked(uint256 tokenId) external view returns (bool);
    function setLock(uint256 tokenId, bool value) external;
    function dvpTransfer(address from, address to, uint256 tokenId) external;
}

/// @notice API publica do NucleaDvP: tipos, eventos e funcoes.
///         Integracoes (BEE4, listener Nuclea) podem usar so esta interface.
interface INucleaDvP {
    // ---------------------------------------------------------------- tipos

    enum BasketState { None, Open, InTrade, Settled, Dissolved }
    enum TradeStatus { None, Registered, Executed, Cancelled }

    struct Basket {
        address holder;
        BasketState state;
        uint64 minMaturity;      // menor vencimento entre as CCBs da cesta
        uint256 totalFaceValue;  // soma dos valores nominais (unidades minimas de BRLN)
        uint256 tradeId;
    }

    struct Trade {
        uint256 basketId;
        address holder;
        address buyer;
        uint256 price;           // unidades minimas de BRLN
        uint64 expiresAt;
        TradeStatus status;
    }

    // -------------------------------------------------------------- eventos

    event OperatorSet(address indexed operator, bool nuclea, bool allowed);
    /// @notice Evento lido pela BEE4: composicao completa da cesta.
    event BasketCreated(uint256 indexed basketId, address indexed holder, uint256[] tokenIds,
        uint256 totalFaceValue, uint64 minMaturity);
    /// @notice Nova composicao completa apos addCCBs.
    event BasketUpdated(uint256 indexed basketId, address indexed holder, uint256[] tokenIds,
        uint256 totalFaceValue, uint64 minMaturity);
    event BasketDissolved(uint256 indexed basketId);
    /// @notice Evento lido pela Nuclea: a BEE4 definiu comprador e preco; Nuclea executa o DvP.
    event TradeRegistered(uint256 indexed tradeId, uint256 indexed basketId, address indexed holder,
        address buyer, uint256 price, uint64 expiresAt, uint256[] tokenIds, uint256 totalFaceValue);
    event DvPExecuted(uint256 indexed tradeId, uint256 indexed basketId, address indexed buyer,
        address holder, uint256 price);
    event TradeCancelled(uint256 indexed tradeId, address indexed by);

    // ---------------------------------------------------------------- admin

    function setNucleaOperator(address operator, bool allowed) external;
    function setBee4Operator(address operator, bool allowed) external;

    // --------------------------------------------------------------- Nuclea

    function createBasket(address holder, uint256[] calldata ids) external returns (uint256 basketId);
    function addCCBs(uint256 basketId, uint256[] calldata ids) external;
    function dissolveBasket(uint256 basketId) external;
    function executeDvP(uint256 tradeId) external;

    // ----------------------------------------------------------------- BEE4

    function registerTrade(uint256 basketId, address buyer, uint256 price, uint64 expiresAt)
        external returns (uint256 tradeId);

    // ------------------------------------------------------ Nuclea ou BEE4

    function cancelTrade(uint256 tradeId) external;

    // ---------------------------------------------------------------- views

    function isNucleaOperator(address account) external view returns (bool);
    function isBee4Operator(address account) external view returns (bool);
    function basketCount() external view returns (uint256);
    function tradeCount() external view returns (uint256);
    function getBasket(uint256 basketId) external view returns (Basket memory);
    function basketTokenIds(uint256 basketId) external view returns (uint256[] memory);
    function getTrade(uint256 tradeId) external view returns (Trade memory);
    function basketCCBs(uint256 basketId) external view returns (
        uint256[] memory ids, string[] memory ipocs, uint256[] memory faceValues, uint64[] memory maturities);
    function canExecute(uint256 tradeId) external view returns (bool ok, string memory reason);
}
