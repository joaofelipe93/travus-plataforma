# CLAUDE.md: notificador de check-in (WhatsApp)

Serviço da Travus Plataforma: recebe o webhook de reserva/cancelamento do PMS e publica a mensagem num **grupo do WhatsApp**. Veio de `~/Documentos/airbnb` (que rodava com PM2 na VM `appairbnb`); aqui roda em container. Leia também o `CLAUDE.md` da raiz.

O projeto é escrito em português — comentários, logs, mensagens de commit e documentação. Mantenha esse padrão.

## Na plataforma

- Container `checkin-whatsapp` (`Dockerfile`: Node 24). `HOST=0.0.0.0` dentro do container: a porta não é publicada, só o Traefik e a rede do compose a alcançam.
- **Banco: o Postgres da plataforma, schema `checkin`** (`checkin.eventos`, `checkin.mensagens`), criado pela migração `api/migrations/00006_checkin_whatsapp.sql`. Mudança de tabela é migração nova na API (goose), nunca DDL no serviço. O serviço conecta com o papel `checkin` (`DATABASE_URL`), que só tem SELECT/INSERT/UPDATE nesse schema e não enxerga o resto da plataforma; o login é ligado pelo `api migrate up` com `CHECKIN_DB_SENHA`. As consultas usam sempre o nome qualificado (`checkin.mensagens`).
- Sessão do WhatsApp em `/dados` (volume): `auth_info/` (**credenciais do número pareado**). Fica fora do banco de propósito: não é dado de consulta. Nunca na imagem nem no git.
- **Envio que sai mas não é registrado** (Postgres caiu entre o `sendText` e o UPDATE): o id fica em memória (`enviadasSemRegistro`) e é registrado antes de qualquer outro envio. Não troque isso por "tenta de novo no próximo ciclo": a mensagem sairia duas vezes no grupo.
- **Um processo só.** Duas conexões na mesma sessão do Baileys derrubam uma à outra, e cada processo tem o próprio laço da fila (mensagem duplicada no grupo). Não rode este serviço localmente com a sessão da produção.
- Os testes (`npm test`, também no `make test` da raiz) usam só dados fictícios: o repositório é público.

## Comandos

```bash
npm run dev             # tsx watch, recarrega a cada alteração
npm run typecheck       # tsc --noEmit — rode antes de considerar qualquer mudança pronta
npm test                # suíte completa (node:test + tsx)
npm run test:watch      # re-roda ao salvar
npm run typecheck:tests # checa tipos incluindo tests/
npm run build           # gera dist/
npm start               # roda dist/ (produção)
```

Antes de considerar qualquer mudança pronta: `npm run typecheck` **e** `npm test`. Não há linter.

Os testes usam o runner nativo do Node — sem framework. Cada arquivo roda em processo próprio, **um de cada vez** (`--test-concurrency=1`: dividem as tabelas do Postgres de teste, `travus_teste`, com as migrações aplicadas pelos testes da API). Rode pelo `make test` da raiz (container `checkin-teste`); fora dele, sem `TEST_DATABASE_URL`, só os testes que não tocam no banco passam. `tests/helpers/setup.ts` **precisa ser o primeiro import**: `config/env.ts` valida o ambiente (e faz `process.exit(1)` se faltar variável) e `db/index.ts` cria o pool já no momento do import. O laço da fila expõe `aguardarCiclo()` para os testes (e o encerramento) esperarem o ciclo em andamento. O teste do worker troca `whatsapp/client.ts` e `whatsapp/sender.ts` por dublês via `mock.module` — daí a flag `--experimental-test-module-mocks` no script. Nenhum teste abre socket nem fala com o WhatsApp.

**A suíte só roda no Node 24.** No 22 o `mock.module` não expõe os named exports do dublê para quem importa estaticamente, e `src/whatsapp/outbox.ts` importa `getStatus`/`isConnected` de `./client.js` assim — o arquivo do worker nem carrega. Isso é limitação do dublê, não da aplicação: no 22 o typecheck e o build passam e quase todos os testes rodam, e por isso `engines` continua em `>=22`. Se for preciso rodar a suíte no 22, o caminho é trocar `mock.module` por injeção de dependência em `startOutboxWorker` — não mexer no especificador do mock, que já foi testado e não resolve.

Endpoints exigem `x-webhook-token: <WEBHOOK_SECRET>` (ou `Authorization: Bearer`), exceto `/health`:

| Método | Rota | Descrição |
|---|---|---|
| `POST` | `/webhooks/nova-reserva` | Recebe o webhook (qualquer JSON). Responde `queued`, `duplicate` ou `stored_no_target`. |
| `GET` | `/health` | Estado do serviço, da conexão e do outbox. |
| `GET` | `/whatsapp/status` | Estado da conexão + **string do QR**. |
| `GET` | `/whatsapp/groups` | Grupos e JIDs. |
| `POST` | `/whatsapp/test` | Mensagem de teste **no grupo real**. |
| `GET` | `/events?limit=20` | Payloads recebidos (dados de hóspedes). |

## Arquitetura

```
webhook → grava em `checkin.eventos` → responde 200 → enfileira em `checkin.mensagens` → laço da fila → Baileys → grupo
```

O ponto central é que **a resposta HTTP e o envio do WhatsApp são desacoplados**. O socket do Baileys pode estar reconectando quando o webhook chega, e o provedor não pode esperar por isso. O handler em `src/routes/webhook.ts` nunca chama o WhatsApp; ele só enfileira. Quem entrega é o worker de `src/whatsapp/outbox.ts`, num `setInterval` que **pula o ciclo inteiro quando `isConnected()` é falso** — assim uma desconexão não consome tentativas de retry.

Consequência para quem for mexer: não adicione envio síncrono no caminho do webhook, mesmo que pareça mais simples.

### Idempotência

`dedupe_key` vem do primeiro campo de id encontrado no payload (`id`, `reservation_id`, `booking_id`…) ou, na falta deles, do SHA-256 do corpo. Provedores de webhook reenviam, e sem isso o grupo receberia a mensagem duas vezes.

A sutileza: um evento repetido só é rejeitado se **já gerou mensagem** (`hasMessageForEvent`). Sem essa checagem, eventos gravados antes de `WHATSAPP_GROUP_JID` existir ficariam presos como duplicados para sempre e nunca seriam enviados — exatamente na janela de setup inicial. Não simplifique isso para um `if (isDuplicate) return`.

### `src/domain/checkin.ts` é o ponto de refatoração

`normalizeCheckin` faz busca tolerante do mesmo campo em vários nomes e aninhamentos (`guest.name`, `hospede.nome`, `guest_name`…) e aceita não achar nada.

O primeiro payload real já é conhecido — um workflow de PMS entregando reservas do Booking, com `guest_name`, `property_name`, `guest_phone`, `booking_uuid`, datas em `dd/mm/aaaa` e números como texto (`"guests": "2"`). Está congelado como fixture em `tests/domain/checkin.test.ts` e `tests/domain/template.test.ts`. O bloco `_payload:_` que ia anexado na mensagem **já foi removido**: ele existia para revelar esse formato.

O mesmo webhook entrega **reserva e cancelamento**, distinguidos pelo `status` (issues #8 e #9). `isCancelamento` casa por prefixo (`cancel…`) para cobrir as grafias em inglês e português, e aceita `cancellation_reason` preenchido como sinal alternativo. O formato das duas mensagens está fixado por teste — mudanças de texto quebram a suíte de propósito.

A busca tolerante **continua** de propósito. Há um exemplo de um canal só; um parse estrito com zod agora rejeitaria variações ainda não vistas (outros canais, cancelamento, alteração de reserva). A rede de segurança é a linha `⚠️ Formato não reconhecido` da mensagem, mais o `warn` em `routes/webhook.ts` — se aparecerem, o formato mudou e há payload novo em `GET /events`.

Ao ajustar campos, **nada fora de `checkin.ts` e `template.ts` deve precisar mudar** — se precisar, algo vazou de camada. A exceção conhecida é `ID_FIELDS` em `routes/webhook.ts`, que é chave de deduplicação, não exibição.

**Cuidado com a chave de deduplicação.** Ela tem duas armadilhas, e as duas já morderam.

O corpo desse provedor inclui `_workflow_execution_id`, que muda a cada execução. Se o campo de id da reserva não estiver em `ID_FIELDS`, a chave cai no SHA-256 do corpo inteiro e um reprocessamento vira mensagem duplicada no grupo. Foi o que aconteceu com `booking_uuid` antes de ele ser adicionado.

E o **cancelamento chega com o mesmo id da reserva**, mudando só o `status`. Por isso a chave leva um sufixo de status quando ele não é `confirmed` (`nova-reserva:<uuid>:cancelled`) — sem isso o cancelamento casaria com a reserva original e seria descartado como duplicata, e o grupo nunca saberia. O `confirmed` e a ausência de status ficam **sem** sufixo de propósito: mudar a chave das reservas invalidaria as já gravadas em produção e duplicaria a próxima mensagem de cada uma.

Consequência para quem for mexer: ao adicionar um novo tipo de evento que reaproveita o id da reserva, o status (ou algo equivalente) precisa entrar na chave.

## Restrições que não são negociáveis

- **A Cloud API oficial da Meta não envia para grupos**, só para conversas individuais. Por isso o Baileys (WhatsApp Web) está embutido. Não sugira trocar por Cloud API/Twilio enquanto o destino for um grupo.
- **`baileys` está fixado em `6.7.24`** (dist-tag `legacy`). A `7.0.0-rc*` tem quedas silenciosas de conexão relatadas. Não atualize sem verificar se saiu uma estável.
- **Só o webhook é público.** Não publique a porta do container nem crie rota no gateway para `/whatsapp/*` ou `/events`: `GET /whatsapp/status` devolve a string do QR — quem a capturar pareia o próprio dispositivo na conta — e `GET /events` devolve os payloads completos das reservas. O `WEBHOOK_SECRET` viaja em header, então só por HTTPS.
- **`/dados` (ou `data/`, rodando fora do container) contém as credenciais da sessão do WhatsApp** (`auth_info/`). Nunca versionar, nunca colar conteúdo em logs ou PRs. `checkin.eventos` guarda os payloads completos (nome, telefone e e-mail de hóspedes).
- O número pareado é um chip dedicado. Baileys é não-oficial e o número pode ser bloqueado pela Meta.

### Detalhes do Baileys que já causaram problema

- `printQRInTerminal` está deprecado — o QR é tratado no evento `connection.update` (campo `qr`) em `src/whatsapp/client.ts`.
- `creds.update` → `saveCreds` é obrigatório, senão a sessão não persiste e o QR reaparece a cada restart.
- `DisconnectReason.restartRequired` (515) é **esperado** logo após o pareamento e exige reconexão imediata — não confundir com `loggedOut` (401), que invalida as credenciais e exige apagar `AUTH_DIR`.
