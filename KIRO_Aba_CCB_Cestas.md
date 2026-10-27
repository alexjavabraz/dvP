# Spec: aba "CCBs e Cestas de Lastro" no front-end existente

> **Para o Kiro.** Esta spec adiciona uma nova aba ao front-end **existente** para operar os contratos `CCBToken` e `NucleaDvP`, que **já estão implantados** na rede do cliente (Hyperledger Besu, EVM Paris).
> A spec tem requisitos (formato EARS), design e tarefas. Pode ser salva em `.kiro/specs/ccb-cestas/` ou usada diretamente.
> **Não crie um app novo, não troque bibliotecas e não refaça o deploy dos contratos.** Siga os padrões que o app já usa.

---

## 0. Descoberta obrigatória antes de codar

O Kiro deve inspecionar o projeto e registrar as respostas no início do `design` antes de implementar:

1. **Framework e roteamento:** React, Vue, Angular ou outro? Como as abas/rotas existentes são declaradas? A nova aba deve seguir exatamente o mesmo padrão: menu, rota, layout e componentes de UI.
2. **Biblioteca web3 em uso:** ethers v5/v6, viem/wagmi, web3.js ou outra. **Use a mesma.** Os exemplos desta spec estão em ethers v6 e devem ser traduzidos para a biblioteca do app.
3. **Conexão de carteira:** como o app obtém o `signer`/`walletClient` e a conta conectada? Como trata troca de conta e de rede?
4. **Configuração de rede:** onde ficam RPC, `chainId` e endereços de contratos (env, arquivo de config, banco)? Os novos endereços entram no mesmo lugar.
5. **Como o app lida com Solidity/ABIs:** ele já carrega ABIs (JSON, geradas de `.sol`, typechain/wagmi codegen)? Reaproveite o mesmo mecanismo para as ABIs abaixo.
6. **Envio de transações:** existe um helper para enviar transação, esperar o recibo e exibir status/erro (toasts, modais)? Reaproveite-o. A rede Besu do cliente pode ter `gasPrice = 0`, então use a mesma configuração de gas que as outras telas usam.
7. **Componentes de tabela, formulário, badge e modal:** use os do app. Não introduza uma nova biblioteca de UI.

---

## 1. Contexto do domínio

| Termo | Significado |
| --- | --- |
| **CCB** | Cédula de Crédito Bancário, representada como NFT ERC-721 no contrato `CCBToken`. Cada CCB tem `ipoc` (identificador único), `faceValue` (valor nominal) e `maturity` (vencimento). |
| **Holder** | Carteira titular da CCB (o vendedor). A CCB **fica sempre na carteira do holder**: entrar numa cesta só a **bloqueia**. |
| **Cesta de lastro** | Conjunto de 1 a 25 CCBs do **mesmo holder**, criado no `NucleaDvP`. Enquanto está na cesta, a CCB não pode ser transferida. |
| **BRLN** | Stablecoin ERC-20 já existente na rede. O endereço é lido de `NucleaDvP.brln()`. Os valores (`faceValue`, `price`) estão em **unidades mínimas** do BRLN. |
| **DvP** | Troca atômica CCBs × BRLN entre carteiras (fase futura, seção 8). |

Fluxo completo (só os passos 1 e 2 entram nesta entrega):

1. **Núclea (owner do CCBToken)** emite a CCB → `CCBToken.mintCCB(...)` → evento `CCBIssued`.
2. **Núclea (operador)** cria a cesta → `NucleaDvP.createBasket(holder, ids)` → evento `BasketCreated`.
3. *(futuro)* **BEE4** registra comprador e preço → `registerTrade` → `TradeRegistered`.
4. *(futuro)* **Núclea** executa o DvP → `executeDvP` → `DvPExecuted`.

Código-fonte, ABIs completas e testes: repositório `alexjavabraz/dvP` (`src/`, `artifacts/*.abi.json`).

---

## 2. Configuração

Adicionar à configuração de rede do app, no mesmo lugar dos outros contratos:

| Chave | Valor |
| --- | --- |
| `CCB_TOKEN_ADDRESS` | Endereço do `CCBToken` implantado *(o cliente preenche)* |
| `NUCLEA_DVP_ADDRESS` | Endereço do `NucleaDvP` implantado *(o cliente preenche)* |
| `BRLN_DECIMALS_FALLBACK` | Opcional. Usado só se `decimals()` do BRLN não responder. |

O endereço do BRLN **não** é configurado: ele vem de `NucleaDvP.brln()`.

**Verificação ao abrir a aba** (exibir um aviso de erro se falhar e bloquear as ações de escrita):

- `chainId` conectado = `chainId` da rede do cliente.
- `NucleaDvP.ccb()` == `CCB_TOKEN_ADDRESS`.
- `CCBToken.dvp()` == `NUCLEA_DVP_ADDRESS`.

---

## 3. ABIs (fragmentos legíveis)

Use as ABIs JSON do repositório se o app já consome JSON. Caso contrário, estes fragmentos bastam (ethers aceita direto; para viem, use `parseAbi`).

```ts
export const CCB_TOKEN_ABI = [
  "function owner() view returns (address)",
  "function dvp() view returns (address)",
  "function nextTokenId() view returns (uint256)",
  "function ownerOf(uint256 tokenId) view returns (address)",
  "function ccbData(uint256 tokenId) view returns (string ipoc, uint256 faceValue, uint64 maturity)",
  "function locked(uint256 tokenId) view returns (bool)",
  "function referenceUsed(bytes32 refHash) view returns (bool)",
  "function mintCCB(address holder, string ipoc, uint256 faceValue, uint64 maturity) returns (uint256 tokenId)",
  "event CCBIssued(uint256 indexed tokenId, address indexed holder, bytes32 indexed referenceHash)",
  "event CCBLockChanged(uint256 indexed tokenId, bool locked)",
  "event Transfer(address indexed from, address indexed to, uint256 indexed tokenId)",
  "error ERC721NonexistentToken(uint256 tokenId)",
  "error ERC721InvalidReceiver(address receiver)",
  "error OwnableUnauthorizedAccount(address account)",
] as const;

export const NUCLEA_DVP_ABI = [
  "function owner() view returns (address)",
  "function ccb() view returns (address)",
  "function brln() view returns (address)",
  "function isNucleaOperator(address account) view returns (bool)",
  "function isBee4Operator(address account) view returns (bool)",
  "function MAX_CCB_PER_BASKET() view returns (uint256)",
  "function basketCount() view returns (uint256)",
  "function tradeCount() view returns (uint256)",
  "function getBasket(uint256 basketId) view returns (tuple(address holder, uint8 state, uint64 minMaturity, uint256 totalFaceValue, uint256 tradeId))",
  "function basketTokenIds(uint256 basketId) view returns (uint256[])",
  "function basketCCBs(uint256 basketId) view returns (uint256[] ids, string[] ipocs, uint256[] faceValues, uint64[] maturities)",
  "function getTrade(uint256 tradeId) view returns (tuple(uint256 basketId, address holder, address buyer, uint256 price, uint64 expiresAt, uint8 status))",
  "function canExecute(uint256 tradeId) view returns (bool ok, string reason)",
  "function createBasket(address holder, uint256[] ids) returns (uint256 basketId)",
  "function addCCBs(uint256 basketId, uint256[] ids)",
  "function dissolveBasket(uint256 basketId)",
  // fase futura (DvP)
  "function registerTrade(uint256 basketId, address buyer, uint256 price, uint64 expiresAt) returns (uint256 tradeId)",
  "function executeDvP(uint256 tradeId)",
  "function cancelTrade(uint256 tradeId)",
  // eventos
  "event BasketCreated(uint256 indexed basketId, address indexed holder, uint256[] tokenIds, uint256 totalFaceValue, uint64 minMaturity)",
  "event BasketUpdated(uint256 indexed basketId, address indexed holder, uint256[] tokenIds, uint256 totalFaceValue, uint64 minMaturity)",
  "event BasketDissolved(uint256 indexed basketId)",
  "event TradeRegistered(uint256 indexed tradeId, uint256 indexed basketId, address indexed holder, address buyer, uint256 price, uint64 expiresAt, uint256[] tokenIds, uint256 totalFaceValue)",
  "event DvPExecuted(uint256 indexed tradeId, uint256 indexed basketId, address indexed buyer, address holder, uint256 price)",
  "event TradeCancelled(uint256 indexed tradeId, address indexed by)",
  "error OwnableUnauthorizedAccount(address account)",
  "error SafeERC20FailedOperation(address token)",
] as const;

export const ERC20_METADATA_ABI = [
  "function decimals() view returns (uint8)",
  "function symbol() view returns (string)",
  "function balanceOf(address) view returns (uint256)",
  "function allowance(address owner, address spender) view returns (uint256)",
] as const;
```

**Enums** (são retornados como `uint8`):

```ts
export const BasketState = { 0: "None", 1: "Aberta", 2: "Em negociação", 3: "Liquidada", 4: "Desfeita" } as const;
export const TradeStatus = { 0: "None", 1: "Registrada", 2: "Executada", 3: "Cancelada" } as const;
```

---

## 4. Requisitos (EARS)

### R1: Aba e permissões
- **R1.1** O sistema DEVE exibir uma nova aba **"CCBs e Cestas"** no menu, seguindo o padrão das abas existentes.
- **R1.2** QUANDO a aba abrir, o sistema DEVE executar as verificações da seção 2 e, SE alguma falhar, exibir um aviso e desabilitar todas as ações de escrita.
- **R1.3** O sistema DEVE mostrar o papel da conta conectada:
  - **Admin CCB**, se `account == CCBToken.owner()`;
  - **Operador Núclea**, se `NucleaDvP.isNucleaOperator(account)`;
  - **Operador BEE4**, se `NucleaDvP.isBee4Operator(account)`.
- **R1.4** O botão **Emitir CCB** só DEVE estar habilitado para **Admin CCB**. O botão **Criar cesta** só DEVE estar habilitado para **Operador Núclea**. Para os demais, o sistema DEVE mostrar o motivo (tooltip ou texto).
- **R1.5** QUANDO a conta ou a rede da carteira mudar, o sistema DEVE recarregar papéis e dados.

### R2: Listar CCBs emitidas
- **R2.1** O sistema DEVE listar **todas** as CCBs emitidas no `CCBToken`. As colunas são `tokenId`, IPOC, holder atual (`ownerOf`), valor nominal formatado em BRLN, vencimento (data local) e status.
- **R2.2** O status DEVE ser derivado assim:
  - **Livre:** `locked == false`;
  - **Em cesta #N:** `locked == true` e a cesta N está *Aberta*;
  - **Em negociação (cesta #N):** `locked == true` e a cesta N está *Em negociação*;
  - badge adicional **Vencida**, se `maturity <= agora`.
- **R2.3** O sistema DEVE permitir filtrar por holder, status e "somente vencidas", além de buscar por IPOC ou `tokenId`.
- **R2.4** O sistema DEVE paginar no cliente, com 25 itens por página por padrão, e carregar em lotes para não travar a UI (seção 5.2).

### R3: Emitir CCB (mint)
- **R3.1** O formulário DEVE ter os campos **Holder** (endereço), **IPOC** (texto), **Valor nominal** (decimal em BRLN, ex.: `1500,00`) e **Vencimento** (data e hora).
- **R3.2** Validações antes de enviar:
  - **Holder:** endereço válido e diferente de `0x000…0`.
  - **IPOC:** sem espaços nas pontas (aplicar `trim`) e não vazio. Consultar `referenceUsed(keccak256(utf8(ipoc)))`; se `true`, bloquear com "IPOC já utilizado".
  - **Valor nominal:** maior que 0, convertido para unidades mínimas com `parseUnits(valor, decimalsBRLN)`.
  - **Vencimento:** convertido para **timestamp Unix em segundos** (UTC) e maior que o horário atual.
- **R3.3** QUANDO o usuário confirmar, o sistema DEVE mostrar um resumo (holder, IPOC, valor, vencimento em data local e o timestamp em segundos) e então chamar `mintCCB(holder, ipoc, faceValueUnits, maturitySeconds)`.
- **R3.4** QUANDO o recibo for confirmado, o sistema DEVE ler o `tokenId` do evento `CCBIssued` no recibo, exibir "CCB #tokenId emitida" e atualizar a lista.
- **R3.5** SE a transação reverter, o sistema DEVE exibir a mensagem traduzida (seção 6).

### R4: Listar cestas de lastro
- **R4.1** O sistema DEVE listar todas as cestas (`1..basketCount`). As colunas são `basketId`, holder, estado, quantidade de CCBs, valor nominal total, menor vencimento e `tradeId` (se houver).
- **R4.2** QUANDO o usuário expandir uma cesta, o sistema DEVE exibir as CCBs dela via `basketCCBs(basketId)`: ID, IPOC, valor e vencimento.
- **R4.3** SE a cesta tiver `tradeId != 0`, o sistema DEVE exibir os dados da operação via `getTrade(tradeId)`: comprador, preço, prazo e status. Esse bloco já prepara a fase de DvP.
- **R4.4** O sistema DEVE destacar cestas com `minMaturity <= agora` como **lastro vencido**.

### R5: Criar cesta de lastro
- **R5.1** O formulário DEVE ter **Holder** (endereço, ou seleção a partir dos holders presentes na lista de CCBs) e **CCBs** (seleção múltipla).
- **R5.2** A seleção de CCBs DEVE mostrar **apenas** CCBs elegíveis: `ownerOf == holder`, `locked == false` e `maturity > agora`. O limite é `MAX_CCB_PER_BASKET` (25).
- **R5.3** O sistema DEVE mostrar uma prévia com a quantidade, a soma dos valores nominais e o menor vencimento antes de enviar.
- **R5.4** QUANDO o usuário confirmar, o sistema DEVE chamar `createBasket(holder, ids)`, com `ids` em `uint256[]`, ler o `basketId` do evento `BasketCreated` no recibo, exibir "Cesta #basketId criada" e atualizar **as duas listas** (as CCBs passam a "Em cesta").
- **R5.5** *(Desejável)* Numa cesta *Aberta*, o Operador Núclea DEVE poder:
  - **Incluir CCBs:** `addCCBs`, com as mesmas regras de elegibilidade e o limite total de 25.
  - **Desfazer a cesta:** `dissolveBasket`, com confirmação. As CCBs voltam a "Livre".

### R6: Atualização e consistência
- **R6.1** A **fonte da verdade é o estado dos contratos** (chamadas `view`). Os eventos servem para disparar a atualização, não para reconstruir o estado.
- **R6.2** O sistema DEVE atualizar as listas depois de cada transação própria e, se o app já tiver esse mecanismo, por polling dos eventos da seção 3 (intervalo sugerido de 10 a 15 s, só com a aba visível).
- **R6.3** O sistema NÃO DEVE armazenar chaves privadas nem assinar transações em backend. Toda escrita é assinada pela carteira conectada.

---

## 5. Design

### 5.1 Estrutura sugerida (adaptar à convenção do app)

```
<pasta-de-features>/ccb-cestas/
  config.ts          // endereços, ABIs (seção 3), enums
  ccbService.ts      // leitura e escrita do CCBToken
  dvpService.ts      // leitura e escrita do NucleaDvP (já com funções futuras)
  units.ts           // parse/format BRLN e datas
  errors.ts          // tradução de erros (seção 6)
  CcbCestasTab.*     // aba com sub-seções: "CCBs" | "Cestas" | (futuro) "Operações DvP"
  CcbList.*, MintCcbForm.*, BasketList.*, CreateBasketForm.*
```

A aba já nasce com **sub-abas** ("CCBs", "Cestas" e, futuramente, "Operações DvP") para que o DvP entre sem reestruturação.

### 5.2 Leitura das CCBs

O `CCBToken` **não é enumerável** (sem `totalSupply`/`tokenByIndex`), mas os IDs são sequenciais a partir de 1:

```ts
const next = await ccb.nextTokenId();                // próximo ID a ser emitido
const ids = Array.from({ length: Number(next) - 1 }, (_, i) => BigInt(i + 1));

// carregar em lotes (ex.: 20 por vez) para não sobrecarregar o RPC
async function loadCcb(id: bigint) {
  const [owner, data, isLocked] = await Promise.all([
    ccb.ownerOf(id), ccb.ccbData(id), ccb.locked(id),
  ]);
  return { id, owner, ipoc: data.ipoc, faceValue: data.faceValue, maturity: data.maturity, locked: isLocked };
}
```

Se o app já usa **Multicall3** e ele está implantado na rede do cliente, use-o. Caso contrário, use lotes com `Promise.all`.

**De qual cesta é cada CCB:** o contrato não tem um mapeamento CCB → cesta. Monte esse mapeamento a partir das cestas (5.3): para cada cesta *Aberta* ou *Em negociação*, `basketTokenIds(id)` → `map[tokenId] = basketId`.

### 5.3 Leitura das cestas

```ts
const count = await dvp.basketCount();
for (let id = 1n; id <= count; id++) {
  const b = await dvp.getBasket(id);   // { holder, state, minMaturity, totalFaceValue, tradeId }
  const tokenIds = await dvp.basketTokenIds(id);
  // detalhe sob demanda (ao expandir): dvp.basketCCBs(id) -> { ids, ipocs, faceValues, maturities }
  // se b.tradeId != 0n: dvp.getTrade(b.tradeId)
}
```

Cestas *Liquidadas* ou *Desfeitas* continuam na lista (histórico), mas suas CCBs não aparecem mais como "em cesta".

### 5.4 Unidades e datas (`units.ts`)

```ts
const brlnAddr = await dvp.brln();
const decimals = await erc20(brlnAddr).decimals().catch(() => BRLN_DECIMALS_FALLBACK);

parseUnits("1500.00", decimals);  // entrada do usuário -> faceValue (aceitar vírgula e converter para ponto)
formatUnits(faceValue, decimals); // exibição "1.500,00 BRLN" (pt-BR)

// vencimento: data/hora escolhida no fuso do usuário -> segundos UTC
const maturity = BigInt(Math.floor(new Date(inputDateTimeLocal).getTime() / 1000));
// exibição: new Date(Number(maturity) * 1000).toLocaleString("pt-BR")
```

**Atenção:** o contrato usa **segundos**. Nunca envie milissegundos.

### 5.5 Escrita

```ts
// mint
const ipoc = form.ipoc.trim();
if (await ccb.referenceUsed(keccak256(toUtf8Bytes(ipoc)))) throw new Error("IPOC já utilizado");
const tx = await ccbWithSigner.mintCCB(holder, ipoc, parseUnits(valor, decimals), maturity);
const rc = await tx.wait();
const tokenId = parseEvent(rc, ccbInterface, "CCBIssued").args.tokenId;

// criar cesta
const tx2 = await dvpWithSigner.createBasket(holder, selectedIds /* bigint[] */);
const rc2 = await tx2.wait();
const basketId = parseEvent(rc2, dvpInterface, "BasketCreated").args.basketId;
```

`parseEvent`: filtrar `rc.logs` pelo endereço do contrato e decodificar com a `Interface`/`decodeEventLog` da biblioteca do app.

**Pré-checagem:** antes de enviar, fazer `staticCall` / `simulateContract` da mesma chamada. Se ela reverter, mostrar o erro traduzido sem abrir a carteira.

---

## 6. Tradução de erros (`errors.ts`)

Extrair a *revert reason* (string) ou o *custom error* (decodificar com a ABI) e mapear:

| Erro do contrato | Mensagem na UI |
| --- | --- |
| `OwnableUnauthorizedAccount` | Esta carteira não é a administradora do contrato. |
| `Nuclea only` | Esta carteira não é operadora Núclea. |
| `BEE4 only` | Esta carteira não é operadora BEE4. |
| `Invalid CCB` | IPOC vazio ou valor nominal igual a zero. |
| `Invalid maturity` / `CCB matured` | Vencimento inválido ou CCB já vencida. |
| `Reference already used` | IPOC já utilizado em outra CCB. |
| `ERC721InvalidReceiver` | Endereço do holder inválido. |
| `Invalid holder` | Holder inválido. |
| `Not holder's CCB` | Uma das CCBs não pertence a este holder. |
| `CCB already in a basket` | Uma das CCBs já está em outra cesta. |
| `Invalid basket size` | A cesta deve ter de 1 a 25 CCBs. |
| `Basket not open` | A cesta não está aberta (em negociação, liquidada ou desfeita). |
| `ERC721NonexistentToken` | CCB inexistente. |
| *(futuro)* `Invalid buyer`, `Invalid terms`, `Trade not registered`, `Trade expired`, `Incorrect BRLN payment`, `SafeERC20FailedOperation` | Ver a seção 8. |
| qualquer outro | Exibir o texto original e o hash da transação. |

---

## 7. Tarefas

- [ ] **T1. Descoberta** (seção 0): documentar framework, biblioteca web3, carteira, configuração, ABIs e helpers de transação.
- [ ] **T2. Configuração:** adicionar `CCB_TOKEN_ADDRESS`, `NUCLEA_DVP_ADDRESS` e as ABIs da seção 3 no padrão do app.
- [ ] **T3. Serviços:** `ccbService`, `dvpService`, `units` e `errors`, incluindo as verificações da seção 2.
- [ ] **T4. Aba e papéis:** rota/menu, sub-abas, banner de papel da conta e verificação de rede (R1).
- [ ] **T5. Lista de CCBs:** carregamento em lotes, status derivado, filtros, busca e paginação (R2).
- [ ] **T6. Emissão de CCB:** formulário, validações, consulta de IPOC já usado, simulação, envio, recibo e atualização (R3).
- [ ] **T7. Lista de cestas:** linhas expansíveis com `basketCCBs` e bloco da operação, se houver (R4).
- [ ] **T8. Criação de cesta:** seleção de CCBs elegíveis, prévia, envio, recibo e atualização das duas listas (R5.1 a R5.4).
- [ ] **T9. (Desejável)** Incluir CCBs e desfazer cesta (R5.5).
- [ ] **T10. Atualização automática:** polling de eventos com a aba visível (R6).
- [ ] **T11. Testes:** unitários de `units`/`errors`; teste de integração contra um nó local ou a rede de homologação seguindo o roteiro da seção 9.

---

## 8. Fase futura: DvP pela UI (não implementar agora, mas deixar preparado)

Criar a sub-aba **"Operações DvP"** desabilitada ("em breve"). Os serviços já expõem as funções abaixo:

| Ação | Quem | Chamada | Observação |
| --- | --- | --- | --- |
| Listar operações | Todos | `tradeCount`, `getTrade(1..n)` | Colunas: cesta, holder, comprador, preço, prazo e status |
| Registrar comprador | **Operador BEE4** | `registerTrade(basketId, buyer, price, expiresAt)` | Cesta *Aberta*; `buyer` diferente do holder; `price > 0`; `expiresAt` futuro (segundos) |
| Pré-checar | Operador Núclea | `canExecute(tradeId)` → `(ok, motivo)` | Mostrar o motivo: saldo ou allowance do comprador, prazo, vencimento |
| Executar DvP | **Operador Núclea** | `executeDvP(tradeId)` | Uma transação: CCBs holder → comprador e BRLN comprador → holder |
| Cancelar | Núclea, BEE4 ou qualquer conta após o prazo | `cancelTrade(tradeId)` | A cesta volta a *Aberta* |

Para executar o DvP, a carteira do comprador precisa ter `BRLN.approve(NUCLEA_DVP_ADDRESS, valor)`. Essa assinatura é **do comprador** e fica fora desta UI, a menos que a carteira dele esteja conectada. A UI deve exibir o saldo e a allowance do comprador (`balanceOf`, `allowance`) no detalhe da operação.

---

## 9. Critérios de aceite (roteiro manual)

1. Com a carteira **admin**, emitir `IPOC-TESTE-001`: valor `1000,00`, vencimento em 1 ano, holder = carteira H. A CCB #N aparece como **Livre**, com holder H.
2. Tentar emitir de novo `IPOC-TESTE-001` → a UI bloqueia antes de enviar com "IPOC já utilizado".
3. Tentar emitir com uma carteira que **não** é admin → botão desabilitado com o motivo exibido.
4. Emitir mais uma CCB para H. Com a carteira **operadora Núclea**, criar uma cesta com as duas → a cesta #M aparece *Aberta*, com total `2000,00` e o menor vencimento correto. As duas CCBs passam a "Em cesta #M".
5. No formulário de nova cesta, as duas CCBs **não** aparecem mais como elegíveis.
6. Expandir a cesta #M → mostra IPOCs, valores e vencimentos corretos.
7. Trocar de conta na carteira → os papéis e os botões se atualizam sem recarregar a página.
8. No MetaMask da carteira H, importar o NFT (endereço do CCBToken + tokenId) → a CCB aparece na carteira mesmo estando em cesta.
