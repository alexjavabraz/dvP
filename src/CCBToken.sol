// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

// OpenZeppelin Contracts 5.x (ate a 5.4.0 para EVM Paris)

import {ERC721} from "@openzeppelin/contracts@5.4.0/token/ERC721/ERC721.sol";
import {Ownable} from "@openzeppelin/contracts@5.4.0/access/Ownable.sol";

// CCB como NFT ERC-721 que permanece na carteira do holder ate o DvP.
// Quando entra numa cesta, a CCB e apenas BLOQUEADA (continua visivel na carteira).
// Somente o contrato NucleaDvP autorizado pode bloquear, desbloquear e liquidar.
contract CCBToken is ERC721, Ownable {
    struct CCBData { string ipoc; uint256 faceValue; uint64 maturity; }

    uint256 public nextTokenId = 1;
    mapping(uint256 => CCBData) public ccbData;
    mapping(bytes32 => bool) public referenceUsed;
    /// @notice CCB bloqueada em cesta: nao pode ser transferida fora do DvP.
    mapping(uint256 => bool) public locked;
    address public dvp;

    event CCBIssued(uint256 indexed tokenId, address indexed holder, bytes32 indexed referenceHash);
    event DvPAuthorized(address indexed dvp);
    event CCBLockChanged(uint256 indexed tokenId, bool locked);
    event AdminCCBTransferred(address indexed administrator, address indexed from, address indexed to, uint256 tokenId);

    modifier onlyDvP() { require(msg.sender == dvp && dvp != address(0), "DvP only"); _; }

    // OZ 5.x: o dono vai no construtor do Ownable
    constructor(address admin) ERC721("Nuclea CCB", "CCB") Ownable(admin) {}

    /// @notice Ligacao unica com o contrato NucleaDvP.
    function setDvP(address dvp_) external onlyOwner {
        require(dvp == address(0), "DvP already set");
        require(dvp_.code.length > 0, "Invalid DvP");
        dvp = dvp_;
        emit DvPAuthorized(dvp_);
    }

    /// @param ipoc IPOC unico da CCB.
    /// @param faceValue Valor nominal em unidades minimas do BRLN.
    /// @param maturity Vencimento em timestamp Unix (segundos).
    function mintCCB(address holder, string calldata ipoc, uint256 faceValue, uint64 maturity)
        external onlyOwner returns (uint256 tokenId)
    {
        require(bytes(ipoc).length > 0 && faceValue > 0, "Invalid CCB");
        require(maturity > block.timestamp, "Invalid maturity");
        bytes32 refHash = keccak256(bytes(ipoc));
        require(!referenceUsed[refHash], "Reference already used");
        referenceUsed[refHash] = true;
        tokenId = nextTokenId++;
        ccbData[tokenId] = CCBData(ipoc, faceValue, maturity);
        _mint(holder, tokenId);
        emit CCBIssued(tokenId, holder, refHash);
    }

    /// @notice Nuclea movimenta CCB livre (nao bloqueada) em nome do holder.
    function adminTransferCCB(address from, address to, uint256 tokenId) external onlyOwner {
        _transfer(from, to, tokenId);
        emit AdminCCBTransferred(msg.sender, from, to, tokenId);
    }

    // ------------------------------------------------------------ DvP only

    function setLock(uint256 tokenId, bool value) external onlyDvP {
        _requireOwned(tokenId); // OZ 5.x: substitui _exists (reverte se a CCB nao existe)
        locked[tokenId] = value;
        emit CCBLockChanged(tokenId, value);
    }

    /// @notice Perna de entrega do DvP: holder -> comprador, sem approve do holder.
    function dvpTransfer(address from, address to, uint256 tokenId) external onlyDvP {
        require(locked[tokenId], "CCB not locked");
        locked[tokenId] = false;
        _transfer(from, to, tokenId);
        emit CCBLockChanged(tokenId, false);
    }

    /// @dev OZ 5.x: _update substitui _beforeTokenTransfer. Toda transferencia passa aqui
    ///      (holder, approve, admin); mint (from == 0) e liberado.
    function _update(address to, uint256 tokenId, address auth) internal override returns (address) {
        address from = _ownerOf(tokenId);
        require(from == address(0) || !locked[tokenId], "CCB locked");
        return super._update(to, tokenId, auth);
    }
}
