#!/usr/bin/env bash
# Checagens de produção pelo lado de fora, com curl: HTTPS, redirecionamento, HSTS, barreira de
# sessão, dashboard e portas internas fechadas, validade do certificado. Não faz login nem cria
# nada (não acessa o Newcon).
#
#   bash tools/smoke/producao.sh app.<domínio> api.<domínio>
#   INSEGURO=1 bash tools/smoke/producao.sh app.localhost api.localhost   # make prod-local ou le-teste
set -uo pipefail

APP=${1:?uso: producao.sh app.<domínio> api.<domínio>}
API=${2:?uso: producao.sh app.<domínio> api.<domínio>}
K=()
[ "${INSEGURO:-}" = 1 ] && K=(-k)
FALHAS=0

ok()    { printf '  \033[32m✓\033[0m %s\n' "$*"; }
falha() { printf '  \033[31mx\033[0m %s\n' "$*"; FALHAS=$((FALHAS + 1)); }
esperar() { if [[ "$3" == "$2" ]]; then ok "$1 → $3"; else falha "$1 → $3 (esperado $2)"; fi; }
codigo() { curl "${K[@]}" -s -o /dev/null -m 15 -w '%{http_code}' "$@"; }

redireciona() { # url destino
  local r
  r=$(curl -s -o /dev/null -m 15 -w '%{http_code} %{redirect_url}' "$1")
  if [[ "$r" == "301 $2" || "$r" == "308 $2" ]]; then ok "$1 → $r"; else falha "$1 → $r (esperado 301/308 para $2)"; fi
}

[ "${INSEGURO:-}" = 1 ] && echo "(INSEGURO=1: certificado não é verificado)"

echo "== HTTP vai para HTTPS"
redireciona "http://$APP/login" "https://$APP/login"
redireciona "http://$API/health" "https://$API/health"

echo "== Rotas públicas"
SAUDE=$(curl "${K[@]}" -s -m 15 "https://$API/health")
if [[ "$SAUDE" == *'"status":"ok"'* && "$SAUDE" == *'"banco":"ok"'* ]]; then ok "https://$API/health: API e banco ok"; else falha "https://$API/health: ${SAUDE:-sem resposta}"; fi
esperar "https://$APP/api/health" 200 "$(codigo "https://$APP/api/health")"
esperar "https://$APP/login" 200 "$(codigo "https://$APP/login")"
HSTS=$(curl "${K[@]}" -s -o /dev/null -D - -m 15 "https://$APP/login" | tr -d '\r' | grep -i '^strict-transport-security:')
if [[ "$HSTS" == *max-age=31536000* ]]; then ok "HSTS em https://$APP/login"; else falha "sem HSTS em https://$APP/login"; fi

echo "== Sem sessão, barrado no gateway"
esperar "https://$APP/api/cotas" 401 "$(codigo "https://$APP/api/cotas")"
esperar "https://$API/cotas" 401 "$(codigo "https://$API/cotas")"
LOCAL=$(curl "${K[@]}" -s -o /dev/null -m 15 -w '%{http_code} %{redirect_url}' "https://$APP/canopus/cotas")
esperar "https://$APP/canopus/cotas (redireciona)" "302 https://$APP/login?proximo=%2Fcanopus%2Fcotas" "$LOCAL"
esperar "rota interna do worker em $API" 401 "$(codigo -X POST "https://$API/internal/tarefas/proxima")"

echo "== Notificador de check-in"
# Só o POST do webhook chega ao serviço, e sem o token ele recusa. QR e payloads, nunca.
RESPOSTA=$(curl "${K[@]}" -s -m 15 -w '\n%{http_code}' -X POST -H 'Content-Type: application/json' -d '{"id":"smoke"}' "https://$API/webhooks/nova-reserva")
esperar "POST https://$API/webhooks/nova-reserva sem token" 401 "$(tail -1 <<<"$RESPOSTA")"
if [[ "$RESPOSTA" == *'"error":"unauthorized"'* ]]; then ok "a recusa veio do notificador"; else falha "webhook não chegou ao notificador: $(head -1 <<<"$RESPOSTA")"; fi
for caminho in /whatsapp/status /whatsapp/groups /events; do
  esperar "https://$API$caminho sem sessão" 401 "$(codigo "https://$API$caminho")"
done

echo "== Fechado para fora"
esperar "dashboard do Traefik (Host: traefik.localhost)" 404 "$(codigo -H 'Host: traefik.localhost' "https://$APP/")"
for porta in 8080 8081 5432 3000; do
  if timeout 5 bash -c "exec 3<>/dev/tcp/$APP/$porta" 2> /dev/null; then
    falha "porta $porta aberta em $APP"
  else
    ok "porta $porta fechada em $APP"
  fi
done

if [ "${INSEGURO:-}" != 1 ]; then
  echo "== Certificado"
  CERT=$(echo | openssl s_client -connect "$APP:443" -servername "$APP" 2> /dev/null | openssl x509 -noout -issuer -enddate 2> /dev/null)
  FIM=$(sed -n 's/^notAfter=//p' <<< "$CERT")
  if [ -n "$FIM" ]; then
    DIAS=$(( ($(date -d "$FIM" +%s) - $(date +%s)) / 86400 ))
    EMISSOR=$(sed -n 's/^issuer=//p' <<< "$CERT")
    if [ "$DIAS" -ge 14 ]; then ok "certificado vence em $DIAS dias ($EMISSOR)"; else falha "certificado vence em $DIAS dias ($EMISSOR)"; fi
  else
    falha "não consegui ler o certificado de $APP:443"
  fi
fi

echo
if [[ $FALHAS -eq 0 ]]; then
  printf '\033[32mTodas as checagens passaram.\033[0m\n'
else
  printf '\033[31m%d checagem(ns) falharam.\033[0m\n' "$FALHAS"
  exit 1
fi
