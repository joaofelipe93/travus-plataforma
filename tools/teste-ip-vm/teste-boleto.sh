#!/usr/bin/env bash
# Teste: o Boleto Fácil da Canopus aceita acesso a partir do IP desta máquina?
#
# Uso (copie a pasta teste-ip-vm inteira para a VM):
#   bash teste-boleto.sh                # só curl (não precisa de Docker)
#   bash teste-boleto.sh --navegador    # também num Chromium de verdade (precisa de Docker)
#
# SÓ LEITURA. Carrega a página pública, baixa o JavaScript do Boleto Fácil e consulta
# /find-cota com um CPF FICTÍCIO (dígito verificador certo, sem cotas na Canopus).
# NÃO chama generate-bill nem imprime boleto: nenhuma cota de cliente é tocada e nada
# é registrado no site.
#
# O site está atrás de Cloudflare, e o que ela recusa não é óbvio. Medido na máquina do
# escritório em 16/10/2026: `curl` padrão passa, `curl` com cabeçalho de navegador passa,
# Chromium headless do Playwright (que se anuncia "HeadlessChrome") toma 403, e o mesmo
# Chromium com User-Agent de Chrome normal passa. Na VM, `curl` padrão tomou 403.
# Por isso o teste mede as quatro combinações e só então conclui: trocar cabeçalho é
# barato, trocar o IP de saída é arquitetura.

set -uo pipefail

BASE="${BOLETO_BASE:-https://www.consorciocanopus.com.br/extensions/boleto-facil-rolledback/public/index.php}"
SITE="${BOLETO_SITE:-https://www.consorciocanopus.com.br}"
# CPF fictício com dígito verificador válido: o site responde {"cotas":[]}.
CPF_FICTICIO="${CPF_FICTICIO:-11122233396}"
UA_NAVEGADOR="${UA_NAVEGADOR:-Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36}"
# Mesma versão do worker do Canopus (workers/canopus/package.json).
PW_VERSION="${PW_VERSION:-1.63.0}"
DIR="$(cd "$(dirname "$0")" && pwd)"

ok()    { printf '  \033[32m✓\033[0m %s\n' "$*"; }
falha() { printf '  \033[31mx\033[0m %s\n' "$*"; }
info()  { printf '  · %s\n' "$*"; }
aviso() { printf '  \033[33m!\033[0m %s\n' "$*"; }

# consultar <user-agent> → 0 se veio o JSON de cotas; guarda a amostra em $CORPO
consultar() {
  local resposta codigo
  resposta=$(curl -sS --max-time 30 -A "$1" -H 'X-Requested-With: XMLHttpRequest' \
    -e "$SITE/boletofacil/" -w '\n%{http_code}' "$BASE/find-cota/$CPF_FICTICIO" 2>&1)
  codigo=$(printf '%s' "$resposta" | tail -1)
  CORPO=$(printf '%s' "$resposta" | sed '$d' | tr -s '\r\n\t ' ' ' | head -c 160)
  CODIGO=$codigo
  [[ "$codigo" == 200 && "$CORPO" == *'"cotas"'* ]]
}

echo "== 1. Esta máquina"
info "Hostname: $(hostname)"
info "IP público: $(curl -s --max-time 10 https://api.ipify.org || echo 'não identificado')"

echo "== 2. Rede até o site (sem dados)"
PAGINA=$(curl -sS -o /dev/null --max-time 25 -w '%{http_code}' "$SITE/boletofacil/" 2>&1)
[[ "$PAGINA" == 200 ]] && ok "Página pública (/boletofacil/): HTTP $PAGINA" \
                       || falha "Página pública (/boletofacil/): HTTP $PAGINA"
JS=$(curl -sS --max-time 25 "$SITE/extensions/boleto-facil-rolledback/public/dist/dist.js" 2>&1)
if [[ "$JS" == *find-cota* ]]; then
  ok "JavaScript do Boleto Fácil baixou e ainda monta as rotas conhecidas"
else
  aviso "JavaScript não baixou ou mudou: confira as rotas em docs/canopus-boleto-facil.md"
fi

echo "== 3. Consulta de cotas com CPF fictício (só leitura)"
if consultar "curl/boleto-facil-teste"; then CURL_PADRAO=1; ok "curl com User-Agent de curl: HTTP $CODIGO $CORPO"
else CURL_PADRAO=0; falha "curl com User-Agent de curl: HTTP $CODIGO $CORPO"; fi
if consultar "$UA_NAVEGADOR"; then CURL_UA=1; ok "curl com User-Agent de navegador: HTTP $CODIGO $CORPO"
else CURL_UA=0; falha "curl com User-Agent de navegador: HTTP $CODIGO $CORPO"; fi

NAVEGADOR=-1
if [[ "${1:-}" == "--navegador" ]]; then
  echo "== 4. O mesmo pedido num Chromium de verdade"
  if ! command -v docker >/dev/null 2>&1; then
    aviso "Docker não encontrado: pulando o teste pelo navegador."
  else
    info "Baixando a imagem do Playwright ${PW_VERSION} na primeira vez (pode demorar)..."
    # UA de Chrome normal de propósito: o padrão do Playwright ("HeadlessChrome") é barrado
    # pela Cloudflare até de um IP que o curl atravessa.
    docker run --rm --ipc=host \
      -e BOLETO_SITE="$SITE" -e BOLETO_BASE="$BASE" -e CPF_FICTICIO="$CPF_FICTICIO" \
      -e BOLETO_UA="$UA_NAVEGADOR" \
      -v "$DIR":/teste:ro -w /tmp \
      "mcr.microsoft.com/playwright:v${PW_VERSION}-noble" \
      bash -c "npm init -y >/dev/null && npm i --silent --no-audit --no-fund playwright@${PW_VERSION} >/dev/null \
        && NODE_PATH=/tmp/node_modules node /teste/boleto-navegador.js"
    NAVEGADOR=$(( $? == 0 ? 1 : 0 ))
  fi
else
  info "Dica: rode com --navegador para testar também num Chromium de verdade."
fi

echo "== Resultado"
if [[ $CURL_PADRAO -eq 1 ]]; then
  ok "O Boleto Fácil ACEITA este IP até com cliente HTTP simples: o serviço roda aqui sem truque."
elif [[ $CURL_UA -eq 1 || $NAVEGADOR -eq 1 ]]; then
  ok "Este IP é aceito, mas só com cara de navegador."
  info "O bloqueio é de CABEÇALHO/fingerprint, não de IP: o worker manda User-Agent de navegador"
  info "(e Referer da página) e roda na VM. Não precisa trocar o IP de saída."
else
  falha "Nem curl nem navegador passaram deste IP: o bloqueio é do IP DE SAÍDA."
  info "Chromium na VM não resolve (sai pelo mesmo IP). As opções estão em"
  info "docs/canopus-boleto-facil.md, seção 'De onde a chamada sai'."
  exit 1
fi
