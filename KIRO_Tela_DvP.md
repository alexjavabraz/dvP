# Spec: feed do `TradeRegistered` e execução do DvP

> **Para o Kiro.** Continuação da spec **"CCBs e Cestas de Lastro"** (`.kiro/specs/ccb-cestas/`), que já está implementada.
> Reaproveite a configuração, as ABIs, `ccbService`, `dvpService`, `units` e `errors` criados nela. **Não duplique.**
> Pode ser salva em `.kiro/specs/dvp-execucao/`.

---

## 0. Descoberta obrigatória antes de codar

1. **Feed de eventos existente:** onde fica o componente, como ele recebe itens (store, contexto, websocket, polling), qual é o formato de um item (tipo, título, timestamp, link) e como trata cliques. **Os eventos do DvP entram nesse feed.** Não crie um feed paralelo.
2. **Mecanismo de escuta on-chain do app:** o app já usa `eth_getLogs` com polling, `eth_subscribe` via WebSocket ou `watchContractEvent` (viem/wagmi)? Use o mesmo.
3. **Onde o cursor/estado do feed é persistido** (memória, localStorage, backend), para não reprocessar nem perder eventos ao recarregar a página.
4. **Sub-aba "Operações DvP"** criada como placeholder na spec anterior: ela passa a ser habilitada aqui.

---

## 1. Contexto

A BEE4 já fez a parte dela: chamou `NucleaDvP.registerTrade(basketId, buyer, price, expiresAt)` com a conta de **operador BEE4**. Isso:

- reserva a cesta (estado *Em negociação*);
- cria a operação `tradeId` com status *Registrada*;
- emite o evento **`TradeRegistered`**.

Falta a **Núclea executar o DvP**. Uma carteira com papel de **operador Núclea** chama `executeDvP(tradeId)`, que em **uma única transação**:

- move as CCBs da cesta da carteira do **holder** (vendedor) para a do **comprador**;
- move `price` em BRLN da carteira do **comprador** para a do **holder**.

Se qualquer perna falhar, tudo reverte e nada se move.

**Fluxo desta tela:** `TradeRegistered` aparece no feed → o usuário clica no `tradeId` → o painel da operação faz as pré-checagens → o operador Núclea clica em **"Executar DvP"** → `DvPExecuted` → o feed e o painel são atualizados.

---

## 2. Eventos monitorados (contrato `NUCLEA_DVP_ADDRESS`)

| Evento | topic0 | Campos indexados (topics 1..3) | Campos em `data` |
| --- | --- | --- | --- |
| `TradeRegistered(uint256 tradeId, uint256 basketId, address holder, address buyer, uint256 price, uint64 expiresAt, uint256[] tokenIds, uint256 totalFaceValue)` | `0xffb4aed9bfc74183a77bc0ac033f09ea872bc676fd8658b4330a55f828846ed8` | `tradeId`, `basketId`, `holder` | `buyer`, `price`, `expiresAt`, `tokenIds`, `totalFaceValue` |
| `DvPExecuted(uint256 tradeId, uint256 basketId, address buyer, address holder, uint256 price)` | `0x0ff4d9fbb576c6d04df4809ffbccc3847b0201ed4b8297797e222ec2eae5429f` | `tradeId`, `basketId`, `buyer` | `holder`, `price` |
| `TradeCancelled(uint256 tradeId, address by)` | `0x68bbfaa8a985659b13e3f23ffac8fc9039d6ca60199cd9327817cb0cc03eb876` | `tradeId`, `by` | — |

As assinaturas já estão em `NUCLEA_DVP_ABI` (spec anterior, seção 3). Os topic0 servem para filtrar no `eth_getLogs`. Decodifique sempre com a ABI, nunca à mão.

> Validado num nó local (EVM Paris) com os contratos do repositório: `registerTrade` → `eth_getLogs` com este topic0 retorna 1 log, com `tradeId`/`basketId`/`holder` nos topics e os campos de `data` decodificados corretamente; `executeDvP` → `DvPExecuted`.

**Configuração nova:**

| Chave | Valor |
| --- | --- |
| `NUCLEA_DVP_DEPLOY_BLOCK` | Bloco do deploy do `NucleaDvP` *(o cliente preenche)*. É o ponto de partida do histórico do feed. |
| `DVP_POLL_INTERVAL_MS` | Opcional. Padrão `5000` (só se o app usar polling). |
| `LOGS_BLOCK_RANGE` | Opcional. Padrão `5000` blocos por chamada de `eth_getLogs` ao carregar o histórico. |

---

## 3. Requisitos (EARS)

### R1: Feed de eventos
- **R1.1** QUANDO um `TradeRegistered` for emitido pelo `NUCLEA_DVP_ADDRESS`, o sistema DEVE adicionar um item ao **feed de eventos existente** com:
  - título "Operação #`tradeId` registrada pela BEE4";
  - cesta `#basketId`, holder, comprador, preço formatado em BRLN e prazo (`expiresAt`) em data local;
  - quantidade de CCBs e valor nominal total;
  - bloco, horário do bloco e hash da transação.
- **R1.2** O `tradeId` do item DEVE ser **clicável** e abrir o painel da operação (R2).
- **R1.3** QUANDO um `DvPExecuted` ou um `TradeCancelled` for emitido, o sistema DEVE adicionar o item correspondente ao feed ("DvP #`tradeId` executado", "Operação #`tradeId` cancelada por `by`") e **atualizar o status** exibido no item `TradeRegistered` de mesmo `tradeId`.
- **R1.4** O item `TradeRegistered` DEVE mostrar um badge de status lido do contrato (`getTrade(tradeId).status`) e do relógio:

  | Condição | Badge | Cor |
  | --- | --- | --- |
  | `status == 1` e `agora < expiresAt` | **Aguardando DvP** | amarelo |
  | `status == 1` e `agora >= expiresAt` | **Expirada** | cinza |
  | `status == 2` | **DvP executado** | verde |
  | `status == 3` | **Cancelada** | cinza |

- **R1.5** QUANDO a aba carregar, o sistema DEVE buscar o histórico de `NUCLEA_DVP_DEPLOY_BLOCK` até o bloco atual, em faixas de `LOGS_BLOCK_RANGE`. Depois, DEVE continuar acompanhando os blocos novos, por polling a partir do último bloco processado ou por subscrição WebSocket, conforme o padrão do app.
- **R1.6** O sistema DEVE deduplicar por `transactionHash + logIndex` e ordenar do mais recente para o mais antigo.
- **R1.7** O feed DEVE permitir filtrar só os itens "Aguardando DvP".
- **R1.8** O sistema DEVE exibir um contador "N operações aguardando DvP", calculado pelo estado do contrato (R5.1) e não pela contagem de eventos.

### R2: Painel da operação (ao clicar no `tradeId`)
- **R2.1** O painel DEVE abrir como modal, drawer ou rota (`/dvp/:tradeId`), seguindo o padrão do app, e carregar **do contrato**, nunca só do evento:
  - `getTrade(tradeId)` → `basketId`, `holder`, `buyer`, `price`, `expiresAt`, `status`;
  - `getBasket(basketId)` e `basketCCBs(basketId)` → lista de CCBs (ID, IPOC, valor nominal, vencimento), total e menor vencimento;
  - BRLN do comprador: `balanceOf(buyer)` e `allowance(buyer, NUCLEA_DVP_ADDRESS)`;
  - BRLN do holder: `balanceOf(holder)`, para comparar antes e depois;
  - `canExecute(tradeId)` → `(ok, reason)`.
- **R2.2** O painel DEVE mostrar uma **checklist de pré-condições**, com ✔/✖ em cada item:

  | Item | Regra | Mensagem se falhar |
  | --- | --- | --- |
  | Operação registrada | `status == 1` | "Esta operação já foi executada ou cancelada." |
  | Dentro do prazo | `agora < expiresAt` | "Prazo da operação expirado em {data}." |
  | Lastro válido | `minMaturity > agora` | "Uma ou mais CCBs da cesta venceram." |
  | Saldo do comprador | `balanceOf(buyer) >= price` | "Saldo BRLN do comprador insuficiente: {saldo} de {preço}." |
  | Autorização do comprador | `allowance(buyer, NucleaDvP) >= price` | "O comprador ainda não autorizou o NucleaDvP a debitar o BRLN (`approve`). Autorizado: {allowance} de {preço}." |
  | Carteira conectada | `isNucleaOperator(account)` | "Esta carteira não é operadora Núclea." |

  `canExecute` é a referência final. SE o `ok` dele divergir da checklist, prevalece o `canExecute`, e o sistema DEVE mostrar o `reason` dele.

### R3: Executar DvP
- **R3.1** O botão **"Executar DvP"** só DEVE ficar habilitado QUANDO todas as pré-condições de R2.2 estiverem ✔.
- **R3.2** QUANDO o usuário clicar, o sistema DEVE mostrar uma confirmação com o resumo:
  - "Enviar {n} CCBs de {holder} para {buyer}";
  - "Debitar {preço} BRLN de {buyer} e creditar em {holder}";
  - "Operação atômica: ou as duas pernas acontecem, ou nenhuma."
- **R3.3** Ao confirmar, o sistema DEVE simular a chamada (`staticCall` / `simulateContract`) e, só se a simulação passar, enviar `executeDvP(tradeId)` assinado pela carteira conectada.
- **R3.4** QUANDO o recibo for confirmado, o sistema DEVE:
  - ler o `DvPExecuted` do recibo;
  - exibir "DvP #`tradeId` executado";
  - mostrar no painel o **antes e depois**: dono das CCBs (`ownerOf` de cada ID agora = comprador) e saldo BRLN do holder (`+price`);
  - atualizar o feed, o badge, a lista de CCBs e a lista de cestas (a cesta passa a *Liquidada*).
- **R3.5** SE a transação reverter, o sistema DEVE mostrar a mensagem traduzida (seção 4) e manter o painel aberto com as checagens atualizadas.
- **R3.6** Durante o envio, o botão DEVE ficar desabilitado com indicador de progresso, para evitar duplo clique. SE outro operador executar antes, o envio reverte com `Trade not registered`, e o sistema DEVE tratar isso como "Esta operação já foi executada" e recarregar o painel.

### R4: Cancelar (secundário)
- **R4.1** O painel DEVE oferecer **"Cancelar operação"** (`cancelTrade(tradeId)`) para operador Núclea ou operador BEE4 enquanto `status == 1`. Depois de `expiresAt`, qualquer conta pode cancelar.
- **R4.2** O cancelamento DEVE pedir confirmação ("A cesta volta a ficar aberta e as CCBs continuam bloqueadas com o holder") e, depois do recibo, atualizar o feed, o painel e as listas.

### R5: Consistência
- **R5.1** O **estado do contrato é a fonte da verdade**. Eventos só disparam atualizações. O contador de R1.8 é calculado percorrendo `tradeCount` e `getTrade(1..n)` e contando `status == 1 && agora < expiresAt`.
- **R5.2** QUANDO a conta ou a rede mudar, o sistema DEVE recarregar os papéis e o painel aberto.
- **R5.3** O sistema NÃO DEVE manter chaves privadas nem assinar em backend.

---

## 4. Tradução de erros (acrescentar ao `errors.ts`)

| Erro do contrato | Mensagem |
| --- | --- |
| `Nuclea only` | Esta carteira não é operadora Núclea. |
| `Trade not registered` | Esta operação já foi executada ou cancelada. |
| `Trade expired` | O prazo desta operação expirou. Cancele-a para liberar a cesta. |
| `CCB matured` | Uma ou mais CCBs da cesta venceram; o DvP não pode ser executado. |
| `Incorrect BRLN payment` | O BRLN não foi creditado integralmente ao vendedor (token com regra de taxa ou bloqueio). |
| `SafeERC20FailedOperation` / revert do BRLN (`ERC20InsufficientBalance`, `ERC20InsufficientAllowance` ou mensagem própria do BRLN) | Falha no débito do BRLN do comprador: verifique o saldo, a autorização (`approve`) e se a conta não está bloqueada no BRLN. |
| `CCB not locked` / `CCB locked` | Estado inconsistente das CCBs; recarregue a cesta. |
| `Not authorized` (no cancelamento) | Só operadores Núclea/BEE4 podem cancelar antes do prazo. |

---

## 5. Design

### 5.1 Arquivos (adaptar à convenção do app)

```
ccb-cestas/
  dvpEvents.ts        // histórico + acompanhamento + decodificação + dedupe (novo)
  feedAdapter.ts      // converte eventos do DvP no formato de item do feed existente (novo)
  TradePanel.*        // painel da operação: checklist, executar, cancelar (novo)
  dvpService.ts       // (existente) garantir executeDvP, cancelTrade, canExecute, getTrade
  errors.ts           // (existente) acrescentar a seção 4
```

### 5.2 Histórico e acompanhamento (ethers v6, traduzir para a biblioteca do app)

```ts
const TOPICS = {
  TradeRegistered: "0xffb4aed9bfc74183a77bc0ac033f09ea872bc676fd8658b4330a55f828846ed8",
  DvPExecuted:     "0x0ff4d9fbb576c6d04df4809ffbccc3847b0201ed4b8297797e222ec2eae5429f",
  TradeCancelled:  "0x68bbfaa8a985659b13e3f23ffac8fc9039d6ca60199cd9327817cb0cc03eb876",
};
const iface = new Interface(NUCLEA_DVP_ABI);

async function fetchRange(from: number, to: number) {
  const logs = await provider.getLogs({
    address: NUCLEA_DVP_ADDRESS,
    topics: [[TOPICS.TradeRegistered, TOPICS.DvPExecuted, TOPICS.TradeCancelled]], // OR no topic0
    fromBlock: from, toBlock: to,
  });
  return logs.map(l => ({ key: `${l.transactionHash}:${l.index}`, log: l, ev: iface.parseLog(l)! }));
}

// histórico: NUCLEA_DVP_DEPLOY_BLOCK .. head em faixas de LOGS_BLOCK_RANGE
// acompanhamento: a cada DVP_POLL_INTERVAL_MS, fetchRange(lastProcessed + 1, head); lastProcessed = head
```

- **Finalidade:** a rede é **Besu QBFT**, que tem finalidade imediata. Um bloco incluído não é revertido, então não é preciso esperar várias confirmações nem tratar reorganização.
- **Horário do evento:** use o `timestamp` do bloco (`getBlock(blockNumber)`), com cache por número de bloco.
- **Cursor:** persista o `lastProcessed` no mesmo lugar em que o feed do app guarda o estado. Ao recarregar, busque de novo os últimos N blocos (ex.: 20) e deduplique por `key`.

### 5.3 Execução

```ts
await dvp.executeDvP.staticCall(tradeId, { from: account });   // simulação (reverte com o motivo)
const tx = await dvpWithSigner.executeDvP(tradeId);
const rc = await tx.wait();
const executed = rc.logs
  .filter(l => l.address.toLowerCase() === NUCLEA_DVP_ADDRESS.toLowerCase())
  .map(l => iface.parseLog(l))
  .find(e => e?.name === "DvPExecuted");
```

### 5.4 Item do feed (exemplo de mapeamento)

```ts
// TradeRegistered -> item do feed
{
  id: key,
  type: "dvp.tradeRegistered",
  title: `Operação #${tradeId} registrada pela BEE4`,
  subtitle: `Cesta #${basketId} · ${tokenIds.length} CCBs · ${formatBRLN(price)} · prazo ${formatDate(expiresAt)}`,
  status: badgeFrom(getTrade(tradeId), now),   // R1.4
  link: { label: `#${tradeId}`, onClick: () => openTradePanel(tradeId) },
  blockNumber, txHash, timestamp,
}
```

---

## 6. Tarefas

- [ ] **T1. Descoberta** (seção 0): feed existente, mecanismo de escuta, persistência do cursor e sub-aba.
- [ ] **T2. Configuração:** `NUCLEA_DVP_DEPLOY_BLOCK` e os opcionais da seção 2.
- [ ] **T3. `dvpEvents`:** histórico em faixas, acompanhamento, decodificação pela ABI, dedupe e cursor (R1.5, R1.6).
- [ ] **T4. `feedAdapter`:** itens `TradeRegistered`, `DvPExecuted` e `TradeCancelled` no feed existente, com badge de status e `tradeId` clicável (R1.1 a R1.4, R1.7).
- [ ] **T5. Contador** "N operações aguardando DvP" pelo estado do contrato (R1.8, R5.1).
- [ ] **T6. `TradePanel`:** dados da operação, da cesta e do BRLN, mais a checklist de pré-condições com `canExecute` (R2).
- [ ] **T7. Executar DvP:** confirmação, simulação, envio, recibo, antes/depois e atualizações (R3).
- [ ] **T8. Cancelar operação** (R4).
- [ ] **T9. Erros** da seção 4.
- [ ] **T10. Habilitar a sub-aba "Operações DvP"**, com a lista de todas as operações (`tradeCount`, `getTrade`) e o mesmo painel ao clicar.
- [ ] **T11. Testes:** unitários do `feedAdapter` e do cálculo de badge; roteiro manual da seção 7.

---

## 7. Critérios de aceite (roteiro manual)

Pré-requisito: uma cesta *Aberta* (tela anterior), um comprador com saldo BRLN suficiente e `BRLN.approve(NUCLEA_DVP_ADDRESS, preço)` assinado pela carteira dele.

1. **Simular a BEE4:** no Remix, com a conta **operador BEE4**, chamar `registerTrade(basketId, comprador, preço, expiresAt)`. Em até um intervalo de polling, o item "Operação #N registrada pela BEE4" aparece no feed com o badge **Aguardando DvP**, e o contador sobe para 1.
2. **Recarregar a página:** o item continua no feed, sem duplicar.
3. **Clicar em `#N`:** o painel abre com os dados da operação e da cesta, e todos os itens da checklist ficam ✔. Com uma carteira **não** operadora Núclea, o último item fica ✖ e o botão desabilitado.
4. **Com a carteira operadora Núclea, clicar em "Executar DvP" e confirmar:**
   - o feed mostra "DvP #N executado";
   - o badge vira **DvP executado** e o contador volta para 0;
   - no painel, todas as CCBs pertencem ao comprador e o saldo BRLN do holder aumentou exatamente o preço;
   - a cesta aparece como *Liquidada*.
5. **Clicar de novo em "Executar DvP"** (ou abrir o painel numa segunda aba e executar): a mensagem é "Esta operação já foi executada", e nada se move.
6. **Autorização insuficiente:** registrar outra operação com um comprador sem `approve` suficiente. O item "Autorização do comprador" fica ✖ com o valor autorizado, e o botão fica desabilitado.
7. **Cancelamento:** registrar uma operação e clicar em "Cancelar operação" como operador Núclea. O feed mostra "Operação #M cancelada", o badge vira **Cancelada** e a cesta volta a *Aberta*.
8. **Expiração:** registrar uma operação com `expiresAt` próximo e aguardar. O badge vira **Expirada**, o botão de executar fica desabilitado e o cancelamento fica disponível para qualquer conta.
