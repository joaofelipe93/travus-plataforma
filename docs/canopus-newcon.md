# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Node.js (CommonJS, Node ≥18) Playwright script that automates the **Credenciamento de Lance** flow in Newcon (Canopus Consórcios, an ASP.NET Web Forms app). For each row of a CSV (`grupo,cota,versao`) it logs in, filters the cota, selects "2º Fixo", and then either screenshots the screen (dry-run) or confirms the bid, downloads the Stimulsoft PDF report, and uploads it to Google Drive. Code comments, log messages, and user-facing errors are in Portuguese — keep it that way.

**`--confirm` registers real financial bids.** Never run `npm run real` / `--confirm` to test a change; use `--dry-run` (optionally with `--limit N`). No `npm run real` run should happen without the user asking for it explicitly.

## Commands

There is no build, lint, or test suite.

```bash
npm install && npm run install-browsers   # deps + Playwright Chromium
npm run dry-run                            # node src/index.js --dry-run
npm run real                               # node src/index.js --confirm  (REAL bids)
npm run auth                               # Google Drive OAuth only → writes token.json
node src/index.js --dry-run --limit 1      # smoke-test against the first CSV row
HEADFUL=true node src/index.js --dry-run --limit 1   # watch the browser
```

Docker (headless only; `./data` is mounted at `/app/data` and holds `cotasreal.csv`, `token.json`, and all outputs):

```bash
docker compose build
docker compose run --rm --service-ports newcon npm run auth   # --service-ports needed for OAuth callback on 43117
docker compose run --rm newcon npm run dry-run
```

The Dockerfile base image is pinned to `mcr.microsoft.com/playwright:v1.47.0-jammy`; if the `playwright` npm version is bumped, bump the image tag to match or the bundled browsers won't line up.

## Architecture

`src/index.js` orchestrates: load config → read CSV → (confirm mode only) init `DriveClient` → launch `NewconClient` → login once → loop cotas. Each cota is wrapped in its own try/catch, so one failure logs an error + `screenshots/error_*.png` and moves on. Exit codes: `0` all ok, `1` some cotas failed, `2` fatal (e.g. login/navigation failure).

- **`config.js` does its work at `require` time**: loads `.env`, parses CLI args with yargs (`.strict()`, `--dry-run` and `--confirm` are mutually exclusive and one is required), validates `NEWCON_URL/USER/PASS`, creates output dirs, and throws if the CSV doesn't exist or `DRIVE_FOLDER_ID` is missing in confirm mode. Relative paths are resolved against the project root, not the cwd.
- **`auth.js`** pushes `--dry-run` onto `process.argv` before requiring config to get past the mode check — so `npm run auth` still requires the Newcon env vars and an existing CSV.
- **`newcon.js` (`NewconClient`)** holds all page interaction. Selectors are hardcoded ASP.NET control IDs (`#ctl00_Conteudo_...`, TreeView node `#ctl00_Conteudo_ctl00_tvwMenut3`), and navigation is confirmed via `waitForURL` on `.aspx` page names (`frmMain`, `frmMainMenu`, `frmConCpCadCredenciamentoLance_Filtro`, `frmConCpCadCredenciamentoLance`, `frmConCmNewconReports`). A page-level `dialog` listener registered in `start()` auto-accepts **every** alert/confirm — this is how the two confirmation popups after "Confirmar" get through. This is intentional, including the "Consorciado já credenciado nesta assembleia… Deseja continuar?" confirm for a cota that already has bids in that assembly: the user wants the bid registered anyway, so don't add logic to dismiss or skip it. Always start from the login URL: the session's `applicationKey` is dynamic, so deep URLs from old sessions don't work. In the top menu, "Contemplação" is an `<a>` with no `href` (so `getByRole('link')` can't find it) and its submenu is hover-only; `_openPreContemplacao()` instead reads the `href` of the hidden "Pré-Contemplação" item (`frmMainMenu.aspx?ID_Modulo=CP&ID_SubGrupo=PC…`), caches the absolute URL, and `goto`s it. Both `goToCredenciamento()` and `backToFilter()` go through it, then click the TreeView node `#ctl00_Conteudo_ctl00_tvwMenut3` ("Credenciamento").

`NEWCON_URL` must be `…/WWW/frmCorCcCnsLogin.aspx` with exactly that casing. The `frmCorCCCnsLogin.aspx` spelling, which the server's own root redirect uses, gets its TCP connection reset right after the TLS handshake, for browsers and curl alike.
- **Known per-cota failures** throw `NewconCotaError` (exported from `newcon.js`), whose message is written for the user:
  - `searchCota()` waits for the main-frame navigation after "Localizar" and checks where it landed. It continues on `frmConCpCadCredenciamentoLance.aspx`. It throws when Newcon opens `frmConCpCnsHistoricoOfertaLance.aspx` (history only, no credenciamento available; the message includes the last assembly with a bid). It also throws when the page stays on the `_Filtro` page (e.g. a nonexistent versão). In that case it includes Newcon's red validation text, such as "Consorciado inválido.", read by `_redMessage()`: there is no known element id, so it takes the visible red leaf text.
  - `selectSegundoFixo()` throws when `#ctl00_Conteudo_rgLance_2` is disabled (e.g. group 6620 has no "2º Lance Fixo") and lists the enabled modalities.
  - Each of these fails in about a second, instead of hitting the 30 s Playwright timeout.
  - `index.js` logs them without a stack.
  - The final summary lists every failed cota with its reason. `result.confirmAttempted` is set right before `confirmAndWaitReport()`, so a failure after that point is flagged as "the bid may have been registered", not "not registered".
- **PDF download**: the report page (`CONCM/frmConCmNewconReports.aspx`) uses the classic ASP.NET Stimulsoft WebViewer, whose toolbar is in the **main page**, not in the `webScrollFrame_…StiWebRelatorio` iframe. `downloadReportPdf()` selects "Adobe PDF File..." in `select[id$="StiWebRelatorio_SaveTypeList"]` and clicks `input[id$="StiWebRelatorio_Save"]`. The server answers with a `Content-Disposition: attachment` `Report.pdf` (no settings dialog), which is caught as a Playwright `download` and checked for a `%PDF-` header. If any of that fails, it falls back to `page.pdf()`: the whole page with the Newcon menu, and it only works headless.
- **Testing the report/PDF step without placing a bid**: on the credenciamento screen (after `searchCota`, without selecting a lance type), `#ctl00_Conteudo_btnHistorico` opens a grid (`table[id*="grdHistLances"]`) of past offers. Each row's `input[id$="srcPrint"]` reprints that protocol's receipt on the same `frmConCmNewconReports.aspx` viewer. Use that to exercise `downloadReportPdf()` instead of running `--confirm`.
- **`drive.js` (`DriveClient`)** uses a Desktop-app OAuth client (scope `drive.file`) whose keys come from `GOOGLE_CLIENT_ID` + `GOOGLE_CLIENT_SECRET` in `.env`, falling back to `credentials.json` only when both are empty. With no `token.json`, it spins up a local HTTP server (random port, or `DRIVE_OAUTH_PORT`/`BIND_HOST` in Docker), prints the auth URL, and saves the token; refreshed tokens are merged back into `token.json`.
- **`csv.js`**: the default input is `cotasreal.csv`, the client spreadsheet (`ADMINISTRADORA,…,NOME,GRUPO,COTA,TIPO DE CONSÓRCIO,ACESSO A COTA ,TELEFONE,E-MAIL`). Headers are normalized (accents stripped, trimmed, lowercased), and only `grupo`, `cota`, `nome` and the optional `versao` are used, so the legacy `grupo,cota,versao` format still parses. It auto-detects `;` vs `,`, strips non-digits, and zero-pads grupo/cota/versao to 6/4/2 digits. versao defaults to `00`: the spreadsheet has no versão column, and `00` is the value confirmed to work in Newcon.
- **PDF file names** come from `reportFileName()` in `newcon.js`: `NOME DO CLIENTE - 006650-0924-00 - AAAA-MM-DD.pdf`, using the local date. Grupo/cota stay in the name because one client can have several cotas. Without a `nome` it falls back to `credenciamento_<tag>_<date>.pdf`. Drive uploads reuse the local basename.
- **`logger.js`** writes one JSONL file per run to `logs/run-<timestamp>.jsonl` and mirrors a compact line to stdout.

## Descoberto na plataforma (dry-run de 2026-09-15, só leitura)

Usado por `workers/canopus/src/leitura-credenciamento.js`. Nada disso preenche campo, marca modalidade ou confirma.

- **Tela de credenciamento** (depois de `searchCota`):
  - `#ctl00_Conteudo_edtDT_Assembleia`: data da assembleia (ex.: `15/09/2026`). É um `<input>` **editável**: só ler o valor, nunca preencher.
  - `#ctl00_Conteudo_lblNO_Assembleia`: número da assembleia (ex.: `027`).
  - `#ctl00_Conteudo_lblVA_Lance_Fixo_2`: percentual do 2º Lance Fixo (ex.: `30.0000`). Vizinhos: `lblVA_Lance_Fixo`, `lblVA_Lance_Minimo`, `lblVA_Lance_Maximo`, `lblVA_Lance_Limitado`.
  - `#ctl00_Conteudo_lblNM_Ocorrencia`: texto vermelho "Último lance ofertado em: 13/09/2026 às 19:52:31, através da(o) Web". **Não diz de qual assembleia** é o lance.
  - Radios da modalidade: `rgLance_0` (LL, Livre), `rgLance_1` (LF, Fixo), `rgLance_2` (LS, 2º Fixo), `rgLance_3` (LM, Limitado), `rgLance_4` (LI, Fidelidade; desabilitado na 6650/0236).
- **Histórico** (`#ctl00_Conteudo_btnHistorico`, submit): abre um **painel na mesma página** (a URL continua `frmConCpCadCredenciamentoLance.aspx`), fechado por `#ctl00_Conteudo_lbkFechaHistorico`. Grade `#ctl00_Conteudo_ucHistoricoOfertaLance_grdHistLances`, colunas: Protocolo, Assembleia, Credenciamento (data e hora), Modalidade ("2º Lance Fixo"), Acesso, Tipo Oferta, Usuário (login do Newcon: não guardar), Vl. Lance, % Lance, Pc. Lance, % Embutido, Vl Embutido, Lance Automático, Vl. FGTS, Tipo Redução, Construtora, Troca de Chaves, Sequência, 2ª Via. É pela coluna **Assembleia** que se sabe se a cota já tem lance na assembleia atual (a 6650/0236 tinha 2 em 15/09/2026).
- **Grupo 6620** (6620/1372): a tela abre, a assembleia é lida (nº 010) e o "2º Fixo" vem desabilitado; modalidades habilitadas: Livre, Fixo, Limitado.
- **6650/2068**: "Localizar" leva ao Histórico das Ofertas (sem credenciamento; último lance na assembleia de 16/06/2026).

Secrets and runtime files (`.env`, `credentials.json`, `token.json`, `client_secret_*.json`, `cotasreal.csv` (client names, phones and e-mails), and the contents of `downloads/`, `screenshots/`, `logs/`) are in `.gitignore`; see `.env.example` for all config variables. The Docker `data/` folder holds the same secrets but is only in `.dockerignore`, not `.gitignore`.
