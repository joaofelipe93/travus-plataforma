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
2. **Segredos e dados de clientes nunca entram no git**: qualquer `.env` (exceto `.env.example`), `token.json`, `credentials.json`, `client_secret_*.json`, `cotasreal.csv` (nomes, telefones, e-mails), PDFs, screenshots, logs, `deploy/.env`, `deploy/data/`. Testes usam só dados fictícios. Antes de cada commit confira `git status` e o conteúdo adicionado. **O repositório no GitHub (`joaofelipe93/travus-plataforma`) é público.**
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
make usuario args='criar --email ana@exemplo.com --nome "Ana" --perfil operador'
                     # também: listar | senha --email X | desativar --email X | ativar --email X
make test            # api (vet + unitários + integração com Postgres), web (eslint + tsc), worker (sintaxe)
make smoke           # checagens pelo gateway com curl (precisa de make up; usuários smoke-*@travus.local)
make dev-web         # web com next dev (recarga automática) atrás do Traefik; make up volta ao normal
make sqlc            # regera api/internal/db depois de mudar migrações ou consultas
make paridade-csv    # regera o esperado do teste de paridade rodando o csv.js original
make worker-dry-run  # dry-run de 1 cota no container do worker (faz login no Newcon; nunca confirma)
```

Endereços locais: http://app.localhost (web e, em `/api/...`, a API), http://api.localhost (API para ferramentas; só `/health` é público), http://traefik.localhost (dashboard).

Não há cadastro público de usuários: só `make usuario`. A senha é pedida no terminal ou lida da entrada padrão (mínimo 12 caracteres).

## Arquitetura

```
                    ┌──────────────────── Traefik (gateway) ─────────────────────┐
 navegador ──HTTP(S)─►  app.<domínio>/api/*  → [ForwardAuth] → strip /api → API   │
                    │  app.<domínio>/*      → [ForwardAuth, sem sessão → /login] → web │
                    │  api.<domínio>/*      → [ForwardAuth] → API (ferramentas/agente)│
                    └───────────────────────────────┬────────────────────────────┘
                          ┌─────────────────────────┴─────┐
                          ▼                               ▼
                    Postgres  ◄── tarefas / eventos ── Worker Canopus (Node)
                                  (rede interna, sem gateway)   └─► Newcon, Google Drive
```

- **`deploy/docker-compose.yml`** (projeto `travus`). Redes: `travus_borda` (traefik, api, web) e `travus_interna` (postgres, migrate, api, cli, api-teste, worker). Só o Traefik publica porta (80). `migrate` roda `api migrate up` antes da `api`. Perfis: `cli` (usuários), `teste` (Go com Postgres, banco `travus_teste`), `canopus` (worker). `deploy/docker-compose.dev.yml` troca o web por `next dev`.
- **Traefik** (`deploy/traefik/`, montado como pasta: bind mount de arquivo único não enxerga edições). Rotas por labels (`exposedByDefault: false`) com prioridades; middlewares em `dinamico.yml`:
  - `sessao-api` / `sessao-pagina`: ForwardAuth em `http://api:8080/auth/verificar` (só repassa `Cookie`); a versão de página responde 302 para `/login?proximo=…` (`preserveLocationHeader`);
  - `remover-prefixo-api`, `limite-api` (20/s), `limite-login` (10/min por IP), `cabecalhos-seguranca`.
  - Públicos sem sessão: `app/login`, `app/_next/*`, `app/favicon.ico`, `app/robots.txt`, `app/api/auth/login`, `app/api/health`, `api/health`.
  - O Traefik tem os aliases `api.localhost`/`app.localhost` na rede `borda` (o Next.js no servidor chama a API pelo gateway).
- **API** (`api/`, Go 1.26): `net/http` com mux do Go 1.22+, `pgx/v5`, goose v3 (`goose.NewProvider`, migrações embutidas), **sqlc** (`internal/db`, gerado e commitado). Imagem distroless.
  - `internal/auth`: senha argon2id (19 MiB, t=2, p=1, no máximo 2 cálculos simultâneos), token de sessão de 256 bits (o banco guarda só o SHA-256), token CSRF.
  - `internal/httpapi`: rotas; `autenticado` valida a sessão **de novo** (não confia em cabeçalho do gateway: o worker está na mesma rede) e exige `Origin` permitida + `X-CSRF-Token` em POST/PATCH/DELETE; `exigirPerfil`. Cookie `__Host-travus_sessao` (HttpOnly, Secure, SameSite=Strict); sessão expira com 12 h sem uso ou 7 dias. Login com mensagem genérica e tempo constante. Auditoria de login, falhas, logout, importações e ativação de cotas.
  - `internal/importacao`: `LerPlanilha` porta as regras do `workers/canopus/src/csv.js` (teste de paridade contra o csv.js original em `testdata/paridade`); `Planejar` compara com o cadastro (novos, alterados, sem mudança, fora da planilha, erros); aplicar recalcula a prévia na transação e recusa se o cadastro mudou.
  - `internal/testedb`: testes de integração (pulados sem `TEST_DATABASE_URL`).
  - `cmd/api`: `serve`, `migrate up|down|status`, `usuario …`, `healthcheck`.
- **Web** (`web/`, Next.js 16, App Router, TypeScript, Tailwind 4, shadcn/ui estilo base-nova (Base UI, não Radix: use `render` em vez de `asChild`), TanStack Query, `output: "standalone"`). **O Next 16 tem mudanças incompatíveis: leia `web/AGENTS.md` e o guia relevante em `web/node_modules/next/dist/docs/` antes de escrever código** (ex.: `middleware` virou `proxy`; `useSearchParams` precisa de `<Suspense>`; env de runtime com `await connection()`).
  - O navegador chama a API em `/api/...` (mesma origem). `src/lib/api.ts` manda o `X-CSRF-Token`, e em 401 recarrega para o login.
  - `(app)/layout.tsx` busca `/auth/sessao` antes de mostrar as telas; botões aparecem conforme o perfil, mas quem decide é a API.
  - Telas: `/login`, `/cotas`, `/clientes`, `/clientes/[id]`, `/importar` (prévia → aplicar/descartar, histórico), `/status`.
- **Worker Canopus** (`workers/canopus/`, Node + Playwright **1.63.0 exato**; a imagem `mcr.microsoft.com/playwright:v1.63.0-noble` precisa ter a mesma versão do `package-lock.json`). Por enquanto é o script legado (`src/index.js`, `src/newcon.js`); `legado/` é só referência. No container, `docker-entrypoint.sh` bloqueia `--confirm`/`real`, e o Chromium precisa de `shm_size` (ou `--ipc=host`). Na Etapa 2 vira worker de tarefas que fala com a API por rotas `/internal/...` com token de serviço, só na rede interna, nunca pelo Traefik. Credencial do Newcon: uma sessão de login por vez (serializar por credencial).
- **`tools/`**: `teste-ip-vm/` (Newcon aceita login do IP de uma VM?), `paridade-csv/` (esperado do teste de paridade), `smoke/etapa1.sh`.

## Perfis

| Perfil | Pode |
|---|---|
| `leitura` | ver clientes, cotas e importações |
| `operador` | + importar planilha, ativar/desativar cota |
| `admin` | tudo do operador (e, nas próximas etapas, o que for restrito) |

## Regras da importação

- Cliente = nome normalizado (maiúsculas, espaços únicos); a planilha não tem CPF.
- Reimportar cria e atualiza. Cota que sumiu da planilha **não** é desativada, só aparece na prévia.
- Telefone/e-mail vazios na planilha não apagam o cadastro.
- Colunas sem uso ficam em `cotas.dados_planilha`; "ACESSO A COTA" é sempre descartada.
- Modalidade padrão: `segundo_fixo`. Grupo/cota/versão com zeros à esquerda (6/4/2), versão padrão `00`.

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

- **Etapa 0 (fundação)**: validada.
- **Etapa 1 (login e cadastro)**: auth + ForwardAuth + perfis, clientes/cotas, importação com prévia, telas. *Em validação.*
- **Etapa 2**: worker em dry-run pela interface (tarefas, eventos, SSE, screenshots por cota, `NewconCotaError` com mensagem clara).
- **Etapa 3**: execução real (implementar e testar **sem confirmar**): revisão, confirmação por perfil autorizado, protocolo, `lances`, PDF no Drive e em armazenamento de objetos, auditoria. Token do Google no banco (criptografado) ou em cofre, nunca em arquivo.
- **Etapa 4**: VM dedicada, domínio, HTTPS, backup do Postgres, logs e alertas.

## Decisões em aberto (não invente a regra)

- "Cota com Parcelas em Atraso. Deseja prosseguir?": hoje aceito automaticamente. Continua, pula ou só destaca?
- Modalidade para cotas sem "2º Fixo" (grupo 6620: só Livre, Fixo, Limitado).
- Domínio (`app.`/`api.`) e provedor da VM de produção.
- Quais perfis podem confirmar lance real (Etapa 3).
