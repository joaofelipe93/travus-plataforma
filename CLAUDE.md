# CLAUDE.md

Guia para o Claude Code (e para quem mais trabalhar aqui). Interface, mensagens, logs e commits em **português**.

## O que é

**Travus Plataforma**: sistema web com gateway (Traefik) ao qual vamos adicionando serviços. O primeiro é a automação de **Credenciamento de Lance** no Newcon (Canopus Consórcios), portada do script de terminal validado em produção em `/home/joao/Documentos/newcon-automation`, que continua sendo a ferramenta em uso até a plataforma substituí-lo.

Tudo o que se sabe do Newcon (seletores, fluxos, armadilhas) está em **`docs/canopus-newcon.md`. Leia inteiro antes de mexer no worker.**

## Regras inegociáveis

1. **Nunca registre lance real.** Não rode `--confirm`, `npm run real` nem nada que clique em "Confirmar" (`#ctl00_Conteudo_btnConfirma`) no Newcon, nem para testar. O Newcon **aceita lances repetidos** na mesma assembleia: um teste errado vira lance duplicado de verdade. Para testar:
   - **dry-run pela plataforma** (tela Execuções → Novo dry-run) ou `make worker-dry-run` (script legado, 1 cota). Vai até a tela de credenciamento, marca "2º Fixo", tira screenshot e não confirma. **Criar um dry-run faz o worker entrar no Newcon**: testes automáticos (`make test`, `make smoke`) nunca criam execução;
   - **reimpressão pelo Histórico** para testar o PDF: na tela de credenciamento, `#ctl00_Conteudo_btnHistorico` → `input[id$="srcPrint"]` de uma linha.

   Execução real só com pedido explícito do usuário, na hora.
2. **Segredos e dados de clientes nunca entram no git**: qualquer `.env` (exceto `.env.example`), `token.json`, `credentials.json`, `client_secret_*.json`, `cotasreal.csv` (nomes, telefones, e-mails), PDFs, screenshots, logs, `deploy/.env`, `deploy/data/`. Testes usam só dados fictícios. Antes de cada commit confira `git status` e o conteúdo adicionado. **O repositório no GitHub (`joaofelipe93/travus-plataforma`) é público.**
3. **Não mexa na VM `appairbnb`.** Ela roda outra aplicação em produção e não faz parte desta plataforma. Nada da plataforma roda nela.
4. **O worker nunca repete automaticamente a etapa de Confirmar** (ver "Proteção contra lance duplicado").
5. **Não altere nada em `/home/joao/Documentos/newcon-automation`.** Só copie de lá.
6. Se algo do Newcon se comportar diferente de `docs/canopus-newcon.md`, **pare e avise** antes de mudar a lógica.
7. Sem push, repositório remoto novo ou deploy sem pedido explícito. Trabalhe por etapas: proponha o plano, espere o ok e pare no fim de cada etapa para validação. Commits pequenos e descritivos.
8. Uma sessão de login no Newcon por vez: nunca rode dry-run da plataforma, `make worker-dry-run` e scripts de descoberta ao mesmo tempo.

## Comandos

```bash
make up              # sobe traefik, postgres, migrate, api, web e worker (cria/completa deploy/.env)
make down            # derruba os containers (o banco fica no volume postgres-dados)
make logs s=worker-canopus   # logs (sem s=, de todos)
make ps              # estado dos containers
make migrate         # aplica migrações pendentes
make usuario args='criar --email ana@exemplo.com --nome "Ana" --perfil operador'
                     # também: listar | senha --email X | desativar --email X | ativar --email X
make test            # api (vet + unitários + integração com Postgres), web (eslint + tsc), worker (node:test, sem Newcon)
make smoke           # checagens pelo gateway com curl (precisa de make up; não cria execução)
make dev-web         # web com next dev (recarga automática) atrás do Traefik; make up volta ao normal
make sqlc            # regera api/internal/db depois de mudar migrações ou consultas
make paridade-csv    # regera o esperado do teste de paridade rodando o csv.js original
make worker-dry-run  # script legado: dry-run de 1 cota da planilha no container (faz login; nunca confirma)
```

Endereços locais: http://app.localhost (web e, em `/api/...`, a API), http://api.localhost (API para ferramentas; só `/health` é público), http://traefik.localhost (dashboard).

Não há cadastro público de usuários: só `make usuario`. A senha é pedida no terminal ou lida da entrada padrão (mínimo 12 caracteres). `deploy/.env` guarda a senha do Postgres e o `WORKER_TOKEN` (gerados pelo `make`).

## Arquitetura

```
                    ┌──────────────────── Traefik (gateway) ─────────────────────┐
 navegador ──HTTP(S)─►  app.<domínio>/api/*  → [ForwardAuth] → strip /api → API:8080│
                    │  app.<domínio>/*      → [ForwardAuth, sem sessão → /login] → web │
                    │  api.<domínio>/*      → [ForwardAuth] → API:8080 (ferramentas)  │
                    └───────────────────────────────┬────────────────────────────┘
                          ┌─────────────────────────┴──────┐
                          ▼                                ▼
                    Postgres  ◄── API:8081 (/internal, token) ◄── Worker Canopus (Node)
                  (LISTEN/NOTIFY → SSE)     rede interna, sem gateway   └─► Newcon
```

- **`deploy/docker-compose.yml`** (projeto `travus`). Redes: `travus_borda` (traefik, api, web) e `travus_interna` (postgres, migrate, api, worker, cli, api-teste). Só o Traefik publica porta (80). `migrate` roda `api migrate up` antes da `api`. Perfis: `cli` (usuários), `teste` (Go com Postgres, banco `travus_teste`), `canopus` (script legado `worker-legado`). `deploy/docker-compose.dev.yml` troca o web por `next dev`.
- **Traefik** (`deploy/traefik/`, montado como pasta: bind mount de arquivo único não enxerga edições). Rotas por labels (`exposedByDefault: false`) com prioridades; middlewares em `dinamico.yml`:
  - `sessao-api` / `sessao-pagina`: ForwardAuth em `http://api:8080/auth/verificar` (só repassa `Cookie`, `trustForwardHeader: false`); a versão de página responde 302 para `/login?proximo=…` (`preserveLocationHeader`);
  - `remover-prefixo-api`, `limite-api` (20/s), `limite-login` (10/min por IP), `cabecalhos-seguranca`.
  - Públicos sem sessão: `app/login`, `app/_next/*`, `app/favicon.ico`, `app/robots.txt`, `app/api/auth/login`, `app/api/health`, `api/health`.
  - O Traefik tem os aliases `api.localhost`/`app.localhost` na rede `borda` (o Next.js no servidor chama a API pelo gateway). Ele leva alguns segundos para ligar rotas de um container recém-saudável (o `make up` espera).
- **API** (`api/`, Go 1.26): `net/http` com mux do Go 1.22+, `pgx/v5`, goose v3 (`goose.NewProvider`, migrações embutidas), **sqlc** (`internal/db`, gerado e commitado). Imagem distroless. Duas portas: **8080** pública (Traefik) e **8081** interna (worker, token `WORKER_TOKEN`, sem rota no Traefik).
  - `internal/auth`: senha argon2id (19 MiB, t=2, p=1, no máximo 2 cálculos simultâneos), token de sessão de 256 bits (o banco guarda só o SHA-256), token CSRF.
  - `internal/httpapi`: rotas; `autenticado` valida a sessão **de novo** (não confia em cabeçalho do gateway) e exige `Origin` permitida + `X-CSRF-Token` em POST/PATCH/DELETE; `exigirPerfil`. Cookie `__Host-travus_sessao` (HttpOnly, Secure, SameSite=Strict); sessão expira com 12 h sem uso ou 7 dias. Auditoria de login, falhas, logout, importações, ativação de cotas e execuções (criação e cancelamento).
    - `execucoes.go`: criar dry-run (só cotas ativas; `real` é recusado), listar, detalhar, cancelar, baixar screenshot (`/arquivos/{id}`, só com sessão).
    - `interno.go`: fila do worker (`/internal/tarefas/proxima` com `FOR UPDATE SKIP LOCKED` e trava de 2 min, renovar, iniciar/concluir cota, screenshot, eventos, finalizar, liberar no SIGTERM). Trava vencida devolve a execução à fila.
    - `sse.go`: `GET /execucoes/{id}/eventos` (SSE, `Last-Event-ID`, `event: fim`); `Hub` com `LISTEN execucao_eventos` (trigger no insert).
  - `internal/importacao`: `LerPlanilha` porta as regras do `workers/canopus/src/csv.js` (teste de paridade contra o csv.js original em `testdata/paridade`); `Planejar` compara com o cadastro; aplicar recalcula a prévia na transação e recusa se o cadastro mudou.
  - `internal/testedb`: testes de integração (pulados sem `TEST_DATABASE_URL`).
  - `cmd/api`: `serve`, `migrate up|down|status`, `usuario …`, `healthcheck`. Screenshots vencidos (30 dias) são apagados de hora em hora.
- **Web** (`web/`, Next.js 16, App Router, TypeScript, Tailwind 4, shadcn/ui estilo base-nova (Base UI, não Radix: use `render` em vez de `asChild`), TanStack Query, `output: "standalone"`). **O Next 16 tem mudanças incompatíveis: leia `web/AGENTS.md` e o guia relevante em `web/node_modules/next/dist/docs/` antes de escrever código** (ex.: `middleware` virou `proxy`; `useSearchParams` precisa de `<Suspense>`; env de runtime com `await connection()`).
  - O navegador chama a API em `/api/...` (mesma origem). `src/lib/api.ts` manda o `X-CSRF-Token`, e em 401 recarrega para o login.
  - `(app)/layout.tsx` busca `/auth/sessao` antes de mostrar as telas; botões aparecem conforme o perfil, mas quem decide é a API.
  - Telas: `/login`, `/execucoes` (lista), `/execucoes/nova` (escolher cotas ativas), `/execucoes/[id]` (progresso por `EventSource`, cotas, lances já existentes na assembleia, screenshots, log, cancelar), `/cotas`, `/clientes`, `/clientes/[id]`, `/importar`, `/status`.
- **Worker Canopus** (`workers/canopus/`, Node + Playwright **1.63.0 exato**; a imagem `mcr.microsoft.com/playwright:v1.63.0-noble` precisa ter a mesma versão do `package-lock.json`).
  - `src/worker.js`: laço da fila. **Só dry-run: não há caminho para Confirmar** (um teste lê o arquivo e falha se aparecer `confirmAndWaitReport`, `downloadReportPdf` ou `btnConfirma`). Por cota: `backToFilter` (a partir da 2ª) → `searchCota` → `lerDadosCredenciamento` → `selectSegundoFixo` → `captureBeforeConfirm` → envia screenshot → `lerHistorico` (só leitura, depois do screenshot) → conclui. `NewconCotaError` → erro conhecido; outro erro → inesperado; ambos com screenshot. SIGTERM: termina a cota atual e devolve à fila (`stop_grace_period: 90s`).
  - `src/newcon.js`, `csv.js`, `logger.js`: o código validado, **sem mudanças**. `src/index.js` é o script legado (`make worker-dry-run`).
  - `src/leitura-credenciamento.js`: seletores de assembleia e do Histórico (só leitura). `src/plataforma.js`: cliente das rotas internas. `src/registro.js`: logger que manda eventos à API. `src/config-worker.js`: só variáveis de ambiente (recusa `NEWCON_URL` com a grafia `frmCorCCCnsLogin`).
  - No container, `docker-entrypoint.sh` bloqueia `--confirm`/`real`, e o Chromium precisa de `shm_size` (ou `--ipc=host`). `legado/` é só referência.
- **`tools/`**: `teste-ip-vm/` (Newcon aceita login do IP de uma VM?), `paridade-csv/` (esperado do teste de paridade), `smoke/etapa1.sh` (gateway, sessão, perfis e rotas de execução, sem criar execução).

## Perfis

| Perfil | Pode |
|---|---|
| `leitura` | ver clientes, cotas, importações e execuções |
| `operador` | + importar planilha, ativar/desativar cota, criar e cancelar dry-run |
| `admin` | tudo do operador (e, nas próximas etapas, o que for restrito) |

## Regras da importação

- Cliente = nome normalizado (maiúsculas, espaços únicos); a planilha não tem CPF.
- Reimportar cria e atualiza. Cota que sumiu da planilha **não** é desativada, só aparece na prévia.
- Telefone/e-mail vazios na planilha não apagam o cadastro.
- Colunas sem uso ficam em `cotas.dados_planilha`; "ACESSO A COTA" é sempre descartada.
- Modalidade padrão: `segundo_fixo`. Grupo/cota/versão com zeros à esquerda (6/4/2), versão padrão `00`.

## Execuções e proteção contra lance duplicado

```
execução:  na_fila → em_andamento → concluida | concluida_com_erros | cancelada | falhou
             ▲            │ (trava vencida ou SIGTERM)
             └────────────┘

cota:  pendente → em_andamento → verificada (dry-run ok)
                      │       └→ confirmacao_iniciada → confirmada        (Etapa 3)
                      ▼                     │
             erro_antes_confirmar     erro_apos_confirmar  (conferência manual pelo Histórico)
       (pendente → cancelada quando a execução é cancelada)
```

- Uma execução em andamento por credencial do Newcon (índice único parcial em `execucoes`); o worker pega uma por vez com `FOR UPDATE SKIP LOCKED` e renova a trava a cada 30 s.
- Cancelar: na fila, cancela na hora; em andamento, o worker termina a cota atual e não começa a próxima.
- Worker que parou (trava vencida): a execução volta à fila; cota `em_andamento` volta a `pendente`; cota `confirmacao_iniciada` vira `erro_apos_confirmar` ("o lance pode ter sido registrado, confira no Histórico").
- **Trigger no banco** (`execucao_cotas_proteger_confirmacao`): cota em `confirmacao_iniciada`, `confirmada` ou `erro_apos_confirmar` nunca volta para um estado que permita reprocessá-la. `iniciar` só aceita `pendente` ou `erro_antes_confirmar`.
- Etapa 3: gravar `confirmacao_iniciada` **no banco antes** de clicar em Confirmar; a API hoje recusa execuções `real` e conclusões que não sejam `verificada`/`erro_antes_confirmar`.
- Os avisos "Consorciado já credenciado nesta assembleia…" e "Cota com Parcelas em Atraso…" só aparecem **depois** do clique em Confirmar: o dry-run não os vê. O dry-run lê o Histórico e mostra quantos lances a cota já tem na assembleia atual.

## Etapas

- **Etapa 0 (fundação)**: validada.
- **Etapa 1 (login e cadastro)**: validada.
- **Etapa 2 (dry-run pela interface)**: fila no Postgres, rotas internas, SSE, worker, telas de execução, leitura de assembleia e Histórico. *Em validação.*
- **Etapa 3**: execução real (implementar e testar **sem confirmar**): revisão, confirmação por perfil autorizado, protocolo, `lances`, PDF no Drive e em armazenamento de objetos, auditoria. Token do Google no banco (criptografado) ou em cofre, nunca em arquivo.
- **Etapa 4**: VM dedicada, domínio, HTTPS, backup do Postgres, logs e alertas.

## Decisões em aberto (não invente a regra)

- "Cota com Parcelas em Atraso. Deseja prosseguir?": hoje aceito automaticamente. Continua, pula ou só destaca?
- Modalidade para cotas sem "2º Fixo" (grupo 6620: só Livre, Fixo, Limitado).
- O que fazer com cota que já tem lance na assembleia (o dry-run mostra; a regra para a execução real é do usuário).
- Domínio (`app.`/`api.`) e provedor da VM de produção.
- Quais perfis podem confirmar lance real (Etapa 3).
