// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

// OpenZeppelin Contracts 5.x (ate a 5.4.0 para EVM Paris)
import {IERC20} from "@openzeppelin/contracts@5.4.0/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts@5.4.0/token/ERC20/utils/SafeERC20.sol";
import {Ownable} from "@openzeppelin/contracts@5.4.0/access/Ownable.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts@5.4.0/utils/ReentrancyGuard.sol"; // 5.x: saiu de security/

// Fluxo:
//   Nuclea  createBasket(holder, ids)            CCBs bloqueadas na carteira do holder;
//                                                evento BasketCreated -> BEE4 conhece a cesta
//   BEE4    registerTrade(basketId, buyer, ...)  BEE4 define comprador e preco;
//                                                evento TradeRegistered -> Nuclea
//   Nuclea  executeDvP(tradeId)                  troca atomica carteira-a-carteira:
//                                                CCBs holder -> comprador, BRLN comprador -> holder
// Nenhum ativo fica custodiado em contrato: CCBs e BRLN aparecem sempre nas carteiras.

interface ICCBTokenDvP {
    function ownerOf(uint256 tokenId) external view returns (address);
    function ccbData(uint256 tokenId) external view returns (string memory ipoc, uint256 faceValue, uint64 maturity);
    function locked(uint256 tokenId) external view returns (bool);
    function setLock(uint256 tokenId, bool value) external;
    function dvpTransfer(address from, address to, uint256 tokenId) external;
}

contract NucleaDvP is Ownable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    enum BasketState { None, Open, InTrade, Settled, Dissolved }
    enum TradeStatus { None, Registered, Executed, Cancelled }

    struct Basket {
        address holder;
        BasketState state;
        uint64 minMaturity;
        uint256 totalFaceValue;
        uint256 tradeId;
    }

    struct Trade {
        uint256 basketId;
        address holder;
        address buyer;
        uint256 price;      // unidades minimas de BRLN
        uint64 expiresAt;
        TradeStatus status;
    }

    uint256 public constant MAX_CCB_PER_BASKET = 25;

    ICCBTokenDvP public immutable ccb;
    IERC20 public immutable brln;
    mapping(address => bool) public isNucleaOperator;
    mapping(address => bool) public isBee4Operator;

    uint256 public basketCount;
    uint256 public tradeCount;
    mapping(uint256 => Basket) private _baskets;
    mapping(uint256 => uint256[]) private _tokenIds;
    mapping(uint256 => Trade) private _trades;

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

    modifier onlyNuclea() { require(isNucleaOperator[msg.sender], "Nuclea only"); _; }
    modifier onlyBee4() { require(isBee4Operator[msg.sender], "BEE4 only"); _; }

    // OZ 5.x: o dono vai no construtor do Ownable
    constructor(address ccb_, address brln_, address admin) Ownable(admin) {
        require(ccb_.code.length > 0 && brln_.code.length > 0, "Invalid contracts");
        ccb = ICCBTokenDvP(ccb_);
        brln = IERC20(brln_);
    }

    // ---------------------------------------------------------------- admin

    function setNucleaOperator(address operator, bool allowed) external onlyOwner {
        require(operator != address(0), "Invalid operator");
        isNucleaOperator[operator] = allowed;
        emit OperatorSet(operator, true, allowed);
    }

    function setBee4Operator(address operator, bool allowed) external onlyOwner {
        require(operator != address(0), "Invalid operator");
        isBee4Operator[operator] = allowed;
        emit OperatorSet(operator, false, allowed);
    }

    // ------------------------------------------------- Nuclea pelo holder

    /// @notice Cria a cesta e bloqueia as CCBs na carteira do holder.
    /// @param ids Exemplo no Remix: [1,2]
    function createBasket(address holder, uint256[] calldata ids) external onlyNuclea returns (uint256 basketId) {
        require(holder != address(0), "Invalid holder");
        basketId = ++basketCount;
        _baskets[basketId] = Basket(holder, BasketState.Open, type(uint64).max, 0, 0);
        _add(basketId, ids);
        Basket storage b = _baskets[basketId];
        emit BasketCreated(basketId, holder, ids, b.totalFaceValue, b.minMaturity);
    }

    /// @notice Inclui mais CCBs enquanto a cesta nao estiver em negociacao.
    function addCCBs(uint256 basketId, uint256[] calldata ids) external onlyNuclea {
        Basket storage b = _baskets[basketId];
        require(b.state == BasketState.Open, "Basket not open");
        _add(basketId, ids);
        emit BasketUpdated(basketId, b.holder, _tokenIds[basketId], b.totalFaceValue, b.minMaturity);
    }

    /// @notice Desfaz a cesta e desbloqueia as CCBs.
    function dissolveBasket(uint256 basketId) external onlyNuclea {
        Basket storage b = _baskets[basketId];
        require(b.state == BasketState.Open, "Basket not open");
        b.state = BasketState.Dissolved;
        uint256[] storage ids = _tokenIds[basketId];
        for (uint256 i; i < ids.length; ++i) ccb.setLock(ids[i], false);
        emit BasketDissolved(basketId);
    }

    // ------------------------------------------- BEE4: define o comprador

    /// @notice BEE4 define comprador, preco e prazo; reserva a cesta e avisa a Nuclea.
    function registerTrade(uint256 basketId, address buyer, uint256 price, uint64 expiresAt)
        external onlyBee4 returns (uint256 tradeId)
    {
        Basket storage b = _baskets[basketId];
        require(b.state == BasketState.Open, "Basket not open");
        require(b.minMaturity > block.timestamp, "CCB matured");
        require(buyer != address(0) && buyer != b.holder, "Invalid buyer");
        require(price > 0 && expiresAt > block.timestamp, "Invalid terms");
        tradeId = ++tradeCount;
        _trades[tradeId] = Trade(basketId, b.holder, buyer, price, expiresAt, TradeStatus.Registered);
        b.state = BasketState.InTrade;
        b.tradeId = tradeId;
        emit TradeRegistered(tradeId, basketId, b.holder, buyer, price, expiresAt, _tokenIds[basketId], b.totalFaceValue);
    }

    // ------------------------------------------------------- Nuclea: DvP

    /// @notice Nuclea comanda a troca. Uma unica transacao: se qualquer perna falhar, tudo reverte.
    function executeDvP(uint256 tradeId) external onlyNuclea nonReentrant {
        Trade storage t = _trades[tradeId];
        require(t.status == TradeStatus.Registered, "Trade not registered");
        require(block.timestamp < t.expiresAt, "Trade expired");
        Basket storage b = _baskets[t.basketId];
        require(b.minMaturity > block.timestamp, "CCB matured");

        t.status = TradeStatus.Executed;
        b.state = BasketState.Settled;

        // Perna 1 - entrega: CCBs da carteira do holder para a do comprador
        uint256[] storage ids = _tokenIds[t.basketId];
        for (uint256 i; i < ids.length; ++i) ccb.dvpTransfer(t.holder, t.buyer, ids[i]);

        // Perna 2 - pagamento: BRLN da carteira do comprador para a do holder
        uint256 beforeBalance = brln.balanceOf(t.holder);
        brln.safeTransferFrom(t.buyer, t.holder, t.price);
        require(brln.balanceOf(t.holder) == beforeBalance + t.price, "Incorrect BRLN payment");

        emit DvPExecuted(tradeId, t.basketId, t.buyer, t.holder, t.price);
    }

    /// @notice Nuclea ou BEE4 cancelam antes do DvP; depois do prazo, qualquer conta.
    ///         A cesta volta a ficar aberta, com as CCBs ainda bloqueadas.
    function cancelTrade(uint256 tradeId) external {
        Trade storage t = _trades[tradeId];
        require(t.status == TradeStatus.Registered, "Trade not registered");
        require(isNucleaOperator[msg.sender] || isBee4Operator[msg.sender] || block.timestamp >= t.expiresAt,
            "Not authorized");
        t.status = TradeStatus.Cancelled;
        Basket storage b = _baskets[t.basketId];
        b.state = BasketState.Open;
        b.tradeId = 0;
        emit TradeCancelled(tradeId, msg.sender);
    }

    // --------------------------------------------------------------- views

    function getBasket(uint256 basketId) external view returns (Basket memory) { return _baskets[basketId]; }
    function basketTokenIds(uint256 basketId) external view returns (uint256[] memory) { return _tokenIds[basketId]; }
    function getTrade(uint256 tradeId) external view returns (Trade memory) { return _trades[tradeId]; }

    /// @notice Detalhe de cada CCB da cesta, para a BEE4 conhecer o lastro.
    function basketCCBs(uint256 basketId) external view returns (
        uint256[] memory ids, string[] memory ipocs, uint256[] memory faceValues, uint64[] memory maturities)
    {
        ids = _tokenIds[basketId];
        ipocs = new string[](ids.length);
        faceValues = new uint256[](ids.length);
        maturities = new uint64[](ids.length);
        for (uint256 i; i < ids.length; ++i) (ipocs[i], faceValues[i], maturities[i]) = ccb.ccbData(ids[i]);
    }

    /// @notice Pre-checagem para a Nuclea antes de enviar executeDvP.
    function canExecute(uint256 tradeId) external view returns (bool ok, string memory reason) {
        Trade storage t = _trades[tradeId];
        if (t.status != TradeStatus.Registered) return (false, "Trade not registered");
        if (block.timestamp >= t.expiresAt) return (false, "Trade expired");
        if (_baskets[t.basketId].minMaturity <= block.timestamp) return (false, "CCB matured");
        if (brln.balanceOf(t.buyer) < t.price) return (false, "Buyer BRLN balance too low");
        if (brln.allowance(t.buyer, address(this)) < t.price) return (false, "Buyer BRLN allowance too low");
        return (true, "");
    }

    function _add(uint256 basketId, uint256[] calldata ids) private {
        Basket storage b = _baskets[basketId];
        require(ids.length > 0 && _tokenIds[basketId].length + ids.length <= MAX_CCB_PER_BASKET, "Invalid basket size");
        for (uint256 i; i < ids.length; ++i) {
            uint256 id = ids[i];
            require(ccb.ownerOf(id) == b.holder, "Not holder's CCB");
            require(!ccb.locked(id), "CCB already in a basket");
            (, uint256 faceValue, uint64 maturity) = ccb.ccbData(id);
            require(maturity > block.timestamp, "CCB matured");
            ccb.setLock(id, true);
            _tokenIds[basketId].push(id);
            b.totalFaceValue += faceValue;
            if (maturity < b.minMaturity) b.minMaturity = maturity;
        }
    }
}
