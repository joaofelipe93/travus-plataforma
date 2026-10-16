#!/usr/bin/env bash
# Teste: o Boleto Fácil da Canopus aceita acesso a partir do IP desta máquina?
#
# Uso (copie a pasta teste-ip-vm inteira para a VM):
#   bash teste-boleto.sh
#
# SÓ LEITURA. Faz três coisas: carrega a página pública, baixa o JavaScript do
# Boleto Fácil e consulta /find-cota com um CPF FICTÍCIO (dígito verificador
# certo, sem cotas na Canopus). NÃO chama generate-bill nem imprime boleto:
# nenhuma cota de cliente é tocada e nada é registrado no site.
#
# O que se quer saber: o site está atrás de Cloudflare, que costuma barrar
# faixas de datacenter (DigitalOcean, AWS…) antes de a requisição chegar na
# aplicação. Se este teste passar na máquina do escritório e falhar na VM, o
# bloqueio é por IP de saída — e aí trocar curl por navegador não resolve nada,
# porque o navegador sai pelo mesmo IP.

set -uo pipefail

BASE="${BOLETO_BASE:-https://www.consorciocanopus.com.br/extensions/boleto-facil-rolledback/public/index.php}"
SITE="https://www.consorciocanopus.com.br"
# CPF fictício com dígito verificador válido: o site responde {"cotas":[]}.
CPF_FICTICIO="${CPF_FICTICIO:-11122233396}"
UA_NAVEGADOR='Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36'

ok()    { printf '  \033[32m✓\033[0m %s\n' "$*"; }
falha() { printf '  \033[31mx\033[0m %s\n' "$*"; }
info()  { printf '  · %s\n' "$*"; }
aviso() { printf '  \033[33m!\033[0m %s\n' "$*"; }

PROBLEMAS=0

# pegar <rótulo> <url> [user-agent]
pegar() {
  local rotulo=$1 url=$2 ua=${3:-curl/boleto-facil-teste}
  local corpo codigo tipo
  corpo=$(curl -sS --max-time 30 -A "$ua" -w '\n%{http_code}\t%{content_type}' "$url" 2>&1)
  codigo=$(printf '%s' "$corpo" | tail -1 | cut -f1)
  tipo=$(printf '%s' "$corpo" | tail -1 | cut -f2)
  CORPO=$(printf '%s' "$corpo" | sed '$d')
  if [[ "$codigo" =~ ^2 ]]; then
    ok "$rotulo: HTTP $codigo ($tipo)"
    return 0
  fi
  falha "$rotulo: HTTP $codigo ($tipo)"
  printf '%s' "$CORPO" | tr -s '\r\n\t ' ' ' | head -c 300 | sed 's/^/      /'
  echo
  PROBLEMAS=$((PROBLEMAS + 1))
  return 1
}

echo "== 1. Esta máquina"
info "Hostname: $(hostname)"
info "IP público: $(curl -s --max-time 10 https://api.ipify.org || echo 'não identificado')"

echo "== 2. Site e JavaScript do Boleto Fácil"
pegar "Página pública (/boletofacil/)" "$SITE/boletofacil/"
pegar "JavaScript do Boleto Fácil" "$SITE/extensions/boleto-facil-rolledback/public/dist/dist.js"
if [[ "$CORPO" == *find-cota* ]]; then
  ok "O JavaScript ainda monta as rotas conhecidas (find-cota/generate-bill)"
else
  aviso "O JavaScript mudou: as rotas podem não ser mais as de docs/canopus-boleto-facil.md"
fi

echo "== 3. Consulta de cotas (CPF fictício, sem cotas)"
if pegar "find-cota com User-Agent de curl" "$BASE/find-cota/$CPF_FICTICIO"; then
  if [[ "$CORPO" == *'"cotas"'* ]]; then
    ok "Resposta é o JSON esperado: $(printf '%s' "$CORPO" | head -c 60)"
  else
    aviso "HTTP 200, mas o corpo não é o JSON de cotas (desafio da Cloudflare?): $(printf '%s' "$CORPO" | tr -s '\r\n\t ' ' ' | head -c 160)"
    PROBLEMAS=$((PROBLEMAS + 1))
  fi
fi
# Se o curl for barrado mas o navegador passar, o bloqueio é por User-Agent/bot,
# não por IP — e aí a saída é mandar os cabeçalhos de navegador, não trocar de IP.
pegar "find-cota com User-Agent de navegador" "$BASE/find-cota/$CPF_FICTICIO" "$UA_NAVEGADOR" >/dev/null \
  && ok "find-cota também responde com cabeçalho de navegador" \
  || aviso "find-cota recusou o cabeçalho de navegador (veja acima)"

echo "== Resultado"
if [[ $PROBLEMAS -eq 0 ]]; then
  ok "O Boleto Fácil ACEITA acesso a partir deste IP (só leitura, nada foi gerado)."
else
  falha "$PROBLEMAS checagem(ns) falharam a partir deste IP."
  info "Se na máquina do escritório passa e aqui não, o bloqueio é do IP de saída:"
  info "navegador na mesma VM não resolve (mesmo IP). Ver docs/canopus-boleto-facil.md."
  exit 1
fi
