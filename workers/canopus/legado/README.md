# Newcon Automation

Automação do fluxo **Credenciamento de Lance** no sistema Newcon (Canopus Consórcios). Para cada linha de um CSV com Grupo/Cota/Versão, o script:

1. Faz login e navega até a tela de credenciamento.
2. Filtra a cota, seleciona "2º Fixo".
3. **Modo dry-run:** salva screenshot antes de Confirmar (nada é registrado).
4. **Modo confirm:** clica em Confirmar, aceita os 2 diálogos, baixa o PDF do relatório e faz upload para uma pasta no Google Drive.

## Requisitos

- Node.js 18 ou superior
- Uma conta Google (para o Drive)
- Credenciais do Newcon

## Instalação

```bash
cd newcon-automation
npm install
npm run install-browsers    # baixa o Chromium do Playwright
cp .env.example .env         # edite com seus dados
cp cotas.example.csv cotasreal.csv   # edite com as cotas reais
```

## Configuração do Google Drive (uma única vez)

1. Acesse https://console.cloud.google.com/ e crie um projeto (ou use um existente).
2. Em **APIs e serviços → Biblioteca**, procure **Google Drive API** e clique em **Ativar**.
3. Em **APIs e serviços → Tela de consentimento OAuth** (Google Auth Platform), configure o app:
   - Tipo de usuário / público-alvo: **Externo**. Preencha o nome do app e os e-mails de contato.
   - Em **Público-alvo**, adicione como **usuário de teste** a conta Google dona da pasta do Drive.
   - Enquanto o app estiver em modo **Teste**, o Google invalida o token a cada 7 dias. Para não precisar reautorizar toda semana, clique em **Publicar app** (fica "Em produção"; o escopo usado, `drive.file`, não exige verificação do Google).
4. Em **Clientes → Criar cliente** (ou **Credenciais → Criar credenciais → ID do cliente OAuth**):
   - Tipo de aplicativo: **App para computador** (Desktop app)
   - Nome: qualquer coisa (ex: "Newcon Automation")
   - Se o client foi criado como **Aplicativo da Web**, cadastre `http://127.0.0.1:43117/oauth2callback` em **URIs de redirecionamento autorizados** e defina `DRIVE_OAUTH_PORT=43117` no `.env`. Sem isso, o Google responde `Erro 400: redirect_uri_mismatch`.
5. Copie o **ID do cliente** e a **Chave secreta do cliente** para o `.env`:
   ```
   GOOGLE_CLIENT_ID=1234567890-abc123.apps.googleusercontent.com
   GOOGLE_CLIENT_SECRET=GOCSPX-xxxxxxxxxxxx
   ```
   Alternativa: baixe o JSON do client e salve como `credentials.json` na raiz do projeto. Ele só é usado se as duas variáveis acima estiverem vazias.
6. No Google Drive, crie a pasta que vai receber os PDFs. Copie o ID da pasta da URL (`https://drive.google.com/drive/folders/AQUI_VAI_O_ID`, sem o `?hl=...` do final) e coloque em `DRIVE_FOLDER_ID` no `.env`.

Depois rode `npm run auth` (ou a primeira execução em `--confirm`): o script vai imprimir uma URL. Abra no navegador, autorize, e o token será salvo em `token.json` (as próximas execuções são silenciosas). Se aparecer o erro `invalid_grant`, apague o `token.json` e rode `npm run auth` de novo.

## Uso

**Sempre teste primeiro em dry-run:**

```bash
npm run dry-run
```

Isso vai:
- Rodar o fluxo completo até a tela de credenciamento com "2º Fixo" marcado
- Tirar um screenshot em `screenshots/` para cada cota
- **NÃO** clica em Confirmar

Abra os screenshots, confira se está tudo certo. Só quando estiver seguro:

```bash
npm run real
```

Ou, para testar apenas as primeiras N linhas do CSV:

```bash
node src/index.js --dry-run --limit 3
node src/index.js --confirm --limit 1
```

## Formato do CSV

O arquivo padrão é o `cotasreal.csv`, a planilha de clientes (veja `cotas.example.csv`):

```csv
ADMINISTRADORA,VENCIMENTO DA PARCELA,FORMA DE PAGAMENTO,DIA DA ASSEMBLEIA,CONTRATAÇÃO,VENDEDOR,NOME,GRUPO,COTA,TIPO DE CONSÓRCIO,ACESSO A COTA ,TELEFONE,E-MAIL
CANOPUS,15,BOLETO,15,,LUIZ,NOME DO CLIENTE,6650,924,IMÓVEL,,,
```

- Só estas colunas são usadas; as demais são ignoradas:
  - `GRUPO`: completado com zeros à esquerda até 6 dígitos.
  - `COTA`: completada até 4 dígitos.
  - `NOME`: usado no nome do PDF.
  - `VERSAO` (opcional): 2 dígitos, padrão `00`.
- Os cabeçalhos podem estar em qualquer caixa, com ou sem acento. O formato antigo (`grupo,cota,versao`) também funciona.
- Aceita separador `,` ou `;`.
- O PDF de cada cota é salvo como `NOME DO CLIENTE - 006650-0924-00 - AAAA-MM-DD.pdf`, em `downloads/` e no Drive. O grupo/cota fica no nome porque um cliente pode ter várias cotas.

## Logs

Cada execução gera um arquivo `logs/run-<timestamp>.jsonl` com uma linha JSON por evento. Erros também produzem screenshot em `screenshots/error_*.png`.

## Estrutura

```
newcon-automation/
├── src/
│   ├── index.js      # orquestrador
│   ├── config.js     # carrega .env e CLI args
│   ├── csv.js        # leitura do CSV
│   ├── newcon.js     # Playwright: fluxo Newcon
│   ├── drive.js      # Google Drive: OAuth + upload
│   └── logger.js
├── .env              # (você cria — não commitar)
├── credentials.json  # (opcional, alternativa a GOOGLE_CLIENT_ID/SECRET — não commitar)
├── token.json        # (gerado após primeiro OAuth — não commitar)
├── cotasreal.csv     # (você cria — planilha de clientes, não commitar)
└── ...
```

## Rodando em Docker

Use se preferir não instalar Node/Chromium localmente ou for rodar em servidor.

### Preparação

```bash
mkdir -p data
cp .env.example .env               # edita: NEWCON_*, DRIVE_FOLDER_ID, GOOGLE_CLIENT_ID/SECRET
cp cotas.example.csv data/cotasreal.csv    # edita com cotas reais
```

Estrutura da pasta `data/` (montada como volume):

```
data/
├── cotasreal.csv       # entrada
├── credentials.json    # (opcional) OAuth client baixado do Google Cloud
├── token.json          # gerado após o primeiro npm run auth
├── downloads/          # PDFs baixados
├── screenshots/        # screenshots (dry-run e erros)
└── logs/               # logs JSONL
```

### Build

```bash
docker compose build
```

### Passo 1 — Autenticar Google Drive (uma única vez)

```bash
docker compose run --rm --service-ports newcon npm run auth
```

Vai imprimir uma URL. Abra no navegador **do seu computador** (não no container), autorize a aplicação, e o token será salvo em `data/token.json`. O `--service-ports` é essencial: mapeia a porta 43117 pra o container receber o callback do OAuth.

### Passo 2 — Dry-run

```bash
docker compose run --rm newcon npm run dry-run
```

Screenshots aparecem em `data/screenshots/`.

### Passo 3 — Execução real

```bash
docker compose run --rm newcon npm run real
```

PDFs em `data/downloads/` e no Google Drive.

### Comandos úteis

```bash
# Processar só as 2 primeiras linhas
docker compose run --rm newcon node src/index.js --dry-run --limit 2

# Ver logs em tempo real (em outro terminal)
tail -f data/logs/run-*.jsonl

# Rodar em cron do host, sem prender terminal
docker compose run --rm -T newcon npm run real
```

### Notas sobre Docker

- O container roda **headless** — não dá pra ver o navegador. Se precisar debugar visualmente, rode fora do Docker com `HEADFUL=true`.
- Os UIDs dos arquivos gerados podem ficar como `pwuser`. Se causar problema, descomente a linha `user:` no `docker-compose.yml`.

## Observações importantes

- **Modo `--confirm` registra lances reais** no consórcio. Só use após validar o dry-run.
- O `applicationKey` da sessão é dinâmico — o script sempre parte da URL de login, nunca reutiliza URLs de sessões antigas.
- O PDF é baixado pelo botão de salvar (disquete) do visualizador Stimulsoft, no formato "Adobe PDF File...", e sai sem o menu do Newcon. Se esse download falhar, o script cai num fallback (`page.pdf()` do Chromium): imprime a página inteira, com o menu, e só funciona com `HEADFUL=false`.
- Timeout padrão de 30s por ação. Aumente `PLAYWRIGHT_TIMEOUT_SECONDS` no `.env` se sua rede for lenta.
