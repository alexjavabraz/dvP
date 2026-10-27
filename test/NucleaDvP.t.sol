// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import "forge-std/Test.sol";

interface ICCB {
    function setDvP(address dvp) external;
    function mintCCB(address holder, string calldata ipoc, uint256 faceValue, uint64 maturity) external returns (uint256);
    function adminTransferCCB(address from, address to, uint256 tokenId) external;
    function ownerOf(uint256 id) external view returns (address);
    function balanceOf(address owner) external view returns (uint256);
    function locked(uint256 id) external view returns (bool);
    function transferFrom(address from, address to, uint256 id) external;
    function setLock(uint256 id, bool v) external;
}
interface IDvP {
    struct Basket { address holder; uint8 state; uint64 minMaturity; uint256 totalFaceValue; uint256 tradeId; }
    struct Trade { uint256 basketId; address holder; address buyer; uint256 price; uint64 expiresAt; uint8 status; }
    function setNucleaOperator(address op, bool ok) external;
    function setBee4Operator(address op, bool ok) external;
    function createBasket(address holder, uint256[] calldata ids) external returns (uint256);
    function addCCBs(uint256 basketId, uint256[] calldata ids) external;
    function dissolveBasket(uint256 basketId) external;
    function registerTrade(uint256 basketId, address buyer, uint256 price, uint64 expiresAt) external returns (uint256);
    function executeDvP(uint256 tradeId) external;
    function cancelTrade(uint256 tradeId) external;
    function getBasket(uint256 basketId) external view returns (Basket memory);
    function getTrade(uint256 tradeId) external view returns (Trade memory);
    function canExecute(uint256 tradeId) external view returns (bool, string memory);
    function basketCCBs(uint256 basketId) external view returns (uint256[] memory, string[] memory, uint256[] memory, uint64[] memory);
}

/// BRLN simulado (fixture), 6 casas, com congelamento de conta.
contract MockBRLN {
    uint8 public decimals = 6;
    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;
    mapping(address => bool) public frozen;
    function mint(address to, uint256 v) external { balanceOf[to] += v; }
    function freeze(address a, bool f) external { frozen[a] = f; }
    function approve(address s, uint256 v) external returns (bool) { allowance[msg.sender][s] = v; return true; }
    function transfer(address to, uint256 v) external returns (bool) { _move(msg.sender, to, v); return true; }
    function transferFrom(address f, address to, uint256 v) external returns (bool) {
        allowance[f][msg.sender] -= v; _move(f, to, v); return true;
    }
    function _move(address f, address to, uint256 v) private {
        require(!frozen[f] && !frozen[to], "BRLN: frozen");
        balanceOf[f] -= v; balanceOf[to] += v;
    }
}

contract NucleaDvPTest is Test {
    uint8 constant OPEN = 1; uint8 constant IN_TRADE = 2; uint8 constant B_SETTLED = 3; uint8 constant DISSOLVED = 4;
    uint8 constant REGISTERED = 1; uint8 constant EXECUTED = 2; uint8 constant CANCELLED = 3;

    address admin = makeAddr("NUCLEA_ADMIN");
    address nuclea = makeAddr("NUCLEA_OPERADOR");
    address bee4 = makeAddr("BEE4");
    address holder = makeAddr("HOLDER");
    address buyer = makeAddr("COMPRADOR");
    address outsider = makeAddr("TERCEIRO");

    ICCB ccb; IDvP dvp; MockBRLN brln;
    uint64 maturity;
    uint256 constant PRICE = 150_000_000;
    uint256 constant BUYER_FUNDS = 1_000_000_000;

    function setUp() public {
        vm.warp(1_800_000_000);
        maturity = uint64(block.timestamp + 365 days);
        brln = new MockBRLN();
        ccb = ICCB(deployCode("CCBToken.sol:CCBToken", abi.encode(admin)));
        dvp = IDvP(deployCode("NucleaDvP.sol:NucleaDvP", abi.encode(address(ccb), address(brln), admin)));
        vm.startPrank(admin);
        ccb.setDvP(address(dvp));
        dvp.setNucleaOperator(nuclea, true);
        dvp.setBee4Operator(bee4, true);
        ccb.mintCCB(holder, "IPOC-001", 100_000_000, maturity);
        ccb.mintCCB(holder, "IPOC-002", 50_000_000, maturity + 30 days);
        ccb.mintCCB(holder, "IPOC-003", 25_000_000, maturity - 30 days);
        vm.stopPrank();
        brln.mint(buyer, BUYER_FUNDS);
        vm.prank(buyer); brln.approve(address(dvp), type(uint256).max); // onboarding da carteira custodiada
    }

    function _ids(uint256 a) internal pure returns (uint256[] memory r) { r = new uint256[](1); r[0] = a; }
    function _ids(uint256 a, uint256 b) internal pure returns (uint256[] memory r) { r = new uint256[](2); r[0] = a; r[1] = b; }

    function _basket() internal returns (uint256 id) {
        vm.startPrank(nuclea);
        id = dvp.createBasket(holder, _ids(1, 2));
        dvp.addCCBs(id, _ids(3));
        vm.stopPrank();
    }

    function _register(uint256 basketId) internal returns (uint256) {
        vm.prank(bee4); // BEE4 define comprador e preco
        return dvp.registerTrade(basketId, buyer, PRICE, uint64(block.timestamp + 1 days));
    }

    // 1. Fluxo completo; ativos sempre nas carteiras
    function test_fullFlow_assetsStayInWallets() public {
        uint256 id = _basket();
        assertEq(ccb.balanceOf(holder), 3);           // continuam na carteira do holder
        assertTrue(ccb.locked(1) && ccb.locked(3));
        assertEq(dvp.getBasket(id).totalFaceValue, 175_000_000);
        assertEq(dvp.getBasket(id).minMaturity, maturity - 30 days);

        uint256 tid = _register(id);
        assertEq(brln.balanceOf(buyer), BUYER_FUNDS); // BRLN continua na carteira do comprador
        (bool ok,) = dvp.canExecute(tid);
        assertTrue(ok);

        vm.prank(nuclea); dvp.executeDvP(tid);
        assertEq(ccb.balanceOf(holder), 0);
        assertEq(ccb.balanceOf(buyer), 3);
        assertFalse(ccb.locked(1));
        assertEq(brln.balanceOf(holder), PRICE);
        assertEq(brln.balanceOf(buyer), BUYER_FUNDS - PRICE);
        assertEq(brln.balanceOf(address(dvp)), 0);    // nada custodiado no contrato
        assertEq(ccb.balanceOf(address(dvp)), 0);
        assertEq(dvp.getTrade(tid).status, EXECUTED);
        assertEq(dvp.getBasket(id).state, B_SETTLED);

        vm.prank(nuclea); vm.expectRevert("Trade not registered"); dvp.executeDvP(tid); // sem duplicidade
    }

    // 2. Atomicidade: falha no BRLN desfaz tambem a entrega das CCBs
    function test_atomic_paymentFailureRevertsDelivery() public {
        uint256 tid = _register(_basket());
        brln.freeze(holder, true);
        vm.prank(nuclea); vm.expectRevert("BRLN: frozen"); dvp.executeDvP(tid);
        assertEq(ccb.ownerOf(1), holder);
        assertTrue(ccb.locked(1));
        assertEq(dvp.getTrade(tid).status, REGISTERED);
        brln.freeze(holder, false);
        vm.prank(nuclea); dvp.executeDvP(tid);
        assertEq(ccb.ownerOf(1), buyer);
    }

    // 3. Atomicidade: comprador sem saldo no momento do DvP -> nada se move
    function test_atomic_buyerSpentFundsBeforeDvP() public {
        uint256 tid = _register(_basket());
        vm.prank(buyer); brln.transfer(outsider, BUYER_FUNDS); // saldo saiu da carteira
        (bool ok, string memory reason) = dvp.canExecute(tid);
        assertFalse(ok); assertEq(reason, "Buyer BRLN balance too low");
        vm.prank(nuclea); vm.expectRevert(); dvp.executeDvP(tid);
        assertEq(ccb.balanceOf(holder), 3);
    }

    // 4. CCB bloqueada nao sai da carteira por nenhum outro caminho
    function test_lockedCCBCannotMove() public {
        _basket();
        vm.prank(holder); vm.expectRevert("CCB locked"); ccb.transferFrom(holder, outsider, 1);
        vm.prank(admin); vm.expectRevert("CCB locked"); ccb.adminTransferCCB(holder, outsider, 1);
        vm.prank(outsider); vm.expectRevert("DvP only"); ccb.setLock(1, false);
        vm.prank(nuclea); vm.expectRevert("CCB already in a basket"); dvp.createBasket(holder, _ids(1));
    }

    // 5. Permissoes: Nuclea monta cesta e executa; BEE4 registra
    function test_permissions() public {
        vm.prank(bee4); vm.expectRevert("Nuclea only"); dvp.createBasket(holder, _ids(1));
        uint256 id = _basket();
        vm.prank(nuclea); vm.expectRevert("BEE4 only"); dvp.registerTrade(id, buyer, PRICE, uint64(block.timestamp + 1 days));
        uint256 tid = _register(id);
        vm.prank(bee4); vm.expectRevert("Nuclea only"); dvp.executeDvP(tid);
        vm.prank(outsider); vm.expectRevert("Nuclea only"); dvp.executeDvP(tid);
        vm.prank(outsider); vm.expectRevert("Not authorized"); dvp.cancelTrade(tid);
        vm.prank(admin); dvp.setNucleaOperator(nuclea, false);
        vm.prank(nuclea); vm.expectRevert("Nuclea only"); dvp.executeDvP(tid);
    }

    // 6. BEE4 conhece a cesta; cesta exclusiva enquanto em negociacao
    function test_bee4KnowsBasketAndExclusivity() public {
        uint256 id = _basket();
        (uint256[] memory ids, string[] memory ipocs, uint256[] memory fv, uint64[] memory mat) = dvp.basketCCBs(id);
        assertEq(ids.length, 3);
        assertEq(ipocs[0], "IPOC-001"); assertEq(ipocs[2], "IPOC-003");
        assertEq(fv[1], 50_000_000);
        assertEq(mat[2], maturity - 30 days);
        _register(id);
        vm.prank(bee4); vm.expectRevert("Basket not open"); dvp.registerTrade(id, buyer, PRICE, uint64(block.timestamp + 1 days));
        vm.prank(nuclea); vm.expectRevert("Basket not open"); dvp.addCCBs(id, _ids(1));
        vm.prank(nuclea); vm.expectRevert("Basket not open"); dvp.dissolveBasket(id);
        vm.prank(admin); uint256 c4 = ccb.mintCCB(holder, "IPOC-004", 1, maturity);
        vm.prank(nuclea); uint256 id2 = dvp.createBasket(holder, _ids(c4));
        vm.prank(bee4); vm.expectRevert("Invalid buyer"); dvp.registerTrade(id2, holder, PRICE, uint64(block.timestamp + 1 days));
    }

    // 7. Cancelamento e expiracao; cesta reaberta, desfeita e CCB liberada
    function test_cancelExpireDissolve() public {
        uint256 id = _basket();
        uint256 tid = _register(id);
        vm.prank(bee4); dvp.cancelTrade(tid);
        assertEq(dvp.getTrade(tid).status, CANCELLED);
        assertEq(dvp.getBasket(id).state, OPEN);
        uint256 tid2 = _register(id);
        vm.warp(block.timestamp + 2 days);
        vm.prank(nuclea); vm.expectRevert("Trade expired"); dvp.executeDvP(tid2);
        vm.prank(outsider); dvp.cancelTrade(tid2);
        vm.prank(nuclea); dvp.dissolveBasket(id);
        assertEq(dvp.getBasket(id).state, DISSOLVED);
        assertFalse(ccb.locked(1));
        vm.prank(admin); ccb.adminTransferCCB(holder, outsider, 1); // livre de novo
        assertEq(ccb.ownerOf(1), outsider);
    }

    // 8. Validade do lastro
    function test_collateralMaturity() public {
        vm.prank(admin); uint256 short = ccb.mintCCB(holder, "IPOC-SHORT", 1, uint64(block.timestamp + 2 days));
        vm.prank(nuclea); uint256 id = dvp.createBasket(holder, _ids(1, short));
        vm.prank(bee4); uint256 tid = dvp.registerTrade(id, buyer, PRICE, uint64(block.timestamp + 10 days));
        vm.warp(block.timestamp + 3 days);
        (bool ok, string memory reason) = dvp.canExecute(tid);
        assertFalse(ok); assertEq(reason, "CCB matured");
        vm.prank(nuclea); vm.expectRevert("CCB matured"); dvp.executeDvP(tid);
        vm.startPrank(nuclea);
        dvp.cancelTrade(tid);
        dvp.dissolveBasket(id);
        vm.expectRevert("CCB matured"); dvp.createBasket(holder, _ids(short)); // vencida nao entra
        vm.stopPrank();
    }
}
