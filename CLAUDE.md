# CLAUDE.md

Guia para o Claude Code (e para quem mais trabalhar aqui). Interface, mensagens, logs e commits em **português**.

## O que é

**Travus Plataforma**: sistema web com gateway (Traefik) ao qual vamos adicionando serviços. O primeiro é a automação de **Credenciamento de Lance** no Newcon (Canopus Consórcios), portada do script de terminal validado em produção em `/home/joao/Documentos/newcon-automation`, que continua sendo a ferramenta em uso até a plataforma substituí-lo.

Tudo o que se sabe do Newcon (seletores, fluxos, armadilhas) está em **`docs/canopus-newcon.md`. Leia inteiro antes de mexer no worker.**

Na tela inicial fica o **Assistente** (`api/internal/assistente`): um agente com o Claude que responde sobre todos os serviços e o banco, só consultando (ver "Assistente").

O segundo serviço é o **notificador de check-in** (`workers/checkin-whatsapp`): recebe o webhook de reserva/cancelamento do PMS e publica num grupo do WhatsApp (Baileys) um resumo diário das reservas (08h "para Hoje", 17h "para Amanhã"). Veio de `/home/joao/Documentos/airbnb`, que roda com PM2 na VM `appairbnb` até a migração (`docs/producao.md`, seção 9). **Antes de mexer nele, leia `workers/checkin-whatsapp/CLAUDE.md`** (deduplicação, Baileys, um processo só).

## Regras inegociáveis

1. **Nunca registre lance real.** Não rode `--confirm`, `npm run real` nem nada que clique em "Confirmar" (`#ctl00_Conteudo_btnConfirma`) no Newcon, nem para testar. O Newcon **aceita lances repetidos** na mesma assembleia: um teste errado vira lance duplicado de verdade. Para testar:
   - **dry-run pela plataforma** (tela Execuções → Novo dry-run) ou `make worker-dry-run` (script legado, 1 cota). Vai até a tela de credenciamento, marca "2º Fixo", tira screenshot e não confirma. **Criar um dry-run faz o worker entrar no Newcon**: testes automáticos (`make test`, `make smoke`) nunca criam execução;
   - **reimpressão pelo Histórico** para testar o PDF: na tela de credenciamento, `#ctl00_Conteudo_btnHistorico` → `input[id$="srcPrint"]` de uma linha.

   Execução real só com pedido explícito do usuário, na hora.
2. **Segredos e dados de clientes nunca entram no git**: qualquer `.env` (exceto `.env.example`), `token.json`, `credentials.json`, `client_secret_*.json`, `cotasreal.csv` (nomes, telefones, e-mails), PDFs, screenshots, logs, `deploy/.env`, `deploy/data/`, a sessão do WhatsApp (`workers/checkin-whatsapp/data/`, volume `checkin-dados`) e payloads de reservas (nomes e telefones de hóspedes). Testes usam só dados fictícios. Antes de cada commit confira `git status` e o conteúdo adicionado. **O repositório no GitHub (`joaofelipe93/travus-plataforma`) é público.**
3. **Não mexa na VM `appairbnb`.** Ela roda o notificador de check-in antigo em produção até a migração terminar (`docs/producao.md`, seção 9); quem troca o webhook no PMS e a desliga é o usuário. Nada da plataforma roda nela. **Não altere nada em `/home/joao/Documentos/airbnb`**: só copie de lá.
4. **O worker nunca repete automaticamente a etapa de Confirmar** (ver "Proteção contra lance duplicado").
5. **Não altere nada em `/home/joao/Documentos/newcon-automation`.** Só copie de lá.
6. Se algo do Newcon se comportar diferente de `docs/canopus-newcon.md`, **pare e avise** antes de mudar a lógica.
7. Sem push, repositório remoto novo ou deploy sem pedido explícito. Trabalhe por etapas: proponha o plano, espere o ok e pare no fim de cada etapa para validação. Commits pequenos e descritivos, no padrão `feat:`/`fix:`/`docs:`/`ci:`… (o versionamento vai ler).
10. **A `main` é protegida: só entra por PR com a CI verde** (vários agentes trabalham ao mesmo tempo). Nunca faça push direto na `main` nem tente contornar a proteção. **Siga o [`CONTRIBUTING.md`](CONTRIBUTING.md)**: uma área por tarefa (`make worktree b=tipo/assunto`), `make test` e `make verificar` antes do PR, título no padrão, branch atualizado com a `main`. Merge do PR de versão, aprovação de deploy e "Voltar versão" são só do usuário.
8. Uma sessão de login no Newcon por vez: nunca rode dry-run da plataforma, `make worker-dry-run` e scripts de descoberta ao mesmo tempo.
9. **WhatsApp do notificador**: nunca pareie o chip da produção fora da VM de produção (`make checkin` local mostra QR de verdade: não escaneie) e nunca rode dois notificadores com a mesma sessão. "Enviar mensagem de teste", `POST /whatsapp/test` e um webhook com o token certo **mandam mensagem ao grupo real**: só com pedido do usuário. Testes automáticos usam dublês do WhatsApp; os smokes só mandam webhook sem token.

## Comandos

```bash
make up              # sobe traefik, postgres, migrate, api, web e worker (cria/completa deploy/.env)
make checkin         # também sobe o notificador de check-in (conecta ao WhatsApp e mostra QR: não escaneie)
make down            # derruba os containers (o banco fica no volume postgres-dados)
make logs s=worker-canopus   # logs (sem s=, de todos)
make ps              # estado dos containers
make migrate         # aplica migrações pendentes
make usuario args='criar --email ana@exemplo.com --nome "Ana" --perfil operador'
                     # também: listar | senha --email X | desativar --email X | ativar --email X
make test            # api e checkin-whatsapp (com Postgres de teste), web (eslint + tsc), worker Canopus (node:test, sem Newcon); um por vez na máquina
make verificar       # checagens de segurança da CI: arquivos proibidos, migrações seguras, gitleaks
make worktree b=feat/assunto   # área de trabalho de um agente (../travus-feat-assunto, .env certos, Newcon fictício)
make smoke           # checagens pelo gateway com curl (precisa de make up; não cria execução)
make dev-web         # web com next dev (recarga automática) atrás do Traefik; make up volta ao normal
make sqlc            # regera api/internal/db depois de mudar migrações ou consultas
make paridade-csv    # regera o esperado do teste de paridade rodando o csv.js original
make worker-dry-run  # script legado: dry-run de 1 cota da planilha no container (faz login; nunca confirma)
make google-token    # importa workers/canopus/token.json (Google Drive) cifrado no banco
make google-status   # mostra token, client OAuth, pasta e LANCE_REAL_HABILITADO
make alerta-teste    # e-mail de teste com SMTP_* e ALERTA_* do deploy/.env
make backup          # backup do Postgres agora (deploy/backups, fora do git: dados de clientes)
make restaurar-teste # restaura o último backup num banco temporário e compara com o banco em uso

# Produção (docs/producao.md)
make prod-local      # modo produção nesta máquina (HTTPS não confiável); volte com make down && make up
make smoke-producao APP=app.<domínio> API=api.<domínio> [INSEGURO=1]   # checagens HTTPS de fora
make deploy VM=travus@<ip>         # publica o commit atual na VM (sem push; recusa com execução em andamento)
make deploy-voltar VM=travus@<ip>  # volta para a versão anterior (migrações não são desfeitas)
make backup-baixar VM=travus@<ip>  # copia o último backup da VM para deploy/backups
```

Endereços locais: http://app.localhost (web e, em `/api/...`, a API), http://api.localhost (API para ferramentas; só `/health` é público), http://traefik.localhost (dashboard).

Não há cadastro público de usuários: só `make usuario`. A senha é pedida no terminal ou lida da entrada padrão (mínimo 12 caracteres). `deploy/.env` guarda a senha do Postgres, o `WORKER_TOKEN`, a `CHAVE_CRIPTOGRAFIA` (sem ela, o token do Google no banco não decifra), `CHECKIN_WEBHOOK_SECRET` (o token que o PMS manda), `CHECKIN_ADMIN_TOKEN` (API → notificador), `CHECKIN_DB_SENHA` (papel `checkin` no Postgres) e `ASSISTENTE_DB_SENHA` (papel só leitura `assistente`), todos gerados pelo `make` (e pelo `publicar.sh` na VM), e, opcionalmente, `ANTHROPIC_API_KEY` (sem ela, o assistente fica indisponível), `ASSISTENTE_MODELO` (padrão `claude-opus-5`), `ASSISTENTE_PERGUNTAS_POR_HORA` (padrão 60), `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_DRIVE_PASTA_ID` e `CHECKIN_WHATSAPP_GROUP_JID` (grupo inicial; o admin escolhe pela tela).

**`LANCE_REAL_HABILITADO`** (padrão `false`, na API e no worker): só `true` liga o lance real. Desligada, a API recusa aprovar (403) e não entrega execução `real`, e o worker nem carrega `lance-real.js`. **Nunca ligue sem pedido explícito do usuário, na hora.**

## CI (GitHub Actions)

`.github/workflows/ci.yml` roda em todo PR para a `main` e em todo push nela; a proteção da `main` exige todos os jobs verdes, branch atualizado e histórico linear, sem exceção para admin:

| Job | O que garante |
|---|---|
| API (Go) | `go vet`, testes com Postgres (migrações, permissões do papel `checkin`) e `api/internal/db` igual ao que o `sqlc` gera |
| Notificador de check-in | migrações no banco de teste, baileys em 6.7.24, typecheck e testes |
| Web (Next.js) | lint, tipos e `next build` |
| Worker Canopus | sintaxe e testes (inclui a garantia de que não há caminho para Confirmar) |
| Scripts e compose | sintaxe dos scripts, compose local e de produção válidos e **versões em sincronia** (`tools/ci/checar-versoes.sh`: Node e Go dos Dockerfiles iguais aos da CI, `go.mod` cabendo neles, imagem do Playwright igual ao pacote do worker) |
| Segurança | nenhum arquivo proibido versionado (`tools/ci/arquivos-proibidos.sh`) e gitleaks no histórico inteiro (falsos positivos revisados em `.gitleaksignore`; segredo de verdade se troca, não se ignora) |
| Migrações seguras para rollback | `tools/ci/checar-migracoes.sh`: migração existente não se edita; número novo maior que o da `main` e sem repetição (dois PRs com a mesma versão: renumere); o Up não destrói nem renomeia, salvo `-- ci: destrutiva-aprovada: <motivo>` |
| Integração | `make up` com credenciais fictícias do Newcon, build da imagem do notificador e `make smoke` (sem execução, sem WhatsApp) |

Localmente: `make test` (testes) e `make verificar` (segurança, versões e migrações).

**Dependências** (`.github/dependabot.yml`): PRs semanais agrupados por pasta (Go, npm da web e dos dois workers, imagens dos Dockerfiles e as próprias ações), no padrão `chore(deps)`, passando pela CI como qualquer PR. Ficam de fora, de propósito: `baileys` (travado em 6.7.24), `playwright` com a imagem `mcr.microsoft.com/playwright` (sobem juntos, à mão) e as versões **maiores** da imagem `node` e do `@types/node` (o runtime fica no **24 LTS**; o 26 vira LTS em outubro de 2026 e a troca sobe os três Dockerfiles, a CI e os tipos num PR só). Atualização que desencontra versão entre arquivos é barrada pelo job Scripts e compose.

Também é obrigatório o **"Título do PR no padrão"** (`.github/workflows/titulo-pr.yml`): o merge é por squash e o título do PR vira a mensagem do commit na `main`, que o versionamento lê. Formato `tipo(escopo)!: descrição`, com `feat`, `fix`, `perf`, `revert`, `docs`, `ci`, `build`, `refactor`, `test`, `chore` ou `style`; `!` marca mudança incompatível.

## Versões

- **Versão semântica** em `version.txt`, mantida pelo **release-please** (`.github/workflows/release-please.yml`, `release-please-config.json`, `.release-please-manifest.json`). Não edite `version.txt` nem `CHANGELOG.md` à mão.
- A cada push na `main`, o release-please abre ou atualiza o PR **"chore: versão X.Y.Z"** com o `CHANGELOG.md`: `feat` sobe o minor, `fix` e `perf` sobem o patch; enquanto for `0.x`, mudança incompatível (`!`) também sobe só o minor. Só `feat`, `fix`, `perf` e `revert` aparecem no changelog.
- **O merge do PR de versão cria a tag `vX.Y.Z` e o release no GitHub.** Ele passa pela CI como qualquer PR. A decisão de lançar uma versão é do usuário.
- O workflow usa o secret `RELEASE_PLEASE_TOKEN` (token fine-grained só deste repositório com Contents, Pull requests e Issues em leitura e escrita): com o `GITHUB_TOKEN`, o PR de versão não dispararia a CI. O token tem validade: renovar antes de vencer.
- A versão e o commit entram nas imagens pelo build (`VERSAO`/`COMMIT`, exportados pelo `Makefile` e pelo `publicar.sh` a partir de `version.txt` e do git) e aparecem no `/health` da API e do notificador, no log de início do worker, em `/status` e no menu do usuário no trilho. Sem eles, `dev`.

## Deploy (CD)

**Padrão: pelo GitHub, com aprovação.** Plano B: `make deploy` da máquina do usuário (compila na VM).

```
merge do PR "chore: versão X.Y.Z" → tag vX.Y.Z + release → workflow Deploy:
  imagens   compila api, web, worker-canopus e checkin-whatsapp da TAG → ghcr.io/joaofelipe93/travus-*:X.Y.Z
  producao  ESPERA APROVAÇÃO (ambiente "producao") → ssh "publicar vX.Y.Z" → smoke de produção
            + versão no /health → se a troca falhar no meio ou o smoke falhar: rollback automático
```

- **Chave de deploy restrita**: no `authorized_keys` do `travus`, `restrict,command="/opt/travus/bin/entrada-ci.sh"` (instalado por `make configurar-ci VM=… CHAVE=….pub`). Ela só aceita `publicar vX.Y.Z` (a VM baixa o código da **tag** direto do GitHub: a chave escolhe versão, não envia código), `voltar` e `estado`; sem shell, sem terminal, sem túnel. Tags `v*` protegidas por ruleset (só admin cria, altera ou apaga).
- **`publicar.sh --imagens vX.Y.Z`** (com `deploy/docker-compose.imagens.yml`): confere `version.txt` = tag, recusa com execução em andamento, baixa as imagens, faz **backup antes do deploy** (`backup.sh antes-do-deploy`, 10 guardados em `backups/antes-do-deploy`), migra e sobe. **Código 3 = recusado antes de mudar qualquer coisa** (nada a desfazer, o workflow não faz rollback); outro código = falhou no meio da troca (rollback). O histórico (`versoes-publicadas`) guarda `<id> <modo>` e o `--voltar` sobe a anterior do jeito que ela foi publicada (imagens ou compilar). Imagens de versões que saíram das 5 releases guardadas são apagadas.
- **Rollback manual**: workflow **Voltar versão** (digitar `VOLTAR`, mesma aprovação) ou **Deploy** à mão com uma versão já lançada. O código volta; **as migrações não** (por isso a CI exige migrações seguras e há o backup antes de cada deploy).
- Ambiente `producao` no GitHub: aprovação obrigatória do usuário, só `main` e tags `v*`; segredos `DEPLOY_SSH_KEY`, `DEPLOY_KNOWN_HOSTS`; variáveis `DEPLOY_HOST`, `DOMINIO_APP`, `DOMINIO_API`.
- **Aprovar o job "Produção" é o pedido explícito de deploy** (regra 7): nunca aprove por conta própria.
- Os pacotes do GHCR precisam ser **públicos** (a VM baixa sem login; as imagens não têm segredos). Pacote novo nasce privado: tornar público uma vez, em github.com/joaofelipe93?tab=packages.

## Arquitetura

```
                    ┌──────────────────── Traefik (gateway) ─────────────────────┐
 navegador ──HTTP(S)─►  app.<domínio>/api/*  → [ForwardAuth] → strip /api → API:8080│
                    │  app.<domínio>/*      → [ForwardAuth, sem sessão → /login] → web │
                    │  api.<domínio>/*      → [ForwardAuth] → API:8080 (ferramentas)  │
 PMS (webhook) ─────►  POST api.<domínio>/webhooks/nova-reserva → checkin-whatsapp:3000│
                    └───────────────────────────────┬────────────────────────────┘
                          ┌─────────────────────────┴──────┐
                          ▼                                ▼
                    Postgres  ◄── API:8081 (/internal, token) ◄── Worker Canopus (Node)
                  (LISTEN/NOTIFY → SSE)     rede interna, sem gateway   └─► Newcon
                       ▲  schema checkin
                       └── checkin-whatsapp (Node) ◄── API (tela WhatsApp, CHECKIN_ADMIN_TOKEN)
                                 └─► WhatsApp (Baileys) → grupo
```

- **`deploy/docker-compose.yml`** (projeto `travus`). Redes: `travus_borda` (traefik, api, web, checkin-whatsapp) e `travus_interna` (postgres, migrate, api, worker, checkin-whatsapp, cli, api-teste, checkin-teste). Só o Traefik publica porta (80). `migrate` roda `api migrate up` antes da `api`. Perfis: `cli` (usuários), `teste` (Go e `checkin-teste` com Postgres, banco `travus_teste`), `canopus` (script legado `worker-legado`), `checkin` (notificador, só com `make checkin`; em produção fica ligado). `deploy/docker-compose.dev.yml` troca o web por `next dev`.
- **Produção** (Etapa 4, roteiro em `docs/producao.md`; a versão publicada aparece no `/health`): `deploy/docker-compose.prod.yml` vai por cima do compose local (Traefik em 80/443 com Let's Encrypt via `deploy/traefik/traefik.producao.yml`, HTTP → HTTPS, HSTS, dashboard fechado, `backup` diário, notificador de check-in ligado, vigia com pasta de backups, certificado e notificador). Domínios por `DOMINIO_APP`/`DOMINIO_API` (padrão `app.localhost`/`api.localhost`); `CERT_RESOLVER` `le` ou `le-teste`. Na VM (`/opt/travus`): `releases/<commit>` enviadas por `git archive`, `compartilhado/.env` e `compartilhado/canopus.env` (segredos, só na VM), `backups/`, `atual` → versão publicada. `deploy/vm/preparar.sh` (root, uma vez: usuário `travus`, SSH só por chave, ufw, fail2ban, Docker, swap; para se achar a `appairbnb`) e `deploy/vm/publicar.sh` (compila uma imagem por vez, por causa dos 2 GB da VM, e sobe a versão; `--voltar`). Na VM, `make up`/`prod-local`/`dev-web` se recusam a rodar. `deploy/backup/backup.sh`: `pg_dump -Fc` com 7 diários, 4 semanais e 6 mensais, marcadores `ultimo-ok`/`ultimo-erro`.
- **Traefik** (`deploy/traefik/`, montado como pasta: bind mount de arquivo único não enxerga edições). Rotas por labels (`exposedByDefault: false`) com prioridades; middlewares em `dinamico.yml`:
  - `sessao-api` / `sessao-pagina`: ForwardAuth em `http://api:8080/auth/verificar` (só repassa `Cookie`, `trustForwardHeader: false`); a versão de página responde 302 para `/login?proximo=…` (`preserveLocationHeader`);
  - `remover-prefixo-api`, `limite-api` (20/s), `limite-login` (10/min por IP), `cabecalhos-seguranca`.
  - Públicos sem sessão: `app/login`, `app/_next/*`, `app/favicon.ico`, `app/robots.txt`, `app/api/auth/login`, `app/api/health`, `api/health`.
  - O Traefik tem os aliases `api.localhost`/`app.localhost` na rede `borda` (o Next.js no servidor chama a API pelo gateway). Ele leva alguns segundos para ligar rotas de um container recém-saudável (o `make up` espera).
- **API** (`api/`, Go 1.26): `net/http` com mux do Go 1.22+, `pgx/v5`, goose v3 (`goose.NewProvider`, migrações embutidas), **sqlc** (`internal/db`, gerado e commitado). Imagem distroless. Duas portas: **8080** pública (Traefik) e **8081** interna (worker, token `WORKER_TOKEN`, sem rota no Traefik).
  - `internal/auth`: senha argon2id (19 MiB, t=2, p=1, no máximo 2 cálculos simultâneos), token de sessão de 256 bits (o banco guarda só o SHA-256), token CSRF.
  - `internal/httpapi`: rotas; `autenticado` valida a sessão **de novo** (não confia em cabeçalho do gateway) e exige `Origin` permitida + `X-CSRF-Token` em POST/PATCH/DELETE; `exigirPerfil`. Cookie `__Host-travus_sessao` (HttpOnly, Secure, SameSite=Strict); sessão expira com 12 h sem uso ou 7 dias. Auditoria de login, falhas, logout, cadastro (cliente e cota criados, editados, excluídos), ativação de cotas e execuções (criação e cancelamento).
    - `execucoes.go`: criar dry-run (só cotas ativas; `real` é recusado aqui), listar, detalhar (com os lances), cancelar, baixar screenshot/PDF (`/arquivos/{id}`, só com sessão).
    - `interno.go`: fila do worker (`/internal/tarefas/proxima` com `FOR UPDATE SKIP LOCKED` e trava de 2 min, renovar, iniciar/concluir cota, screenshot, eventos, finalizar, liberar no SIGTERM). Trava vencida devolve a execução à fila. Tipos entregues: `dry_run`, `reimpressao` e, só com a chave, `real`. Na finalização, cota que ficou em `confirmacao_iniciada` vira `erro_apos_confirmar`.
    - `reais.go`: `GET /execucoes/{id}/revisao` (revisão do dry-run), `POST /execucoes/reais` (**só admin**, chave ligada, dry-run concluído há menos de 2 h e ainda não aprovado, quantidade de cotas digitada, "registrar mesmo assim" por cota que já tem lance na assembleia), `POST /execucoes/reimpressoes` (comprovante de um protocolo pelo Histórico; `enviar_drive` opcional), `POST /lances/{id}/reenviar-drive`, `GET /integracoes/google-drive` (admin).
    - `interno_real.go`: `pdf`, `confirmacao-iniciada` (confere tipo, chave, cancelamento e a assembleia aprovada; o worker só clica em Confirmar depois do 204), `concluir-confirmacao` (grava o lance), `concluir-reimpressao`.
    - `drive_fila.go`: fila de envio dos PDFs ao Drive (tentativas com espera 1, 2, 4, 8 min; envio travado volta em 10 min; o lance fica registrado mesmo se o envio falhar). `internal/drive`: cliente REST (escopo `drive.file`, nome do arquivo igual ao `reportFileName` do script); `internal/cripto`: AES-256-GCM para o token do Google no banco (tabela `integracoes`).
    - `reservas.go`: `GET /reservas` (**admin e operador**: nome e telefone de hóspedes). Lê `checkin.eventos` direto do banco (funciona com o notificador fora do ar), interpreta o payload com a mesma busca tolerante de `domain/checkin.ts` (datas `dd/mm/aaaa` → AAAA-MM-DD, valor `"1.360,50"` → centavos, noites pelas datas quando faltam) e junta reserva e cancelamento pelo id da reserva (mesmos campos de `ID_FIELDS`) num item só, com a situação do aviso no grupo (última mensagem de cada evento). Lê os 3000 eventos mais recentes (`limite_atingido` avisa). Campo novo do PMS: ajuste as listas de nomes lá e no notificador.
    - `whatsapp.go` + `internal/checkin` (cliente): tela WhatsApp, **só admin**, com auditoria. `GET /integracoes/whatsapp` (estado, número, QR, grupo, fila; notificador fora do ar vira `indisponivel` com 200), `GET …/grupos`, `PUT …/grupo`, `POST …/teste`, `POST …/desconectar` (exige `"confirmar": true`). Repassa ao notificador pela rede interna com `CHECKIN_ADMIN_TOKEN` (`CHECKIN_URL`); o navegador nunca vê o token.
    - `vigia.go`: a cada 5 min confere banco, contato do worker (10 min), fila parada, cota em `erro_apos_confirmar` (24 h), Drive, backup (26 h, erro), disco (80%), certificado (14 dias) e, com `VIGIA_CHECKIN=true` (produção), o notificador (sem resposta ou WhatsApp fora há 10 min, sem grupo, mensagens que desistiram, fila parada); manda e-mail só quando muda (novos, lembrete a cada 12 h, resolvidos), sem nome de cliente, e faz ping no monitor externo (`VIGIA_PING_URL`). `internal/alerta`: SMTP com STARTTLS obrigatório (ou TLS na 465). Sem `SMTP_HOST`, os alertas só vão para o log.
    - `sse.go`: `GET /execucoes/{id}/eventos` (SSE, `Last-Event-ID`, `event: fim`); `Hub` com `LISTEN execucao_eventos` (trigger no insert).
    - `crm.go`: cadastro do Canopus (clientes e cotas digitados). `POST /clientes` (com as cotas juntas, numa transação), `PATCH`/`DELETE /clientes/{id}`, `POST /cotas`, `PUT`/`DELETE /cotas/{id}`. Nome repetido ou cota repetida viram 409; cota com lance ou execução não muda de identidade nem some (409, desative).
    - `assistente.go` + `assistente_ferramentas.go` + `internal/assistente`: assistente da tela inicial (ver "Assistente"). `GET /assistente` (disponível, modelo, o que o perfil pode) e `POST /assistente/conversa` (histórico da tela → resposta em SSE: `texto`, `ferramenta`, `fim`, `erro`). Uma pergunta por vez e `ASSISTENTE_PERGUNTAS_POR_HORA` por pessoa; auditoria `assistente_pergunta` com os tokens gastos.
    - `perfil.go` + `internal/credenciais`: "Meu perfil" (ver "Perfil e credenciais pessoais"). `GET /perfil` (dados, catálogo e o que a pessoa já cadastrou), `PATCH /perfil` (nome, telefone, cargo, observações), `PUT`/`DELETE /perfil/credenciais/{credencial}`, `GET /credenciais` (**só admin**: quem tem e quem falta). Auditoria sem valores (`perfil_atualizado`, `credencial_gravada`, `credencial_removida`).
  - `internal/cadastro`: valida e normaliza o que o formulário manda (nome em maiúsculas com espaços únicos, grupo/cota/versão só com dígitos e zeros à esquerda 6/4/2, modalidade, dia do mês, data AAAA-MM-DD).
  - `internal/importacao`: só histórico. A importação de planilha saiu da plataforma com o CRM; o pacote (e o teste de paridade contra o `csv.js` do script legado, em `testdata/paridade`) fica como referência das regras que o CRM herdou, e a tabela `importacoes` continua porque as cotas importadas apontam para ela.
  - `internal/testedb`: testes de integração (pulados sem `TEST_DATABASE_URL`).
  - `cmd/api`: `serve`, `migrate up|down|status`, `usuario …`, `google importar-token|status`, `alerta testar`, `healthcheck`. Screenshots vencidos (30 dias) são apagados de hora em hora; PDFs de comprovante não expiram.
- **Web** (`web/`, Next.js 16, App Router, TypeScript, Tailwind 4, shadcn/ui estilo base-nova (Base UI, não Radix: use `render` em vez de `asChild`), TanStack Query, `output: "standalone"`). **O Next 16 tem mudanças incompatíveis: leia `web/AGENTS.md` e o guia relevante em `web/node_modules/next/dist/docs/` antes de escrever código** (ex.: `middleware` virou `proxy`; `useSearchParams` precisa de `<Suspense>`; env de runtime com `await connection()`).
  - O navegador chama a API em `/api/...` (mesma origem). `src/lib/api.ts` manda o `X-CSRF-Token`, e em 401 recarrega para o login.
  - `(app)/layout.tsx` busca `/auth/sessao` antes de mostrar as telas; botões aparecem conforme o perfil, mas quem decide é a API.
  - **Um espaço por serviço**, com tema escuro único nas cores da marca Travus Capital (azul-noite e ouro; fontes Inter e Poppins empacotadas por `@fontsource` + `next/font/local`, sem Google no build). `src/lib/servicos.ts` é o registro: cada serviço tem id, nome, descrição e abas com perfis; o trilho (`components/trilho.tsx`, serviço ativo marcado em ouro) e o cabeçalho com abas (`components/espaco-servico.tsx`, usado no `layout.tsx` de cada serviço) saem dele. Serviço novo = entrada no registro + pasta `app/(app)/<id>/` com `layout.tsx`. O ouro fica reservado para a marca, o serviço ativo, o botão principal e o foco.
  - Telas: `/login`, `/` (**Assistente**: chat com saudação, sugestões por perfil, resposta em streaming com markdown via `react-markdown` + `remark-gfm`, sem HTML cru; a conversa fica só na memória da página; `components/assistente.tsx`, `lib/assistente.ts`; o login leva para cá), `/status`, `/perfil` (Meu perfil: dados de contato e acessos pessoais, aberta pelo menu do usuário no trilho; o formulário de cada credencial sai do catálogo que a API manda, os campos são `type="password"` e o valor nunca é exibido de volta) e, por serviço, **Canopus** `/canopus/execucoes` (lista), `/canopus/execucoes/nova` (escolher cotas ativas), `/canopus/execucoes/[id]` (progresso por `EventSource`, cotas, lances já existentes na assembleia, screenshots, comprovantes, log, cancelar, botão "Revisar para lance real"), `/canopus/execucoes/[id]/revisao` (revisão do dry-run: cotas, "registrar mesmo assim", confirmação digitando a quantidade; mostra o bloqueio quando a chave está desligada), `/canopus/cotas` (resumo, filtros que respondem enquanto se digita, ordenação pelo cabeçalho), `/canopus/clientes` e `/canopus/clientes/[id]` (a mesma tela: resumo da carteira, lista à esquerda e o cliente aberto à direita, com cotas, lances, PDF e situação no Drive, Reimprimir e "Buscar comprovante no Histórico"; o endereço com id abre o cliente direto); **Reservas** `/reservas/lista` (admin e operador: reservas em cartões com imóvel, canal, estadia, hóspede, valor, telefone com link para o WhatsApp e se o aviso saiu no grupo; resumo do dia e do mês, visões Próximas/Hoje/Canceladas/Todas, busca e filtros por imóvel e canal; consulta a cada 15 s e destaca com aviso o que chega com a tela aberta) e `/reservas/whatsapp` (só admin: QR com `qrcode.react`, consultado a cada 2 s até conectar, grupo, teste, desconectar).
  - `components/aviso-credencial.tsx`: `AvisoCredencial` e `useCredencial` para um serviço pedir o token pessoal de quem está na tela (com link para `/perfil`) em vez de falhar.
  - O cadastro do Canopus (CRM) tem os componentes próprios: `espaco-clientes.tsx` (lista + painel), `painel-cliente.tsx`, `cadastro-sheets.tsx` (formulários em painel lateral), `campos-cadastro.tsx`, `resumo-carteira.tsx` e `traco-cotas.tsx` (uma marca por cota, ativa ou não).
  - `page.tsx` só pode exportar o componente da página (o build do Next recusa exports extras): componentes compartilhados vão para `src/components/` (ex.: `comprovante.tsx`).
- **Worker Canopus** (`workers/canopus/`, Node + Playwright **1.63.0 exato**; a imagem `mcr.microsoft.com/playwright:v1.63.0-noble` precisa ter a mesma versão do `package-lock.json`).
  - `src/worker.js`: laço da fila. **Só dry-run: não há caminho para Confirmar** (um teste lê o arquivo e falha se aparecer `confirmAndWaitReport`, `downloadReportPdf` ou `btnConfirma`). Por cota: `backToFilter` (a partir da 2ª) → `searchCota` → `lerDadosCredenciamento` → `selectSegundoFixo` → `captureBeforeConfirm` → envia screenshot → `lerHistorico` (só leitura, depois do screenshot) → conclui. `NewconCotaError` → erro conhecido; outro erro → inesperado; ambos com screenshot. SIGTERM: termina a cota atual e devolve à fila (`stop_grace_period: 90s`).
  - `src/newcon.js`, `csv.js`, `logger.js`: o código validado, **sem mudanças**. `src/index.js` é o script legado (`make worker-dry-run`).
  - `src/leitura-credenciamento.js`: seletores de assembleia e do Histórico (só leitura). `src/plataforma.js`: cliente das rotas internas. `src/registro.js`: logger que manda eventos à API. `src/config-worker.js`: só variáveis de ambiente (recusa `NEWCON_URL` com a grafia `frmCorCCCnsLogin`).
  - No container, `docker-entrypoint.sh` bloqueia `--confirm`/`real`, e o Chromium precisa de `shm_size` (ou `--ipc=host`). `legado/` é só referência.
- **Notificador de check-in** (`workers/checkin-whatsapp/`, Node 24 + TypeScript, Fastify, Baileys **6.7.24** fixo; guia próprio em `workers/checkin-whatsapp/CLAUDE.md`).
  - Webhook → `checkin.eventos` (deduplicação pelo id da reserva + status) → `checkin.reservas` → responde 200. **Resumo diário** (issue #17): às 08h "Reservas Confirmadas para Hoje" e às 17h "para Amanhã", todas numa mensagem (`checkin.resumos`, um por tipo e dia, nunca duas vezes); avulsa só para a reserva ou o cancelamento que chega depois do resumo do dia e para payload sem id/data. Tudo vai por `checkin.mensagens` → laço da fila envia ao grupo quando o WhatsApp está conectado (6 tentativas com espera).
  - **Banco: o Postgres da plataforma, schema `checkin`** (migrações 00006, 00007 e 00010 da API; mudança de tabela é migração nova na API). Papel `checkin` só com SELECT/INSERT/UPDATE nesse schema; o login é ligado pelo `api migrate up` com `CHECKIN_DB_SENHA`.
  - Dois tokens: `WEBHOOK_SECRET` (só o webhook, é o que o PMS conhece) e `ADMIN_TOKEN` (`/whatsapp/*`, `/events`: QR e dados de hóspedes, só a API). Única rota no gateway: `POST /webhooks/nova-reserva`.
  - Grupo de destino em `checkin.configuracao` (escolhido na tela) ou `WHATSAPP_GROUP_JID`. Sessão do WhatsApp no volume `checkin-dados` (sem backup: perdeu, pareia de novo). Aparece no celular como "Travus Plataforma".
  - **Um container só**: duas conexões na mesma sessão derrubam uma à outra e cada processo enviaria a fila.
  - Testes (`checkin-teste`, `make test`): `travus_teste` depois dos testes da API, um arquivo por vez, WhatsApp com dublês.
- **`tools/`**: `teste-ip-vm/` (Newcon aceita login do IP de uma VM?), `paridade-csv/` (esperado do teste de paridade), `smoke/etapa1.sh` (gateway, sessão, perfis, rotas de execução e recusa do lance real, sem criar execução), `smoke/producao.sh` (HTTPS de fora: redirecionamento, HSTS, barreira de sessão, dashboard e portas fechadas, validade do certificado; sem login).

## Perfis

| Perfil | Pode |
|---|---|
| `leitura` | ver clientes, cotas, execuções, revisões e comprovantes |
| `operador` | + cadastrar, editar e excluir cliente e cota, ativar/desativar cota, criar e cancelar dry-run, pedir reimpressão de comprovante, enviar comprovante ao Drive, ver as reservas (dados de hóspedes) |
| `admin` | tudo do operador + **aprovar lance real**, ver a situação do Google Drive e **gerenciar o WhatsApp do notificador** (QR, grupo, teste, desconectar) |

Todo perfil usa o **Assistente**, com as ferramentas do perfil: `leitura` só tem `estado_plataforma` e `resumo_canopus`; operador e admin também `listar_reservas` e `consultar_banco`.

Qualquer perfil cuida do **próprio** perfil (dados de contato e credenciais pessoais); só o admin vê o panorama de quem já cadastrou cada credencial.

## Perfil e credenciais pessoais

- **Dados de contato** em `usuarios` (`telefone`, `cargo`, `observacoes`, migração 00008): servem para saber a quem recorrer. Cada pessoa edita só os seus. **E-mail e perfil não se editam pela tela**: o e-mail é o login e o perfil é decisão de admin (`make usuario`). Criar, desativar e trocar senha continuam no terminal.
- **Credenciais pessoais** em `credenciais_usuario`: o token de cada pessoa num serviço de terceiros (ex.: Trello), para o serviço agir em nome dela. O que existe está no catálogo em `api/internal/credenciais` (id, nome, o que é, como obter, campos) — **credencial nova = uma entrada lá**, e a tela e a validação saem dela.
- Os valores vão cifrados com AES-256-GCM (`CHAVE_CRIPTOGRAFIA`, mesmo cofre do token do Google) no contexto `credencial:<usuario_id>:<credencial>`: o que está gravado para uma pessoa não decifra no lugar do de outra. **O valor nunca volta ao navegador nem entra na auditoria**: a tela mostra só a dica (últimos 4 caracteres do campo principal) e as datas. Sem `CHAVE_CRIPTOGRAFIA` a API recusa gravar (503).
- Um serviço usa `credenciais.Ler(ctx, q, cofre, usuarioID, "trello")`; `ErrNaoCadastrada` é o caso normal de "o operador ainda não cadastrou" e vira um aviso pedindo o token, não um erro.

## Assistente

- **Só consulta.** Nenhuma ferramenta cria execução, aprova lance, reenvia PDF, manda WhatsApp ou muda cadastro; se pedirem, ele explica onde a pessoa faz. Ferramenta nova do assistente **nunca** age: se um dia precisar, é decisão do usuário, com confirmação na tela.
- Modelo `claude-opus-5` (pensamento adaptativo, esforço médio, fallback do servidor em recusa), laço de tool use com streaming em `internal/assistente/agente.go` (no máximo 10 rodadas, resultado de ferramenta cortado em 24 mil caracteres). Instruções fixas em `instrucoes.go` (vão para o cache do prompt; **serviço, tabela ou regra nova: atualize o esquema descrito lá**); data, hora e perfil vão num bloco à parte.
- Ferramentas (`httpapi/assistente_ferramentas.go`): `estado_plataforma` (versão, alertas ativos do vigia, worker, Drive, WhatsApp sem QR nem número), `resumo_canopus` (cadastro, execuções, cotas, lances e Drive por período, consultas do sqlc em `queries/assistente.sql`), `listar_reservas` (mesma interpretação da tela Reservas) e `consultar_banco`.
- `consultar_banco`: SELECT livre num **pool próprio com o login do papel `assistente`** (migração 00011: `default_transaction_read_only`, `statement_timeout` 5 s, `NOINHERIT`, 4 conexões), transação `READ ONLY`, 200 linhas. O papel **não enxerga** `sessoes`, `senha_hash`, `dados_cifrados`/`dica`, `arquivos.conteudo`, `importacoes.linhas`, `auditoria.ip` nem `goose_db_version`. Tabela nova não entra sozinha: dê o `GRANT` numa migração se o assistente precisar dela (e descreva em `instrucoes.go`). Não troque o pool por `SET ROLE` na conexão da API: ela é superusuária e um `set_config('role', …)` voltaria a ela.
- Dados de hóspedes e clientes vão para a API da Anthropic nas respostas das ferramentas (decisão do usuário, 23/09/2026); o perfil leitura não recebe as ferramentas de reservas nem a consulta livre.
- Testes com dublê do modelo (`modeloRoteiro`, `modeloDuble`): **nenhum teste chama a Anthropic**. O smoke não manda pergunta.

## Regras do cadastro (CRM)

O Canopus é um CRM: **admin e operador digitam** o cliente e as cotas dele (Clientes → Novo).
Não há mais importação de planilha.

- Cliente = nome normalizado (maiúsculas, espaços únicos); a administradora não dá CPF. Nome repetido é o mesmo cliente: a API recusa com 409.
- Grupo/cota/versão só com dígitos e zeros à esquerda (6/4/2), versão padrão `00`; administradora padrão `CANOPUS`; modalidade padrão `segundo_fixo`.
- O formulário manda todos os campos: **telefone ou e-mail em branco apagam** o contato (ao contrário da importação, em que coluna vazia não mexia no cadastro).
- Os campos que a planilha trazia soltos são colunas da cota: `vendedor`, `forma_pagamento`, `vencimento_parcela`, `dia_assembleia`, `contratacao` (migração 00008, que trouxe o que estava em `dados_planilha`). `dados_planilha` continua no banco com o que veio da importação antiga — não é mais escrito.
- **Cota com lance ou execução é histórico**: não muda de administradora, grupo, cota, versão nem de cliente, e não se exclui (409; para tirá-la das execuções, desative). Cliente com cotas também não se exclui.
- Só a API valida de verdade: a tela ajuda a preencher, mas quem recusa é o servidor.

## Execuções e proteção contra lance duplicado

```
execução:  na_fila → em_andamento → concluida | concluida_com_erros | cancelada | falhou
             ▲            │ (trava vencida ou SIGTERM)
             └────────────┘

cota:  pendente → em_andamento → verificada (dry-run ok) | reimpressa (reimpressão ok)
                      │       └→ confirmacao_iniciada → confirmada        (lance real)
                      ▼                     │
             erro_antes_confirmar     erro_apos_confirmar  (conferência manual pelo Histórico)
       (pendente → cancelada quando a execução é cancelada)

lance real:  dry-run concluído → revisão → admin aprova (chave ligada, < 2 h) → execução real na fila
             worker: confere a assembleia e o Histórico de novo → 2º Fixo → screenshot
                     → grava confirmacao_iniciada (API) → só então clica em Confirmar
                     → protocolo do alert → PDF → concluir-confirmacao (lance + fila do Drive)
```

- Uma execução em andamento por credencial do Newcon (índice único parcial em `execucoes`); o worker pega uma por vez com `FOR UPDATE SKIP LOCKED` e renova a trava a cada 30 s.
- Cancelar: na fila, cancela na hora; em andamento, o worker termina a cota atual e não começa a próxima.
- Worker que parou (trava vencida): a execução volta à fila; cota `em_andamento` volta a `pendente`; cota `confirmacao_iniciada` vira `erro_apos_confirmar` ("o lance pode ter sido registrado, confira no Histórico").
- **Trigger no banco** (`execucao_cotas_proteger_confirmacao`): cota em `confirmacao_iniciada`, `confirmada` ou `erro_apos_confirmar` nunca volta para um estado que permita reprocessá-la. `iniciar` só aceita `pendente` ou `erro_antes_confirmar`.
- `confirmacao_iniciada` é gravada **no banco antes** do clique em Confirmar; sem o 204 da API, o worker não clica. Depois do clique, qualquer falha vira `erro_apos_confirmar` e **nunca** é repetida (`concluirConfirmacao` só tenta de novo *informar* o resultado à API).
- Três camadas: a chave `LANCE_REAL_HABILITADO` (API e worker), a trava gravada antes do clique e o trigger no banco. Um dry-run origina no máximo uma execução real.
- Os avisos "Consorciado já credenciado nesta assembleia…" e "Cota com Parcelas em Atraso…" só aparecem **depois** do clique em Confirmar: o dry-run não os vê. O dry-run lê o Histórico e mostra quantos lances a cota já tem na assembleia atual; a revisão soma os lances já registrados pela plataforma.
- Tabela `lances`: protocolo único por administradora; origem `plataforma` (lance real) ou `historico` (reimpressão). `drive_status`: `pendente`, `enviando`, `enviado`, `erro`, `sem_pdf`, `nao_enviar` (reimpressão sem pedido de envio; dá para enviar pela tela).

## Etapas

- **Etapa 0 (fundação)**: validada.
- **Etapa 1 (login e cadastro)**: validada. Em 17/09/2026 o cadastro virou **CRM**: a importação de planilha
  saiu da interface e da API (o histórico continua legível), admin e operador digitam cliente e cotas, e os campos
  que a planilha trazia soltos viraram colunas da cota. O design das telas ainda vai ser estruturado com o usuário.
- **Etapa 2 (dry-run pela interface)**: validada.
- **Etapa 3 (lance real, implementado e testado sem confirmar)**: revisão, aprovação só por admin, trava antes do clique, protocolo, `lances`, PDF no Postgres (bytea; armazenamento de objetos depois) e no Drive, reimpressão pelo Histórico, auditoria, token do Google cifrado no banco. *Em validação.* O primeiro lance real só com pedido explícito do usuário.
- **Etapa 5 (notificador de check-in)**: trazido do projeto airbnb para `workers/checkin-whatsapp`, SQLite trocado pelo Postgres (schema `checkin`), no compose e no gateway, tela WhatsApp para o admin (QR, grupo, teste, desconectar) e vigia. *Testado localmente sem parear o número*; falta a migração na VM (`docs/producao.md`, seção 9), que depende da Etapa 4.
- **Etapa 6 (assistente)**: chat na tela inicial com o Claude e ferramentas só de leitura, papel `assistente` no Postgres. *Testado com dublê do modelo*; falta a chave da Anthropic (o usuário cria) para o primeiro teste de verdade.
- **Etapa 4 (servidor)**: compose de produção com HTTPS, preparação da VM, deploy e volta, backup na VM com teste de restauração, vigia com alertas por e-mail e monitor externo, smoke de produção. *Parte do repositório pronta e testada localmente (`make prod-local`); falta a VM e o domínio, que o usuário cria depois.*

## Decisões em aberto (não invente a regra)

- Modalidade para cotas sem "2º Fixo" (grupo 6620: só Livre, Fixo, Limitado). Hoje é erro conhecido.
- Domínio (`app.`/`api.`): o usuário define e cria depois.

Decididas pelo usuário no CRM (17/09/2026): a importação por planilha sai da interface e da API, mas a tabela
`importacoes` e o histórico ficam; **admin e operador** cadastram e editam; os campos extras da planilha viram
colunas de verdade na cota.

Decididas pelo usuário na Etapa 3: "Parcelas em Atraso" → aceitar e marcar o lance; cota que já tem lance na assembleia → pular, salvo "registrar mesmo assim" por cota na revisão; só admin aprova lance real; prazo de oferta encerrado → erro conhecido; PDF do teste de reimpressão não vai ao Drive.

Decididas pelo usuário na Etapa 5: o notificador sai da `appairbnb` e roda na VM da plataforma (a `appairbnb` fica até a migração ser validada); container próprio com só o webhook público; um banco só (Postgres, schema `checkin`), por causa do agente de chat que vai responder sobre todos os serviços; parear o número de novo em vez de copiar sessão e histórico; admin conecta e escolhe o grupo pela tela, sem terminal, com botão de desconectar e gerar novo QR.

Decididas pelo usuário na Etapa 4: VM na DigitalOcean `s-2vcpu-2gb` (2 vCPU, 2 GB; o deploy compila uma imagem por vez; redimensionar para 4 GB se apertar), criada pelo usuário; backup do Postgres só na VM, com cópia manual (`make backup-baixar`); alertas por e-mail + monitor externo.
