# NucleaChain DvP: cesta de CCBs com DvP carteira-a-carteira

POC de registro de CCBs, montagem de cesta de lastro e liquidação DvP (CCB × BRLN) na NucleaChain (Hyperledger Besu, EVM **Paris**).

- A **BEE4** conhece a cesta pelo evento e define o comprador.
- A **Núclea** comanda a troca.
- Os ativos aparecem sempre nas carteiras de vendedor e comprador. Nenhum contrato custodia CCB ou BRLN.

| Arquivo | Contrato | Papel |
| --- | --- | --- |
| `src/CCBToken.sol` | `CCBToken` | CCB como NFT ERC-721 (IPOC, valor nominal, vencimento), com bloqueio na carteira |
| `src/NucleaDvP.sol` | `NucleaDvP` | Cestas, registro da operação pela BEE4 e DvP atômico executado pela Núclea |

O BRLN é o ERC-20 já implantado na NucleaChain. Ele não faz parte deste repositório.

## Quem faz o quê

| Participante | Responsabilidade |
| --- | --- |
| **Núclea** | Emite as CCBs, monta a cesta pelo holder e **comanda a troca** (`executeDvP`) |
| **BEE4** | Lê a cesta, faz seus processos e **define comprador e preço** (`registerTrade`) |
| **Vendedor (holder) e comprador** | Não assinam nada no fluxo. Os ativos ficam visíveis nas carteiras deles. |

## Fluxo

| # | Quem envia | Chamada | Resultado |
| --- | --- | --- | --- |
| 1 | Núclea (admin do token) | `CCBToken.mintCCB(holder, ipoc, faceValue, maturity)` | CCB na carteira do vendedor; `CCBIssued` |
| 2 | Núclea (operador) | `NucleaDvP.createBasket(holder, [ids])` | CCBs bloqueadas na carteira do vendedor. **Evento `BasketCreated`** para a BEE4. |
| 2b | Núclea (opcional) | `NucleaDvP.addCCBs(basketId, [ids])` | Inclui CCBs com a cesta aberta; `BasketUpdated` com a composição completa |
| — | BEE4 (leitura) | `NucleaDvP.basketCCBs(basketId)` | IDs, IPOCs, valores nominais e vencimentos da cesta |
| 3 | **BEE4** | `NucleaDvP.registerTrade(basketId, buyer, price, expiresAt)` | Cesta reservada. **Evento `TradeRegistered`** para a Núclea. |
| 4 | **Núclea** | `NucleaDvP.executeDvP(tradeId)` | **Uma transação:** CCBs vendedor → comprador e BRLN comprador → vendedor. `DvPExecuted`. |

Diagrama de sequência: `docs/NucleaChain_Sequencia_DvP.drawio` (abre no diagrams.net).

**Atomicidade:** se qualquer perna falhar, `executeDvP` reverte inteira e nada se move. Causas possíveis: saldo ou approve do comprador, congelamento no BRLN, CCB vencida ou prazo expirado. Antes de executar, a Núclea pode consultar `canExecute(tradeId)`, que retorna `(ok, motivo)`.

**Cancelamento:** `cancelTrade(tradeId)` pode ser chamado pela Núclea ou pela BEE4 antes do DvP, ou por qualquer conta depois de `expiresAt`. A cesta volta a ficar aberta. `dissolveBasket(basketId)` (Núclea) desbloqueia as CCBs.

**Bloqueio da CCB:** enquanto estiver numa cesta, a CCB não sai da carteira por nenhum caminho (holder, approve, `adminTransferCCB`). Só o `NucleaDvP` a move, no `executeDvP`.

**Campos de data:** `maturity` é o vencimento econômico da CCB. `expiresAt` é o prazo da negociação. Os dois são timestamps Unix em segundos. Valores ficam em unidades mínimas do BRLN (com 6 casas, 100 BRLN = `100000000`).

## Compilação: OpenZeppelin 5.4.0 + EVM Paris

- **OpenZeppelin `5.4.0` fixado nos imports** (`@openzeppelin/contracts@5.4.0/...`). Da 5.5.0 em diante, o OpenZeppelin usa o opcode `mcopy` (Cancun) e não compila para Paris.
- **Solidity ≥ 0.8.24** (`pragma ^0.8.24`), **EVM `paris`**, otimização 200 runs. A partir do 0.8.20 o padrão do compilador é Shanghai (`PUSH0`), então escolha `paris` explicitamente.

## Deploy no Remix (conta ADMIN Núclea, VALUE = 0)

Em *Advanced Configurations*, selecione o compilador 0.8.24 ou mais novo e EVM `paris`. O Remix baixa o OpenZeppelin 5.4.0 pelos imports.

| Ordem | Contrato | Construtor |
| --- | --- | --- |
| 1 | `CCBToken` | `admin` |
| 2 | `NucleaDvP` | `ccb_` = CCBToken, `brln_` = BRLN existente, `admin` |

Configuração (ADMIN):

```text
CCBToken.setDvP(<NucleaDvP>)                  // única vez
NucleaDvP.setNucleaOperator(<NUCLEA_OP>, true)
NucleaDvP.setBee4Operator(<BEE4_OP>, true)
```

**Única assinatura por carteira de cliente:** uma vez por comprador, a Núclea assina `BRLN.approve(<NucleaDvP>, <limite>)` com a chave da carteira do comprador. Isso é necessário porque o BRLN é um ERC-20 externo. O holder de CCB não assina nada.

**Para ver os ativos no MetaMask:**
- BRLN: "Importar token" com o endereço do BRLN.
- CCB: "Importar NFT" com o endereço do CCBToken e o tokenId.

## Eventos

| Contrato | Evento | Quem lê |
| --- | --- | --- |
| CCBToken | `CCBIssued(tokenId, holder, referenceHash)` | Núclea |
| NucleaDvP | **`BasketCreated(basketId, holder, tokenIds, totalFaceValue, minMaturity)`** | **BEE4: conhece a cesta** |
| NucleaDvP | `BasketUpdated(...)` (mesmos campos) | BEE4 |
| NucleaDvP | **`TradeRegistered(tradeId, basketId, holder, buyer, price, expiresAt, tokenIds, totalFaceValue)`** | **Núclea: gatilho do DvP** |
| NucleaDvP | `DvPExecuted(tradeId, basketId, buyer, holder, price)` | BEE4 e Núclea |
| NucleaDvP | `TradeCancelled(tradeId, by)` | BEE4 e Núclea |

Um `executeDvP` repetido reverte com `Trade not registered`, então o listener da Núclea pode reprocessar com segurança, por exemplo num failover entre clouds.

As ABIs estão em `artifacts/*.abi.json`.

## Testes (Foundry)

```bash
forge install foundry-rs/forge-std OpenZeppelin/openzeppelin-contracts@v5.4.0 --no-git
forge test -vv
```

8 cenários:
- fluxo completo, verificando que os ativos ficam nas carteiras e o contrato termina com saldo zero;
- atomicidade com BRLN congelado;
- atomicidade com o comprador sem saldo;
- CCB bloqueada imóvel por todos os caminhos;
- permissões e revogação;
- BEE4 lendo a cesta e cesta exclusiva durante a negociação;
- cancelamento, expiração e desfazimento da cesta;
- vencimento do lastro.

O BRLN do teste é um mock (`MockBRLN`).

## Fora do escopo

POC não auditada. Sem pausa, upgrade ou fracionamento. O IPOC fica legível on-chain (`ccbData`). O `price` não é limitado pelo `totalFaceValue`. A chave ADMIN é única.
