# Besu Explorer

Explorer leve para redes **Hyperledger Besu (QBFT)**, distribuído como **um único executável** para Linux. Ele não precisa de Docker, banco de dados, Java, Node.js nem acesso à internet.

- **Banco em memória:** o explorer lê a rede pelo JSON-RPC do nó e guarda tudo em RAM. Ao reiniciar, reindexa do zero. Só a configuração (RPC e BRLN) fica gravada num pequeno arquivo JSON.
- **Páginas:**
  - **Início:** bloco atual, validadores QBFT, últimos blocos e últimas transações.
  - **Blocos:** todos, ou só os que têm transações.
  - **Bloco:** cabeçalho, proponente QBFT e transações.
  - **Transações:** lista geral.
  - **Pendentes:** mempool do nó, mostrando quais transações estão **prontas** (entram no próximo bloco) e quais estão **aguardando nonce** (lacuna de nonce da conta), há quanto tempo cada uma está no pool e se veio pelo RPC deste nó. A página se atualiza sozinha a cada 5 s e permite filtrar por endereço. Exige a API `TXPOOL` no nó.
  - **Transação** (busca por txId): status, entrada decodificada, eventos e transferências de BRLN.
  - **Endereço:** saldo, transações e transferências de BRLN.
  - **BRLN:** transferências (emissão, transferência e queima), filtro por carteira, maiores saldos e conferência do supply com os eventos.
  - **Configuração:** endereço RPC do nó e endereço do contrato BRLN, com valores padrão.
- **Busca:** por txId, hash de bloco, número de bloco ou endereço.
- Interface em português, com tema claro e escuro, funcionando também no celular.

## Requisitos

| Item | Valor |
| --- | --- |
| Sistema | RHEL 9 (ou qualquer Linux x86_64 com kernel 3.2+). Há também um binário `arm64`. |
| Nó Besu | JSON-RPC HTTP com as APIs `ETH` e `NET`. Opcionais: `QBFT` (lista os validadores) e `TXPOOL` (página Pendentes). Exemplo: `--rpc-http-api=ETH,NET,QBFT,TXPOOL` |
| Memória | ~150 MB para ~275 mil blocos (medido). Cresce ~0,5 KB por bloco. |
| Porta | 8080/TCP (padrão, configurável). |

O explorer só **lê** da rede: não assina nem envia transações e não precisa de chaves.

## Instalação rápida (sem root)

```bash
tar xzf besu-explorer-1.1.0-linux-amd64.tar.gz
cd besu-explorer-1.1.0-linux-amd64
./besu-explorer
```

Abra `http://<ip-da-maquina>:8080`. Na primeira execução, o arquivo `besu-explorer.json` é criado com os valores padrão:

```json
{
  "rpc": "http://127.0.0.1:8545",
  "brln": ""
}
```

Vá em **Configuração**, informe o endereço do contrato BRLN e clique em **Salvar e reindexar**. Para rodar em segundo plano sem root: `nohup ./besu-explorer > explorer.log 2>&1 &`.

## Instalação como serviço (com root, systemd)

```bash
sudo useradd --system --no-create-home --shell /sbin/nologin besu-explorer
sudo install -m 0755 besu-explorer /usr/local/bin/besu-explorer
sudo install -d -o besu-explorer -g besu-explorer -m 0750 /etc/besu-explorer
sudo install -m 0644 besu-explorer.service /etc/systemd/system/besu-explorer.service
sudo systemctl daemon-reload
sudo systemctl enable --now besu-explorer
sudo firewall-cmd --permanent --add-port=8080/tcp && sudo firewall-cmd --reload   # se o firewalld estiver ativo
journalctl -u besu-explorer -f                                                     # logs
```

O binário fica em `/usr/local/bin`, onde o SELinux do RHEL já permite a execução por serviços. A configuração fica em `/etc/besu-explorer/besu-explorer.json` e é criada no primeiro início.

## Tela de configuração

| Campo | Padrão | Observação |
| --- | --- | --- |
| Endereço RPC do nó | `http://127.0.0.1:8545` | O nó na mesma máquina |
| Endereço do contrato BRLN | vazio | Vazio desativa a página BRLN |

Ao salvar, o explorer **confere** se o nó responde e se existe contrato no endereço do BRLN. Se algo falhar, mostra o erro e **não altera** a configuração. Se tudo der certo, grava o arquivo, descarta o banco em memória e reindexa. **Restaurar padrões** volta aos valores da tabela.

- **Senha:** use `--admin-password` (ou `EXPLORER_ADMIN_PASSWORD`) para exigir senha na tela `/config` (autenticação básica do navegador). As demais páginas continuam abertas. Sem senha, qualquer pessoa que alcance a porta pode alterar a configuração.
- **Prioridade:** padrões < arquivo < opções `--rpc` / `--brln`. Se o explorer for iniciado com essas opções, elas prevalecem sobre a tela a cada reinício, e a tela mostra um aviso.

## Opções

| Opção | Variável | Padrão | Descrição |
| --- | --- | --- | --- |
| `--config` | `EXPLORER_CONFIG` | `besu-explorer.json` | Arquivo de configuração |
| `--listen` | `EXPLORER_LISTEN` | `:8080` | Endereço e porta da interface (`127.0.0.1:8080` para acesso só local) |
| `--rpc` | `EXPLORER_RPC` | — | Sobrepõe o RPC do arquivo |
| `--brln` | `EXPLORER_BRLN` | — | Sobrepõe o BRLN do arquivo |
| `--admin-password` | `EXPLORER_ADMIN_PASSWORD` | — | Senha da tela `/config` |
| `--abi-dir` | `EXPLORER_ABI_DIR` | — | Pasta com ABIs `.json` extras (ABI pura ou artefato Foundry/Hardhat) para decodificar funções e eventos de outros contratos |
| `--start-block` | — | `0` | Primeiro bloco a indexar |
| `--batch` | — | `100` | Blocos por requisição em lote |
| `--workers` | — | `4` | Lotes em paralelo na sincronização inicial |
| `--poll` | — | `2s` | Intervalo de consulta de novos blocos |
| `--brln-decimals` | — | `18` | Casas decimais se `decimals()` do contrato não responder |
| `--tz` | — | `America/Sao_Paulo` | Fuso horário das datas |
| `--version` | — | — | Mostra a versão |

Monitoramento: `GET /api/status` retorna JSON com o bloco atual, o progresso, os totais e o último erro.

## Como funciona

1. **Sincronização inicial:** busca os blocos em lotes (`eth_getBlockByNumber` em batch) e os recibos dos blocos com transações (`eth_getBlockReceipts`, ou `eth_getTransactionReceipt` se o nó não tiver o primeiro).
2. **Acompanhamento:** depois, consulta novos blocos a cada `--poll`. O QBFT tem finalidade imediata, então não há reorganização para tratar.
3. **Blocos vazios** guardam só um resumo de ~100 bytes. Transações guardam dados completos e recibo.
4. **Pendentes:** a página consulta o nó na hora (`txpool_besuPendingTransactions`, `txpool_besuTransactions` e `txpool_besuStatistics`); o mempool não é guardado em memória. "Pronta" e "aguardando nonce" são calculados comparando o nonce de cada transação com o nonce já confirmado da conta (`eth_getTransactionCount ... latest`). O mempool é **do nó consultado**; cada um dos outros nós tem o seu.
5. **BRLN:** usa o evento padrão `Transfer(address,address,uint256)` do ERC-20. Funciona com qualquer versão do contrato que emita esse evento. Os saldos da lista são calculados pelos eventos; na página do endereço, aparece o `balanceOf` real do contrato.

## Limitações

- **Transações internas** (chamadas entre contratos) não são mostradas, porque exigiriam as APIs `DEBUG`/`TRACE` no nó.
- **Motivo do revert** só aparece se o nó for iniciado com `--revert-reason-enabled`.
- **Sem verificação de código-fonte de contratos.** A decodificação usa as assinaturas ERC-20 embutidas e as ABIs de `--abi-dir`.
- **Reinício:** tudo é reindexado.
- **Pendentes:** mostra até 1.000 transações do mempool. Sem a API `TXPOOL`, a página só explica como habilitá-la.

## Compilar a partir do código-fonte

O projeto não tem dependências externas (só a biblioteca padrão do Go), então compila **offline**:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o besu-explorer .
go test ./...
```

Go 1.24+.
