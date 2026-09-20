# Como contribuir (pessoas e agentes)

Vários agentes trabalham ao mesmo tempo nesta plataforma. Este guia é o fluxo que mantém tudo
encaixado e seguro. As regras do negócio e da arquitetura estão no [`CLAUDE.md`](CLAUDE.md): leia
antes, principalmente as **regras inegociáveis**.

## O fluxo

```
make worktree b=tipo/assunto → código + testes → make test e make verificar → PR → CI verde → merge (squash)
                                                                                      ↓
                              deploy aprovado pelo usuário ← release ← merge do PR de versão (usuário)
```

1. **Uma área de trabalho por tarefa.** Na pasta principal:
   ```bash
   make worktree b=feat/reservas-historico     # cria ../travus-feat-reservas-historico a partir da origin/main
   ```
   O worktree já vem com o `deploy/.env` da pasta principal, as dependências da web e do worker e
   **credenciais fictícias do Newcon** (um agente nunca entra no Newcon por engano).
2. **Nome do branch**: `tipo/assunto`, com o mesmo tipo do título do PR (abaixo).
3. **Antes de abrir o PR**: `make test` e `make verificar`. Se mexeu na tela, confira no navegador
   (`make up`, http://app.localhost).
4. **PR para a `main`**, com o modelo preenchido. A `main` é protegida: sem push direto, sem force
   push, sem exceção para admin. O merge só libera com **todos os jobs da CI verdes** e o branch
   **atualizado com a `main`** (`gh pr update-branch <número>` ou merge da `main` no branch).
5. **Merge por squash**: o título do PR vira o commit na `main`.

## Título do PR (obrigatório, a CI confere)

`tipo(escopo)!: descrição`, em português, no imperativo ou descritivo, sem ponto final.

| Tipo | Quando | Versão |
|---|---|---|
| `feat` | funcionalidade nova para quem usa | minor (0.2.0 → 0.3.0) |
| `fix` | correção de algo que quebrava | patch (0.2.0 → 0.2.1) |
| `perf` | desempenho | patch |
| `revert` | desfaz um PR | patch |
| `docs`, `ci`, `build`, `refactor`, `test`, `chore`, `style` | o resto | não gera versão |

Escopos em uso: `api`, `web`, `worker`, `checkin-whatsapp`, `deploy`. `!` depois do escopo marca
mudança incompatível. Exemplos: `feat(reservas): histórico de mensagens`,
`fix(api): sessão expirava antes do prazo`.

## Trabalhando em paralelo na mesma máquina

- **A stack local é uma só** (projeto Docker `travus`, porta 80, um volume do Postgres). `make up`
  num worktree troca os containers pelos daquele código. Combine antes de subir a stack, e prefira
  a CI (job Integração) para validar o que não é tela.
- **`make test` e `make smoke` rodam um de cada vez** na máquina (trava comum a todos os
  worktrees): se outro estiver rodando, o seu espera e avisa. Não rode os testes da API fora do
  `make test`: eles apagam o banco `travus_teste`.
- **Migrações**: o número da migração nova é o próximo livre na `main`. Se outro PR usar o mesmo
  número primeiro, a CI do seu PR reprova: atualize com a `main` e renumere. Migração já existente
  nunca se edita, e o Up não apaga nem renomeia (ver "Migrações seguras" no `CLAUDE.md`).
- **`api/internal/db` é gerado** (`make sqlc`). Em conflito, resolva as migrações e consultas e
  gere de novo; não resolva o código gerado à mão.
- **`version.txt` e `CHANGELOG.md` são do release-please.** Não edite.
- **Versões amarradas**: Node e Go aparecem nos Dockerfiles e na CI, e o Playwright aparece na imagem e no `package-lock` do worker Canopus. Mudou num lugar, mude em todos (`make verificar` confere). PR do Dependabot que quebrar isso reprova: ajuste o outro lado no mesmo PR.
- Remova o worktree quando o PR entrar: `git worktree remove ../travus-<tipo>-<assunto>`.

## O que um agente nunca faz

- Nada que registre **lance real** ou clique em "Confirmar" no Newcon; nenhum dry-run sem pedido
  do usuário; nunca duas sessões do Newcon ao mesmo tempo.
- Nada que mande mensagem ao **grupo real do WhatsApp** ou pareie o número da produção.
- Nenhum **segredo ou dado de cliente/hóspede** no git (o repositório é público): `.env`,
  tokens, planilha real, PDFs, screenshots, dumps, sessão do WhatsApp.
- **Não faz merge do PR de versão, não aprova deploy e não roda "Voltar versão"**: são decisões do
  usuário. Não liga `LANCE_REAL_HABILITADO`.
- Não contorna a proteção da `main` nem da CI (nada de `--admin`, `--no-verify`, desligar job).
- Não mexe na VM `appairbnb` nem em `~/Documentos/airbnb` e `~/Documentos/newcon-automation`.

## Deploy e rollback (só o usuário)

Ver `CLAUDE.md` ("Versões" e "Deploy (CD)") e `docs/producao.md` (seção 10): merge do PR
"chore: versão X.Y.Z" → tag e release → workflow Deploy compila as imagens → **aprovação** no
ambiente `producao` → publica, faz smoke e volta sozinho se falhar. Rollback manual: workflow
**Voltar versão**.
