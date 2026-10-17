# CLAUDE.md: worker de boletos da Canopus

Serviço da Travus Plataforma (issue #48): busca no **Boleto Fácil** da Canopus os boletos em
aberto dos clientes cadastrados e grava na plataforma as parcelas e o PDF. **Quem manda a
mensagem no WhatsApp é a etapa seguinte: aqui nada é enviado a ninguém.**

Leia antes de mexer: o `CLAUDE.md` da raiz e, obrigatoriamente, **`docs/canopus-boleto-facil.md`**
— é lá que está o que se sabe do site (as três rotas, os formatos, o 429, a Cloudflare) e o que
ainda **não** foi verificado. O projeto é escrito em português; mantenha o padrão.

## O que este worker não é

Não é o worker do Newcon. O Boleto Fácil é outro host (`www`, público, PHP/Slim), **sem login**:
a credencial é o CPF do cliente, que vem da API a cada cliente. Este worker **não tem segredo da
Canopus**, não entra no Newcon e não disputa a sessão dele — os dois podem rodar juntos.

## Como roda

```
laço → API: próxima rodada (uma em andamento por vez)
      por cliente: find-cota (CPF) → casa as cotas com o cadastro
                   por cota casada: generate-bill → parcelas com valor > 0
                                    → PDF (só na rodada de envio) → API
                   → conclui o cliente (consultado | sem_boleto | erro)
      → finaliza a rodada
```

- `src/boleto-facil.js`: cliente do site, nos dois modos (ver abaixo), com o recuo do 429.
- `src/cotas.js`: casamento entre a cota do site e a do cadastro. **O site manda `CD_Cota` como
  número e sem zeros à esquerda** (apareceram cotas de 3 e 4 dígitos) e tem um campo `Versao` que
  nenhum template do site usa: comparar como texto erra. Os dois lados são normalizados (6/4/2).
- `src/parcelas.js`: leitura do `generate-bill` (valor em centavos, descarte da linha `DIF` de
  valor zero, rota da impressão por `type`).
- `src/worker.js`: o laço e a decisão por cliente. `src/plataforma.js`: rotas internas da API.
- `src/registro.js`: log em JSON no stdout + eventos na rodada, em lote.

## Regras que não são negociáveis

1. **Ritmo.** O site responde **429**. Há pausa entre clientes (`BOLETOS_PAUSA_CLIENTE_MS`) e
   entre cotas (`BOLETOS_PAUSA_COTA_MS`), e recuo com espera dobrando no 429. **Não troque por
   paralelismo**: a rodada inteira pode ser barrada, e a VM sai por um IP só.
2. **O PDF é buscado logo depois do `generate-bill`**, no mesmo cookie: o `transactionId` muda a
   cada chamada e o link de impressão **não é durável**. Não guarde URL de impressão.
3. **User-Agent de Chrome, sempre.** Com o padrão do Playwright (`HeadlessChrome`) o site
   responde **403** até de um IP que o `curl` atravessa. É o `BOLETO_UA`.
4. **Nenhum teste fala com o site de verdade** (mesma regra do worker do Newcon). Os testes usam
   servidor falso e dublês; o modo navegador não é exercitado em teste automático.
5. **CPF não entra em log.** Ele chega pela API, é usado na chamada e pronto. Nome de cliente
   pode (a tela mostra de qualquer jeito); documento e valor, não.
6. **Erro de um cliente não derruba a rodada**: cada cliente é concluído com o que deu.

## Modos

| Modo | O que faz | Quando usar |
|---|---|---|
| `navegador` (padrão) | Chromium com User-Agent de Chrome; abre a página uma vez e faz os três pedidos por `fetch` **dentro** dela (rede e cookies do navegador, resposta em JSON, PDF em bytes) | enquanto a Cloudflare barrar cliente HTTP do IP de saída |
| `http` | `fetch` do Node com cabeçalho de navegador e porta-cookies próprio | mais leve; só depois de o teste na VM mostrar que passa |

A troca é a variável `BOLETOS_MODO`. Nada mais muda: a interface do cliente é a mesma.

## Comandos

```bash
npm test     # node:test; nenhum teste abre navegador nem fala com o site
node --check src/*.js
make boletos # (na raiz) sobe o container; make logs s=worker-boletos
```

## Variáveis

`API_INTERNA_URL`, `WORKER_TOKEN` (≥32 caracteres) e `WORKER_NOME` falam com a plataforma.
`BOLETOS_MODO`, `BOLETO_SITE`, `BOLETO_BASE`, `BOLETO_UA`, `BOLETOS_TIMEOUT_MS`,
`BOLETOS_PAUSA_CLIENTE_MS`, `BOLETOS_PAUSA_COTA_MS`, `BOLETOS_TENTATIVAS_429`,
`BOLETOS_ESPERA_429_MS`, `BOLETOS_INTERVALO_FILA_MS`, `BOLETOS_RENOVAR_TRAVA_MS` e `HEADFUL`
ajustam o site e o ritmo. Todas têm padrão em `src/config.js`, que **recusa subir** com
`WORKER_TOKEN` curto ou modo desconhecido.
