# Besu Explorer

Explorer leve para redes **Hyperledger Besu (QBFT)**, distribuído como **um único executável** para Linux. Ele não precisa de Docker, banco de dados, Java, Node.js nem acesso à internet.

- **Banco em memória:** o explorer lê a rede pelo JSON-RPC do nó e guarda tudo em RAM. Ao reiniciar, reindexa do zero. Só a configuração (RPC, BRLN, rótulos de endereços e logos) fica gravada num pequeno arquivo JSON.
- **Páginas:**
  - **Início:** bloco atual, validadores QBFT, últimos blocos e últimas transações.
  - **Blocos:** todos, ou só os que têm transações.
  - **Bloco:** cabeçalho, proponente QBFT e transações.
  - **Transações:** lista geral.
  - **Pendentes:** mempool do nó, mostrando quais transações estão **prontas** (entram no próximo bloco) e quais estão **aguardando nonce** (lacuna de nonce da conta), há quanto tempo cada uma está no pool e se veio pelo RPC deste nó. A página se atualiza sozinha a cada 5 s e permite filtrar por endereço. Exige a API `TXPOOL` no nó.
  - **Transação** (busca por txId): status, entrada decodificada, eventos e transferências de BRLN.
  - **Endereço:** saldo, transações e transferências de BRLN.
  - **BRLN:** transferências (emissão, transferência e queima), filtro por carteira, maiores saldos e conferência do supply com os eventos (o `totalSupply` é lido no mesmo bloco indexado; veja [Como funciona](#como-funciona)).
  - **Configuração** (`/explorer-conf/admin`, no menu só para o usuário `admin`): endereço RPC do nó e do contrato BRLN, rótulos de endereços e logo da tela de carregamento.
- **Login** com dois usuários fixos: `admin` (tudo) e `client` (tudo, exceto a configuração). Veja [Acesso](#acesso).
- **Atualização automática** das páginas, **rótulos** nos endereços, **logo da empresa** no cabeçalho e **tela de carregamento animada** com o logo na abertura e entre páginas.
- **Busca:** por txId, hash de bloco, número de bloco ou endereço.
- Interface em português seguindo o **Design System Núclea** (cantos retos, sem sombras, títulos em mono caixa-alta, uma cor-tema por área: lilás no explorer, verde no BRLN, lima na configuração; logo oficial da Núclea embutido em `web/static/brand/`), **tema claro e escuro** com botão no cabeçalho (segue o sistema até o usuário escolher; a escolha fica salva no navegador), funcionando também no celular. As fontes vão embutidas no executável (não precisa de internet).

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
tar xzf besu-explorer-1.3.0-linux-amd64.tar.gz
cd besu-explorer-1.3.0-linux-amd64
./besu-explorer
```

Abra `http://<ip-da-maquina>:8080`. Na primeira execução, o arquivo `besu-explorer.json` é criado com os valores padrão:

```json
{
  "rpc": "http://127.0.0.1:8545",
  "brln": ""
}
```

Entre com o usuário `admin`, abra **Config** no menu (`/explorer-conf/admin`), informe o endereço do contrato BRLN e clique em **Salvar e reindexar**. Na mesma tela, cadastre os rótulos dos endereços e envie o logo. Para rodar em segundo plano sem root: `nohup ./besu-explorer > explorer.log 2>&1 &`.

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

## Acesso

Todas as páginas exigem login (tela `/login`). Há dois usuários fixos:

| Usuário | Senha padrão | Pode ver |
| --- | --- | --- |
| `admin` | `Deltasp+=ks3` | Todas as páginas, inclusive a configuração |
| `client` | `vErt1Ce@2026` | Todas as páginas, exceto a configuração |

- **Trocar as senhas** sem recompilar: `--admin-password` / `EXPLORER_ADMIN_PASSWORD` e `--client-password` / `EXPLORER_CLIENT_PASSWORD`. Vazio mantém a senha padrão. O código guarda só o hash SHA-256 das senhas padrão.
- **Sessão:** cookie `HttpOnly` válido por 12 horas, guardado em memória. Ao reiniciar o explorer, todos precisam entrar de novo. **Sair** no cabeçalho encerra a sessão.
- **Tentativas erradas:** depois de 10 erros em 10 minutos, o IP fica bloqueado até a janela passar.
- **Scripts e monitoramento:** as mesmas credenciais funcionam por autenticação básica, por exemplo `curl -u client:'vErt1Ce@2026' http://<ip>:8080/api/status`.
- **HTTPS:** o explorer serve HTTP puro, então a senha trafega sem criptografia. Fora de uma rede fechada ou VPN, coloque um proxy HTTPS na frente (o cookie passa a ser `Secure` quando o proxy envia `X-Forwarded-Proto: https`).

## Tela de configuração (`/explorer-conf/admin`)

A configuração só abre para o usuário `admin`, que a vê no menu como **Config**. Ela tem três partes.

**1. Conexão**

| Campo | Padrão | Observação |
| --- | --- | --- |
| Endereço RPC do nó | `http://127.0.0.1:8545` | O nó na mesma máquina |
| Endereço do contrato BRLN | vazio | Vazio desativa a página BRLN |

Ao salvar, o explorer **confere** se o nó responde e se existe contrato no endereço do BRLN. Se algo falhar, mostra o erro e **não altera** a configuração. Se tudo der certo, grava o arquivo, descarta o banco em memória e reindexa. **Restaurar padrões** volta aos valores da tabela. Rótulos e logo são mantidos.

**2. Rótulos de endereços:** dê um nome a qualquer endereço (até 40 caracteres), por exemplo "Núclea – operador" ou "BEE4". O rótulo aparece no lugar do endereço em todas as páginas (transações, blocos, pendentes, BRLN, validadores), sempre acompanhado do endereço abreviado. Também funciona na busca e nos filtros. Salvar ou remover um rótulo vale na hora e **não reindexa** a rede.

**3. Logo:** envie o logo (PNG, JPG, WebP ou SVG, até 512 KB, de preferência SVG com fundo transparente). Ele aparece no **canto superior esquerdo** e, **animado no centro da tela**, na abertura do explorer (primeira página da sessão, por pelo menos 1,2 s) e a cada troca de página. Há um segundo campo, opcional, para a versão do logo para o **tema escuro** (por exemplo, o logo em branco). Os arquivos ficam na mesma pasta da configuração (`explorer-logo.<ext>` e `explorer-logo-dark.<ext>`).

**Logo padrão embutido:** se existirem `web/static/brand/logo.svg` (ou `.png`, `.jpg`, `.webp`) e, opcionalmente, `web/static/brand/logo-dark.svg` no código-fonte, eles vão dentro do executável e valem enquanto nenhum logo for enviado pela tela. Sem nenhum dos dois, aparece a marca ≋.

**Observações:**
- **Prioridade:** padrões < arquivo < opções `--rpc` / `--brln`. Se o explorer for iniciado com essas opções, elas prevalecem sobre a tela a cada reinício, e a tela mostra um aviso.

Exemplo do arquivo gravado:

```json
{
  "rpc": "http://127.0.0.1:8545",
  "brln": "0x...",
  "labels": { "0xf39f...2266": "Núclea – operador" },
  "logo": "explorer-logo.svg"
}
```

## Atualização automática (a cada bloco)

As páginas se atualizam **a cada bloco novo**, sem recarregar e sem perder a rolagem:

- O servidor avisa o navegador quando indexa um bloco (Server-Sent Events em `/api/events`, uma conexão por aba). O navegador busca a página e aplica **só o que mudou**:
  - nas tabelas de blocos, transações, transferências BRLN, maiores saldos e pendentes, as **linhas novas entram no topo com destaque** na cor-tema e as mais antigas saem. As linhas que não mudaram não são tocadas;
  - contadores, paginação e dados do endereço são trocados se mudaram;
  - filtros e busca não são afetados enquanto você digita.
- Os tempos relativos ("há 12s") são calculados no navegador pelo relógio do servidor, a cada segundo.
- Na **página 2 em diante**, as linhas não mudam sob o leitor: aparece o aviso "Há registros novos", com link para os mais recentes.
- A página **Pendentes** (mempool) muda entre blocos, então é consultada a cada `--refresh` segundos (padrão 5).
- Se o aviso de blocos cair (por exemplo, um proxy que não repassa streaming), o navegador passa a consultar a cada `--refresh` segundos e tenta reconectar a cada 15 s.
- O botão **Ao vivo / Pausado** no cabeçalho liga e desliga, e a escolha fica salva no navegador. Com a aba em segundo plano, nada é buscado; ao voltar, a página se atualiza. A tela de configuração não se atualiza sozinha.
- **Atraso até o bloco aparecer:** até `--poll` (padrão 1 s) mais o tempo de buscar o bloco no nó.
- **Proxy na frente (nginx):** o `/api/events` precisa de `proxy_buffering off` (o explorer já envia `X-Accel-Buffering: no`) e de `proxy_read_timeout` maior que 15 s; o servidor manda um sinal a cada 15 s.

## Opções

| Opção | Variável | Padrão | Descrição |
| --- | --- | --- | --- |
| `--config` | `EXPLORER_CONFIG` | `besu-explorer.json` | Arquivo de configuração |
| `--listen` | `EXPLORER_LISTEN` | `:8080` | Endereço e porta da interface (`127.0.0.1:8080` para acesso só local) |
| `--rpc` | `EXPLORER_RPC` | — | Sobrepõe o RPC do arquivo |
| `--brln` | `EXPLORER_BRLN` | — | Sobrepõe o BRLN do arquivo |
| `--admin-password` | `EXPLORER_ADMIN_PASSWORD` | `Deltasp+=ks3` | Senha do usuário `admin` |
| `--client-password` | `EXPLORER_CLIENT_PASSWORD` | `vErt1Ce@2026` | Senha do usuário `client` |
| `--refresh` | `EXPLORER_REFRESH` | `5` | Intervalo da consulta da página Pendentes e da consulta de reserva, em segundos. As demais páginas se atualizam a cada bloco. `0` desliga toda a atualização automática |
| `--abi-dir` | `EXPLORER_ABI_DIR` | — | Pasta com ABIs `.json` extras (ABI pura ou artefato Foundry/Hardhat) para decodificar funções e eventos de outros contratos |
| `--start-block` | — | `0` | Primeiro bloco a indexar |
| `--batch` | — | `100` | Blocos por requisição em lote |
| `--workers` | — | `4` | Lotes em paralelo na sincronização inicial |
| `--poll` | — | `1s` | Intervalo de consulta de novos blocos (também define o atraso máximo até o bloco aparecer na tela) |
| `--brln-decimals` | — | `18` | Casas decimais se `decimals()` do contrato não responder |
| `--tz` | — | `America/Sao_Paulo` | Fuso horário das datas |
| `--version` | — | — | Mostra a versão |

Monitoramento: `GET /api/status` (com login, por exemplo `curl -u client:...`) retorna JSON com o bloco atual, o progresso, os totais e o último erro.

## Como funciona

1. **Sincronização inicial:** busca os blocos em lotes (`eth_getBlockByNumber` em batch) e os recibos dos blocos com transações (`eth_getBlockReceipts`, ou `eth_getTransactionReceipt` se o nó não tiver o primeiro).
2. **Acompanhamento:** depois, consulta novos blocos a cada `--poll` e avisa as páginas abertas a cada bloco gravado. O QBFT tem finalidade imediata, então não há reorganização para tratar.
3. **Blocos vazios** guardam só um resumo de ~100 bytes. Transações guardam dados completos e recibo.
4. **Pendentes:** a página consulta o nó na hora (`txpool_besuPendingTransactions`, `txpool_besuTransactions` e `txpool_besuStatistics`); o mempool não é guardado em memória. "Pronta" e "aguardando nonce" são calculados comparando o nonce de cada transação com o nonce já confirmado da conta (`eth_getTransactionCount ... latest`). O mempool é **do nó consultado**; cada um dos outros nós tem o seu.
5. **Conferência do supply BRLN:** a cada bloco novo, o `totalSupply()` do contrato é lido **no último bloco indexado** (não no `latest`) e comparado com a soma dos eventos (emissões menos queimas) até esse mesmo bloco. Só emissão e queima mudam o total, então o resultado só é recalculado depois delas; enquanto a leitura do bloco da emissão ou queima não chega (menos de um segundo), o card mostra "conferindo". O aviso **"eventos somam X"** só aparece em divergência real, por exemplo se o contrato mudar o supply sem emitir `Transfer`. A conferência começa depois da sincronização inicial (nós Besu com armazenamento Bonsai não guardam o estado de blocos antigos). O resultado também sai em `/api/status` (`token.supplyCheck`: `ok`, `divergente`, `conferindo` ou `aguardando`), útil para alertas.
6. **BRLN:** usa o evento padrão `Transfer(address,address,uint256)` do ERC-20. Funciona com qualquer versão do contrato que emita esse evento. Os saldos da lista são calculados pelos eventos; na página do endereço, aparece o `balanceOf` real do contrato.

## Limitações

- **Transações internas** (chamadas entre contratos) não são mostradas, porque exigiriam as APIs `DEBUG`/`TRACE` no nó.
- **Motivo do revert** só aparece se o nó for iniciado com `--revert-reason-enabled`.
- **Sem verificação de código-fonte de contratos.** A decodificação usa as assinaturas ERC-20 embutidas e as ABIs de `--abi-dir`.
- **Reinício:** tudo é reindexado.
- **Pendentes:** mostra até 1.000 transações do mempool. Sem a API `TXPOOL`, a página só explica como habilitá-la.

## Compilar a partir do código-fonte

O projeto não tem dependências externas (só a biblioteca padrão do Go), então compila **offline**:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o besu-explorer .   # RHEL 9 x86_64
go test ./...
```

Go 1.24+.
