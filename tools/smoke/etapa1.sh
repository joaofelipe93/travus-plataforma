#!/usr/bin/env bash
# Checagens da Etapa 1 pelo gateway (Traefik), com curl. Precisa de `make up`.
#
#   bash tools/smoke/etapa1.sh
#
# Cria (ou redefine a senha de) dois usuários de teste, smoke-operador@travus.local e
# smoke-leitura@travus.local, e os desativa no fim. Envia uma planilha FICTÍCIA só para
# gerar a prévia e a descarta: o cadastro não é alterado. Não acessa o Newcon.
set -uo pipefail

RAIZ="$(cd "$(dirname "$0")/../.." && pwd)"
COMPOSE=(docker compose -f "$RAIZ/deploy/docker-compose.yml")
APP=http://app.localhost
API=http://api.localhost
PLANILHA="$RAIZ/api/internal/importacao/testdata/paridade/01-planilha-clientes.csv"
COOKIE_NOME="__Host-travus_sessao"
SENHA="smoke-$(openssl rand -hex 12)"
FALHAS=0

ok()    { printf '  \033[32m✓\033[0m %s\n' "$*"; }
falha() { printf '  \033[31mx\033[0m %s\n' "$*"; FALHAS=$((FALHAS + 1)); }

# esperar "descrição" código_esperado código_obtido
esperar() {
  if [[ "$3" == "$2" ]]; then ok "$1 → $3"; else falha "$1 → $3 (esperado $2)"; fi
}

codigo() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

usuario_cli() { # email perfil
  local email=$1 perfil=$2 saida
  saida=$(printf '%s\n' "$SENHA" | "${COMPOSE[@]}" --profile cli run --rm -T cli criar --email "$email" --nome "Smoke $perfil" --perfil "$perfil" 2>&1)
  if [[ "$saida" == *"já existe"* ]]; then
    printf '%s\n' "$SENHA" | "${COMPOSE[@]}" --profile cli run --rm -T cli senha --email "$email" >/dev/null 2>&1
    "${COMPOSE[@]}" --profile cli run --rm -T cli ativar --email "$email" >/dev/null 2>&1
  fi
}

# login email → define COOKIE e CSRF
login() {
  local resposta cabecalhos
  cabecalhos=$(mktemp)
  resposta=$(curl -s -D "$cabecalhos" -H 'Content-Type: application/json' -H "Origin: $APP" \
    -d "{\"email\":\"$1\",\"senha\":\"$SENHA\"}" "$APP/api/auth/login")
  COOKIE=$(grep -i "^set-cookie: $COOKIE_NOME=" "$cabecalhos" | sed -E "s/^[^:]+: ($COOKIE_NOME=[^;]*).*/\1/" | tr -d '\r')
  ATRIBUTOS=$(grep -i "^set-cookie: $COOKIE_NOME=" "$cabecalhos" | tr -d '\r')
  CSRF=$(printf '%s' "$resposta" | sed -nE 's/.*"csrf_token":"([^"]+)".*/\1/p')
  rm -f "$cabecalhos"
}

echo "== Preparando usuários de teste"
usuario_cli smoke-operador@travus.local operador
usuario_cli smoke-leitura@travus.local leitura

echo "== Rotas públicas"
esperar "GET api.localhost/health" 200 "$(codigo "$API/health")"
esperar "GET app.localhost/api/health" 200 "$(codigo "$APP/api/health")"
esperar "GET app.localhost/login" 200 "$(codigo "$APP/login")"

echo "== Sem sessão, barrado no gateway"
esperar "GET app.localhost/api/cotas" 401 "$(codigo "$APP/api/cotas")"
esperar "GET api.localhost/cotas" 401 "$(codigo "$API/cotas")"
esperar "GET app.localhost/api/auth/sessao" 401 "$(codigo "$APP/api/auth/sessao")"
LOCAL=$(curl -s -o /dev/null -w '%{http_code} %{redirect_url}' "$APP/cotas")
esperar "GET app.localhost/cotas (redireciona)" "302 $APP/login?proximo=%2Fcotas" "$LOCAL"
esperar "Cabeçalho vindo do cliente não fura a barreira" 401 "$(codigo -H 'X-Usuario-Perfil: admin' "$APP/api/cotas")"

echo "== Login"
esperar "login com senha errada" 401 "$(codigo -H 'Content-Type: application/json' -H "Origin: $APP" -d '{"email":"smoke-operador@travus.local","senha":"errada-errada-1"}' "$APP/api/auth/login")"
esperar "login de outra origem" 403 "$(codigo -H 'Content-Type: application/json' -H 'Origin: http://outro.site' -d "{\"email\":\"smoke-operador@travus.local\",\"senha\":\"$SENHA\"}" "$APP/api/auth/login")"

login smoke-operador@travus.local
if [[ -n "$COOKIE" && -n "$CSRF" ]]; then ok "login do operador: cookie e token CSRF recebidos"; else falha "login do operador sem cookie/CSRF"; fi
for atributo in HttpOnly Secure 'SameSite=Strict' 'Path=/'; do
  if [[ "$ATRIBUTOS" == *"$atributo"* ]]; then ok "cookie com $atributo"; else falha "cookie sem $atributo"; fi
done
OPERADOR_COOKIE=$COOKIE OPERADOR_CSRF=$CSRF

esperar "GET app.localhost/api/auth/sessao (operador)" 200 "$(codigo -H "Cookie: $OPERADOR_COOKIE" "$APP/api/auth/sessao")"
esperar "GET app.localhost/api/cotas (operador)" 200 "$(codigo -H "Cookie: $OPERADOR_COOKIE" "$APP/api/cotas")"
esperar "GET app.localhost/cotas (página, operador)" 200 "$(codigo -H "Cookie: $OPERADOR_COOKIE" "$APP/cotas")"

echo "== CSRF e perfis"
esperar "POST importação sem token CSRF" 403 "$(codigo -H "Cookie: $OPERADOR_COOKIE" -H "Origin: $APP" -F "arquivo=@$PLANILHA" "$APP/api/importacoes")"

login smoke-leitura@travus.local
LEITURA_COOKIE=$COOKIE LEITURA_CSRF=$CSRF
esperar "GET app.localhost/api/cotas (leitura)" 200 "$(codigo -H "Cookie: $LEITURA_COOKIE" "$APP/api/cotas")"
esperar "POST importação (leitura)" 403 "$(codigo -H "Cookie: $LEITURA_COOKIE" -H "Origin: $APP" -H "X-CSRF-Token: $LEITURA_CSRF" -F "arquivo=@$PLANILHA" "$APP/api/importacoes")"

RESPOSTA=$(curl -s -w '\n%{http_code}' -H "Cookie: $OPERADOR_COOKIE" -H "Origin: $APP" -H "X-CSRF-Token: $OPERADOR_CSRF" -F "arquivo=@$PLANILHA" "$APP/api/importacoes")
esperar "POST importação (operador, planilha fictícia)" 201 "$(tail -1 <<<"$RESPOSTA")"
IMPORTACAO=$(head -1 <<<"$RESPOSTA" | sed -nE 's/^\{"id":([0-9]+).*/\1/p')
if [[ -n "$IMPORTACAO" ]]; then
  esperar "descartar a prévia fictícia" 200 "$(codigo -X POST -H "Cookie: $OPERADOR_COOKIE" -H "Origin: $APP" -H "X-CSRF-Token: $OPERADOR_CSRF" "$APP/api/importacoes/$IMPORTACAO/descartar")"
fi

echo "== Execuções (sem criar dry-run: isso faria o worker entrar no Newcon)"
esperar "GET app.localhost/api/execucoes (leitura)" 200 "$(codigo -H "Cookie: $LEITURA_COOKIE" "$APP/api/execucoes")"
esperar "POST execução (leitura)" 403 "$(codigo -H "Cookie: $LEITURA_COOKIE" -H "Origin: $APP" -H "X-CSRF-Token: $LEITURA_CSRF" -H 'Content-Type: application/json' -d '{"tipo":"dry_run","cota_ids":[1]}' "$APP/api/execucoes")"
esperar "POST execução real (operador, recusada)" 422 "$(codigo -H "Cookie: $OPERADOR_COOKIE" -H "Origin: $APP" -H "X-CSRF-Token: $OPERADOR_CSRF" -H 'Content-Type: application/json' -d '{"tipo":"real","cota_ids":[1]}' "$APP/api/execucoes")"
esperar "rota interna do worker pelo gateway (com sessão)" 404 "$(codigo -X POST -H "Cookie: $OPERADOR_COOKIE" -H "Origin: $APP" -H "X-CSRF-Token: $OPERADOR_CSRF" "$APP/api/internal/tarefas/proxima")"
esperar "rota interna do worker em api.localhost" 401 "$(codigo -X POST "$API/internal/tarefas/proxima")"
esperar "porta interna 8081 no host" 000 "$(codigo -m 3 -X POST http://localhost:8081/internal/tarefas/proxima)"

echo "== Logout"
esperar "POST logout (leitura)" 204 "$(codigo -X POST -H "Cookie: $LEITURA_COOKIE" -H "Origin: $APP" -H "X-CSRF-Token: $LEITURA_CSRF" "$APP/api/auth/logout")"
esperar "POST logout (operador)" 204 "$(codigo -X POST -H "Cookie: $OPERADOR_COOKIE" -H "Origin: $APP" -H "X-CSRF-Token: $OPERADOR_CSRF" "$APP/api/auth/logout")"
esperar "sessão depois do logout" 401 "$(codigo -H "Cookie: $OPERADOR_COOKIE" "$APP/api/auth/sessao")"

echo "== Limite de tentativas de login (por último: bloqueia o IP por até 1 minuto)"
CODIGOS=$(for _ in $(seq 15); do codigo -H 'Content-Type: application/json' -H "Origin: $APP" -d '{"email":"x@x","senha":"yyyyyyyyyyyy"}' "$APP/api/auth/login"; echo; done | sort | uniq -c | tr -s ' ' | tr '\n' ';')
if [[ "$CODIGOS" == *429* ]]; then ok "15 logins seguidos: $CODIGOS"; else falha "15 logins seguidos sem 429: $CODIGOS"; fi

echo "== Limpando"
"${COMPOSE[@]}" --profile cli run --rm -T cli desativar --email smoke-operador@travus.local >/dev/null 2>&1
"${COMPOSE[@]}" --profile cli run --rm -T cli desativar --email smoke-leitura@travus.local >/dev/null 2>&1
ok "usuários de teste desativados"

echo
if [[ $FALHAS -eq 0 ]]; then
  printf '\033[32mTodas as checagens passaram.\033[0m\n'
else
  printf '\033[31m%d checagem(ns) falharam.\033[0m\n' "$FALHAS"
  exit 1
fi
