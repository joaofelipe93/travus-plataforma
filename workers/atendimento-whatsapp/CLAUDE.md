# CLAUDE.md: atendimento no WhatsApp

Serviço da Travus Plataforma (issue #100, etapa D): conecta o WhatsApp **de cada bot** de
atendimento e transporta as mensagens entre a pessoa e a API. Leia também o `CLAUDE.md` da raiz
(seção "Assistente" e a do serviço de Atendimento).

O projeto é escrito em português — comentários, logs, commits e documentação.

## O que torna este serviço diferente dos outros dois de WhatsApp

| | Notificador de check-in | Boletos | **Este** |
|---|---|---|---|
| Sessões | 1 | 1 | **N (uma por bot ligado)** |
| Destino | um **grupo** | privado de **clientes** | privado de **quem escreveu** |
| Volume | `checkin-dados` | `boletos-dados` | `atendimento-dados` (uma subpasta por bot) |
| Banco | papel `checkin` | papel `boletos` | **nenhum** — fala com a API |
| Quem decide o que sai | o próprio serviço | a API | a API |

**Nunca aponte o `AUTH_DIR` para a pasta de outro serviço**, e nunca rode dois destes containers
com o mesmo volume: duas conexões na mesma sessão do Baileys derrubam uma à outra, e cada
processo tem o próprio laço de fila — a pessoa receberia a resposta duas vezes.

## O que ele faz

```
mensagem no WhatsApp do bot X → POST /internal/atendimento/mensagens (API decide tudo)
                                   ↓
                      a API compõe a resposta e põe na fila
                                   ↓
laço (3s) → POST /internal/atendimento/fila/proxima {bots com sessão aberta}
          → por mensagem: confere se o número tem WhatsApp → envia → marca enviada → pausa

laço (30s) → GET /internal/atendimento/bots → abre sessão para bot novo, encerra a do que saiu
```

Ele **não decide nada**: não sabe o que é etapa, teto, ensaio ou instrução de bot, não tem a
chave da Anthropic e não acessa o Postgres. É o carteiro — e é por isso que a peça que mais
reinicia (o socket do WhatsApp) não tem regra de negócio dentro.

## Regras que não são negociáveis

1. **Uma sessão por bot, uma pasta por bot.** O estado não é de módulo (como nos outros dois
   serviços): cada sessão é um objeto com o próprio socket, a própria pasta e o próprio ciclo de
   reconexão. Misturar isso faz um bot responder pelo número do outro.
2. **Ritmo.** Uma mensagem por vez, com pausa entre elas, **inclusive entre bots diferentes** —
   o IP de saída é o mesmo. Rajada no privado de várias pessoas é o caminho curto para a Meta
   bloquear o número. Não troque por paralelismo.
3. **Resposta que saiu e não foi registrada** (a API cai entre o envio e o POST): o id fica em
   memória (`enviadasSemRegistro`) e é registrado antes de qualquer outro envio. Não troque por
   "tenta de novo no próximo ciclo": a pessoa receberia a mesma resposta duas vezes.
4. **Número sem WhatsApp é falha definitiva** (`SemWhatsappError`): não gasta tentativa e aparece
   na tela. A conferência (`onWhatsApp`) existe porque, sem ela, a mensagem "sai" sem erro e
   ninguém recebe nada.
5. **O bot só responde conversa privada.** `telefoneDoJid` descarta grupo, status, canal e lista
   de transmissão, e `fromMe` é ignorado. Bot respondendo em grupo alheio — ou a si mesmo — é
   laço infinito em público.
6. **Telefone não entra em log cru**: use `mascarar()`.
7. **Nenhum teste fala com o WhatsApp nem com a API.** O gerente recebe a fábrica de sessão por
   injeção e o laço da fila recebe a API; os dublês registram o que teria acontecido.
8. **O teto `MAX_SESSOES` existe para o serviço recusar a sessão a mais com mensagem clara**, em
   vez de morrer sem memória no meio de uma conversa. A VM tem 2 GB (`docs/producao.md`): acima
   de 3 ou 4 bots, redimensione antes de subir o teto. Quem ficou de fora aparece em
   `/whatsapp/status` (`sem_sessao`) e na tela.

## Comandos

```bash
npm run typecheck && npm test   # antes de considerar qualquer mudança pronta
npm run dev                     # tsx watch
make atendimento                # (na raiz) sobe este serviço — mostra QR de verdade, um por bot
```

Pelo `make test` da raiz, é o container `atendimento-teste` (não precisa de banco).

## Rotas (nenhuma no gateway)

| Método | Rota | Descrição |
|---|---|---|
| `GET` | `/health` | estado do serviço e quantas sessões estão abertas |
| `GET` | `/whatsapp/status` | **uma linha por sessão**: estado, número, **string do QR**, mais o freio e quem ficou sem sessão |
| `POST` | `/whatsapp/:slug/teste` | mensagem de teste pelo número daquele bot (`{"telefone":"5511…"}`). **Vai para um número de verdade e sai mesmo em modo ensaio** |
| `POST` | `/whatsapp/:slug/desconectar` | desvincula o aparelho **daquele bot**, apaga a sessão e gera QR novo (`{"confirmar":true}`) |

Todas exigem o `ADMIN_TOKEN` (`x-admin-token` ou `Authorization: Bearer`), que só a API tem.
**`GET /whatsapp/status` devolve os QRs**: quem os captura pareia o próprio aparelho nas contas.
Nunca publique a porta nem crie rota no Traefik para cá.

## Variáveis

`ADMIN_TOKEN` (≥32), `API_URL` (a API na rede interna, porta 8081), `WORKER_TOKEN`,
`WORKER_NOME`, `AUTH_DIR` (pasta das sessões, em volume), `ENVIO_HABILITADO` (freio de mão
local, padrão `true`), `LOTE` (respostas por ciclo, padrão 5), `RECONCILIAR_SEGUNDOS` (padrão
30), `MAX_SESSOES` (padrão 4), `PORT`/`HOST`/`LOG_LEVEL`/`LOG_FORMAT`. Faltando o obrigatório, o
processo **não sobe** — e sem a API respondendo no boot ele também não sobe, porque abrir sessões
sem ter a quem entregar as mensagens é pior do que não abrir.
