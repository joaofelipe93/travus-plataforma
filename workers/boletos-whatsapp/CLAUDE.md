# CLAUDE.md: boletos no WhatsApp

Serviço da Travus Plataforma (issue #48): pega a fila de `boletos.mensagens` e manda o boleto
**no privado de cada cliente**. Leia também o `CLAUDE.md` da raiz e o do worker que busca os
boletos (`workers/boletos-canopus`).

O projeto é escrito em português — comentários, logs, commits e documentação.

## Este serviço NÃO é o notificador de check-in

São dois serviços, **dois números** e **duas sessões do Baileys**:

| | Notificador de check-in | Este serviço |
|---|---|---|
| Destino | um **grupo** (`@g.us`) | o **privado de cada cliente** (`@s.whatsapp.net`) |
| Volume da sessão | `checkin-dados` | `boletos-dados` |
| Papel no Postgres | `checkin` (schema `checkin`) | `boletos` (schema `boletos`) |
| Quem enfileira | o próprio serviço (webhook do PMS) | a **API**, quando uma rodada de boletos termina |

**Nunca aponte o `AUTH_DIR` dos dois para a mesma pasta**: duas conexões na mesma sessão do
Baileys derrubam uma à outra, e cada processo tem o próprio laço de fila — o cliente receberia o
boleto duas vezes.

## O que ele faz

```
API põe em boletos.mensagens (pendente) → laço (5s) → confere conexão e o freio
  → por mensagem: confere se o número existe no WhatsApp → manda (documento com legenda, ou
    texto na cobrança) → marca enviada e APAGA o anexo → espera a pausa → próxima
```

Ele **não decide** o que enviar: isso é da API (que só põe na fila o que está liberado pela
chave do servidor e pela configuração). Aqui é o carteiro.

## Regras que não são negociáveis

1. **Ritmo.** Entre uma mensagem e a outra vale `boletos.configuracao.pausa_envio_segundos`.
   Disparo em rajada no privado de dezenas de clientes é o caminho mais curto para a Meta
   bloquear o número. **Não troque por paralelismo** e não "otimize" a pausa.
2. **Envio que sai e não é registrado** (Postgres cai entre o envio e o `UPDATE`): o id fica em
   memória (`enviadasSemRegistro`) e é registrado antes de qualquer outro envio. Não troque por
   "tenta de novo no próximo ciclo": o cliente receberia o boleto duas vezes.
3. **Número sem WhatsApp é falha definitiva** (`SemWhatsappError`): não gasta as cinco
   tentativas e aparece na tela para alguém corrigir o cadastro. A conferência (`onWhatsApp`)
   existe porque, sem ela, a mensagem "sai" sem erro e ninguém recebe nada.
4. **O anexo sai do banco no envio** (`anexo_pdf = NULL`). A cópia do PDF na fila existe só
   enquanto a mensagem está pendente; o original continua em `arquivos`, para a tela.
5. **Telefone não entra em log cru**: use `mascarar()`. E o CPF do cliente nunca chega aqui.
6. **Nenhum teste fala com o WhatsApp.** O laço recebe o "carteiro" por injeção
   (`rodarUmCiclo(carteiro)`), e os dublês registram o que teria sido enviado — por isso não há
   `mock.module` nem a flag experimental que o notificador de check-in precisa.

## Comandos

```bash
npm run typecheck && npm test   # antes de considerar qualquer mudança pronta
npm run dev                     # tsx watch
make boletos                    # (na raiz) sobe este serviço e o worker que busca os boletos
```

Os testes usam o Postgres de teste da plataforma (`TEST_DATABASE_URL`, banco `travus_teste`,
migrações aplicadas pelos testes da API) e rodam um de cada vez. Pelo `make test` da raiz, é o
container `boletos-teste`.

## Rotas (nenhuma no gateway)

| Método | Rota | Descrição |
|---|---|---|
| `GET` | `/health` | estado do serviço, da conexão e da fila |
| `GET` | `/whatsapp/status` | conexão, número pareado, **string do QR**, freios e fila |
| `POST` | `/whatsapp/teste` | mensagem de teste para um telefone (`{"telefone":"5511…"}`). **Vai para um número de verdade** |
| `POST` | `/whatsapp/desconectar` | desvincula o aparelho, apaga a sessão e gera QR novo (`{"confirmar":true}`) |

Todas exigem o `ADMIN_TOKEN` (`x-admin-token` ou `Authorization: Bearer`), que só a API tem.
**`GET /whatsapp/status` devolve o QR**: quem o captura pareia o próprio aparelho na conta.
Nunca publique a porta nem crie rota no Traefik para cá.

## Variáveis

`ADMIN_TOKEN` (≥32), `DATABASE_URL` (papel `boletos`), `AUTH_DIR` (sessão, em volume),
`ENVIO_HABILITADO` (freio de mão local, padrão `true`), `LOTE` (mensagens por ciclo, padrão 5),
`PORT`/`HOST`/`LOG_LEVEL`/`LOG_FORMAT`. Faltando o obrigatório, o processo **não sobe**.
