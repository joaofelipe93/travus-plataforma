# CLAUDE.md

Guia para o Claude Code (e para quem mais trabalhar aqui). Interface, mensagens, logs e commits em **português**.

## O que é

**Travus Plataforma**: sistema web com gateway (Traefik) ao qual vamos adicionando serviços. O primeiro é a automação de **Credenciamento de Lance** no Newcon (Canopus Consórcios), portada do script de terminal validado em produção em `/home/joao/Documentos/newcon-automation`, que continua sendo a ferramenta em uso até a plataforma substituí-lo.

Tudo o que se sabe do Newcon (seletores, fluxos, armadilhas) está em **`docs/canopus-newcon.md`. Leia inteiro antes de mexer no worker.**

## Regras inegociáveis

1. **Nunca registre lance real.** Não rode `--confirm`, `npm run real` nem nada que clique em "Confirmar" (`#ctl00_Conteudo_btnConfirma`) no Newcon, nem para testar. O Newcon **aceita lances repetidos** na mesma assembleia: um teste errado vira lance duplicado de verdade. Para testar:
   - **dry-run**: `make worker-dry-run` (container) ou `HEADFUL=false node src/index.js --dry-run --limit 1` em `workers/canopus`. Vai até a tela de credenciamento, marca "2º Fixo", tira screenshot e não confirma;
   - **reimpressão pelo Histórico** para testar o PDF: na tela de credenciamento, `#ctl00_Conteudo_btnHistorico` → `input[id$="srcPrint"]` de uma linha.

   Execução real só com pedido explícito do usuário, na hora.
2. **Segredos e dados de clientes nunca entram no git**: qualquer `.env` (exceto `.env.example`), `token.json`, `credentials.json`, `client_secret_*.json`, `cotasreal.csv` (nomes, telefones, e-mails), PDFs, screenshots, logs, `deploy/.env`, `deploy/data/`. Antes de cada commit confira `git status` e o conteúdo adicionado. **O repositório no GitHub (`joaofelipe93/travus-plataforma`) é público.**
3. **Não mexa na VM `appairbnb`.** Ela roda outra aplicação em produção e não faz parte desta plataforma. Nada da plataforma roda nela.
4. **O worker nunca repete automaticamente a etapa de Confirmar** (ver "Proteção contra lance duplicado").
5. **Não altere nada em `/home/joao/Documentos/newcon-automation`.** Só copie de lá.
6. Se algo do Newcon se comportar diferente de `docs/canopus-newcon.md`, **pare e avise** antes de mudar a lógica.
7. Sem push, repositório remoto novo ou deploy sem pedido explícito. Trabalhe por etapas: proponha o plano, espere o ok e pare no fim de cada etapa para validação. Commits pequenos e descritivos.

## Comandos

```bash
make up              # sobe traefik, postgres, migrate, api e web (cria deploy/.env na 1ª vez)
make down            # derruba os containers (o banco fica no volume postgres-dados)
make logs s=api      # logs (sem s=, de todos)
make ps              # estado dos containers
make migrate         # aplica migrações pendentes
make test            # go vet/test da api, eslint + tsc do web, sintaxe do worker
make worker-dry-run  # dry-run de 1 cota no container do worker (faz login no Newcon; nunca confirma)
```

Endereços locais: http://app.localhost (web), http://api.localhost/health (API), http://traefik.localhost (dashboard do Traefik).

Por serviço, fora do Docker:

- **API**: `cd api && go test ./...`. O binário tem os subcomandos `serve`, `migrate up|down|status` e `healthcheck` (usa `DATABASE_URL`).
- **Web**: `cd web && npm run dev`. A página consulta `API_INTERNAL_URL` (padrão `http://api.localhost`, que no host só responde com `make up` rodando).
- **Worker legado**: `cd workers/canopus && npm ci && HEADFUL=false node src/index.js --dry-run --limit 1` (usa `.env`, `cotasreal.csv` locais, ignorados pelo git).

## Arquitetura

```
                    ┌─────────────── Traefik (gateway) ───────────────┐
 navegador ──HTTP(S)─►  app.<domínio>   → web (Next.js)               │
                    │  api.<domínio>   → [ForwardAuth] → API (Go)     │
                    └───────────────────────────────┬─────────────────┘
                          ┌─────────────────────────┴─────┐
                          ▼                               ▼
                    Postgres  ◄── tarefas / eventos ── Worker Canopus (Node)
                                  (rede interna, sem gateway)   └─► Newcon, Google Drive
```

- **`deploy/docker-compose.yml`** (projeto `travus`). Redes: `travus_borda` (traefik, api, web) e `travus_interna` (postgres, migrate, api, worker). Só o Traefik publica porta (80); API e Postgres não. O serviço `migrate` roda `api migrate up` e a `api` só sobe depois dele. O worker só roda com `--profile canopus`, para o `up` não fazer login no Newcon.
- **Traefik** (`deploy/traefik/`): rotas por labels (`exposedByDefault: false`); middlewares `limite-api@file` e `cabecalhos-seguranca@file` em `dinamico.yml`. O container do Traefik tem os aliases `api.localhost` e `app.localhost` na rede `borda`, então o Next.js chama a API **pelo gateway** também do lado do servidor. Adicionar um serviço = container + labels. ForwardAuth (`/auth/verificar`) entra na Etapa 1.
- **API** (`api/`, Go 1.26): `net/http` com o mux do Go 1.22+, `pgx/v5` (pool conecta sob demanda), goose v3 via `goose.NewProvider` com migrações `api/migrations/*.sql` embutidas no binário. `sqlc` entra na Etapa 1. Imagem distroless (sem shell).
- **Web** (`web/`, Next.js 16, App Router, TypeScript, Tailwind 4, `output: "standalone"`). **O Next 16 tem mudanças incompatíveis: leia `web/AGENTS.md` e o guia relevante em `web/node_modules/next/dist/docs/` antes de escrever código.** Env de runtime: `await connection()` antes de ler `process.env`. shadcn/ui e TanStack Query entram na Etapa 1.
- **Worker Canopus** (`workers/canopus/`, Node + Playwright **1.63.0 exato**; a imagem `mcr.microsoft.com/playwright:v1.63.0-noble` precisa ter a mesma versão do `package-lock.json`). Por enquanto é o script legado (`src/index.js`, `src/newcon.js`); `legado/` é só referência. No container, `docker-entrypoint.sh` bloqueia `--confirm`/`real`, e o Chromium precisa de `shm_size` (ou `--ipc=host`). Na Etapa 2 vira worker de tarefas que fala com a API por rotas `/internal/...` com token de serviço, só na rede interna, nunca pelo Traefik. Credencial do Newcon: uma sessão de login por vez (serializar por credencial).
- **`tools/teste-ip-vm/`**: testa se o Newcon aceita login a partir do IP de uma VM.

## Proteção contra lance duplicado (obrigatória no worker)

```
pendente → em_andamento → confirmacao_iniciada → confirmada (protocolo, pdf)
                 │                  │
                 ▼                  ▼
          erro_antes_confirmar   erro_apos_confirmar  (conferência manual pelo Histórico)
```

- Gravar `confirmacao_iniciada` **no banco antes** de clicar em Confirmar.
- Nunca repetir automaticamente cota que chegou a `confirmacao_iniciada`. Retomada automática só de `pendente` ou `erro_antes_confirmar`.
- Na UI, `erro_apos_confirmar` aparece destacado: "o lance pode ter sido registrado, confira no Histórico".
- Uma tarefa por vez com trava (`SELECT … FOR UPDATE SKIP LOCKED` via rota interna), renovada durante o trabalho. No SIGTERM, termina a cota atual e não começa a próxima.
- Os avisos "Consorciado já credenciado nesta assembleia…" e "Cota com Parcelas em Atraso…" só aparecem **depois** do clique em Confirmar: o dry-run não os vê.

## Etapas

- **Etapa 0 (fundação)**: gitignore, cópia fiel, compose com Traefik/Postgres/API `/health`/Next.js, imagem do worker em dry-run, Makefile. *Em validação.*
- **Etapa 1**: auth (argon2id, cookie de sessão HttpOnly/Secure/SameSite, CSRF, perfis `admin`/`operador`/`leitura`) + ForwardAuth; `usuarios`, `clientes`, `cotas`; importação do `cotasreal.csv` (lógica do `csv.js`); telas de login, clientes/cotas e importação.
- **Etapa 2**: worker em dry-run pela interface (tarefas, eventos, SSE, screenshots por cota, `NewconCotaError` com mensagem clara).
- **Etapa 3**: execução real (implementar e testar **sem confirmar**): revisão, confirmação por perfil autorizado, protocolo, `lances`, PDF no Drive e em armazenamento de objetos, auditoria. Token do Google no banco (criptografado) ou em cofre, nunca em arquivo.
- **Etapa 4**: VM dedicada, domínio, HTTPS, backup do Postgres, logs e alertas.

## Decisões em aberto (não invente a regra)

- "Cota com Parcelas em Atraso. Deseja prosseguir?": hoje aceito automaticamente. Continua, pula ou só destaca?
- Modalidade para cotas sem "2º Fixo" (grupo 6620: só Livre, Fixo, Limitado).
- Cota 6650/2068 só abre o Histórico: desativar no cadastro?
- Domínio (`app.`/`api.`) e provedor da VM de produção.
