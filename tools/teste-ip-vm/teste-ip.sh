#!/usr/bin/env bash
# Teste: o Newcon (Canopus) aceita acesso a partir do IP desta máquina?
#
# Uso (copie a pasta teste-ip-vm inteira para a VM):
#   bash teste-ip.sh               # testes de rede + login real (precisa de Docker)
#   bash teste-ip.sh --sem-login   # só testes de rede (não precisa de Docker nem de senha)
#
# O login NÃO faz nenhuma operação: entra, confirma que chegou na tela inicial,
# tira um screenshot (login-resultado.png) e sai. Usuário e senha são pedidos no
# terminal, ou lidos de NEWCON_USER / NEWCON_PASS se já estiverem definidos.

set -uo pipefail

NEWCON_URL="${NEWCON_URL:-https://cnp3.consorciocanopus.com.br/WWW/frmCorCcCnsLogin.aspx}"
PW_VERSION="1.63.0"
DIR="$(cd "$(dirname "$0")" && pwd)"

ok()    { printf '  \033[32m✓\033[0m %s\n' "$*"; }
falha() { printf '  \033[31mx\033[0m %s\n' "$*"; }
info()  { printf '  · %s\n' "$*"; }
aviso() { printf '  \033[33m!\033[0m %s\n' "$*"; }

REDE_OK=1

http_check() { # rótulo url [informativo]
  local res
  res=$(curl -sS -o /dev/null --max-time 20 -w 'HTTP %{http_code} em %{time_total}s' "$2" 2>&1)
  if [[ "$res" =~ ^HTTP\ [23] ]]; then
    ok "$1: $res"
  elif [[ -n "${3:-}" ]]; then
    aviso "$1: $res (só informativo: a automação não usa este site)"
  else
    falha "$1: $res"
    REDE_OK=0
  fi
}

echo "== 1. Esta máquina"
info "Hostname: $(hostname)"
info "IP público: $(curl -s --max-time 10 https://api.ipify.org || echo 'não identificado')"

echo "== 2. Rede até a Canopus (sem login)"
http_check "Site institucional (www.consorciocanopus.com.br)" "https://www.consorciocanopus.com.br/" informativo
http_check "Servidor do Newcon (cnp3, raiz)" "https://cnp3.consorciocanopus.com.br/"

HTML=$(curl -sS --max-time 30 "$NEWCON_URL" 2>&1)
if [[ "$HTML" == *edtUsuario* ]]; then
  ok "Página de login do Newcon carregou com o formulário"
else
  falha "Página de login do Newcon NÃO carregou: $(printf '%s' "$HTML" | tr -s '\r\n\t ' ' ' | head -c 200)"
  REDE_OK=0
fi

if [[ "${1:-}" == "--sem-login" ]]; then
  echo "== 3. Login: pulado (--sem-login)"
else
  echo "== 3. Login real no Newcon (só entra e sai)"
  if ! command -v docker >/dev/null 2>&1; then
    falha "Docker não encontrado. Instale o Docker ou rode: bash teste-ip.sh --sem-login"
    exit 1
  fi
  [[ -z "${NEWCON_USER:-}" ]] && read -rp "  Usuário Newcon: " NEWCON_USER
  [[ -z "${NEWCON_PASS:-}" ]] && { read -rsp "  Senha Newcon (não aparece ao digitar): " NEWCON_PASS; echo; }
  export NEWCON_URL NEWCON_USER NEWCON_PASS

  info "Baixando a imagem do Playwright ${PW_VERSION} na primeira vez (pode demorar alguns minutos)..."
  # -e VAR sem valor repassa a variável do ambiente, sem a senha aparecer na linha de comando.
  # O playwright é instalado em /tmp; NODE_PATH faz o /teste/login.js encontrá-lo.
  docker run --rm --ipc=host \
    -e NEWCON_URL -e NEWCON_USER -e NEWCON_PASS \
    -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" \
    -v "$DIR":/teste -w /tmp \
    "mcr.microsoft.com/playwright:v${PW_VERSION}-noble" \
    bash -c "npm init -y >/dev/null && npm i --silent --no-audit --no-fund playwright@${PW_VERSION} >/dev/null \
      && NODE_PATH=/tmp/node_modules node /teste/login.js; rc=\$?; chown \"\$HOST_UID:\$HOST_GID\" /teste/login-resultado.png 2>/dev/null; exit \$rc"
  LOGIN_RC=$?
fi

echo "== Resultado"
if [[ $REDE_OK -eq 0 ]]; then
  falha "A rede até o Newcon falhou a partir deste IP. Provável bloqueio por IP/datacenter."
  exit 1
elif [[ "${1:-}" == "--sem-login" ]]; then
  ok "Rede OK. Rode sem --sem-login para testar o login de verdade."
elif [[ ${LOGIN_RC:-1} -eq 0 ]]; then
  ok "Newcon ACEITA acesso a partir deste IP (rede e login funcionaram)."
else
  falha "A rede respondeu, mas o login falhou. Veja a mensagem acima e o login-resultado.png."
  exit 1
fi
