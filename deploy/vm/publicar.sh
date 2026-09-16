#!/usr/bin/env bash
# Ativa uma versão da plataforma na VM de produção. Quem chama é o `make deploy` (ou o
# `make deploy-voltar`) na sua máquina; roda na VM como o usuário travus.
#
#   publicar.sh <versão>   sobe /opt/travus/releases/<versão> (enviada pelo make deploy)
#   publicar.sh --voltar   sobe de novo a versão publicada antes da atual
#
# Segredos e dados ficam fora das versões, em /opt/travus/compartilhado (só na VM):
#   .env          deploy/.env de produção (criado na primeira vez a partir do .env.producao.example)
#   canopus.env   credenciais do Newcon do worker (NEWCON_URL, NEWCON_USER, NEWCON_PASS)
# Recusa publicar com execução em andamento (FORCAR=1 publica mesmo assim).
set -euo pipefail

BASE=/opt/travus
COMP=$BASE/compartilhado
HISTORICO=$BASE/versoes-publicadas
MANTER=5

falhar() { echo "erro: $*" >&2; exit 1; }

versao_anterior() {
  local atual
  atual=$(basename "$(readlink -f "$BASE/atual" 2> /dev/null || true)")
  tac "$HISTORICO" 2> /dev/null | awk -v atual="$atual" '$1 != atual { print $1; exit }'
}

preparar_segredos() { # pasta da versão
  if [ ! -f "$COMP/.env" ]; then
    (
      umask 077
      sed "s/troque-esta-senha/$(openssl rand -hex 24)/" "$1/deploy/.env.producao.example" > "$COMP/.env"
      printf '\nWORKER_TOKEN=%s\nCHAVE_CRIPTOGRAFIA=%s\n' "$(openssl rand -hex 32)" "$(openssl rand -hex 32)" >> "$COMP/.env"
    )
    echo "Criado $COMP/.env com senha do Postgres, WORKER_TOKEN e CHAVE_CRIPTOGRAFIA novos."
    echo "Agora: preencha DOMINIO_APP, DOMINIO_API e os dados de SMTP nele, guarde a"
    echo "CHAVE_CRIPTOGRAFIA fora da VM e rode o deploy de novo."
    exit 1
  fi
  # Segredos que versões novas passaram a exigir: acrescenta o que faltar (nunca troca um existente).
  local nome
  for nome in CHECKIN_WEBHOOK_SECRET CHECKIN_ADMIN_TOKEN CHECKIN_DB_SENHA; do
    if ! grep -q "^$nome=" "$COMP/.env"; then
      (umask 077; printf '%s=%s\n' "$nome" "$(openssl rand -hex 32)" >> "$COMP/.env")
      echo "Acrescentado $nome em $COMP/.env."
    fi
  done
  if grep -q '^DOMINIO_APP=app\.exemplo\.com\.br$' "$COMP/.env"; then
    falhar "preencha DOMINIO_APP e DOMINIO_API em $COMP/.env"
  fi
  [ -f "$COMP/canopus.env" ] || falhar "falta $COMP/canopus.env com NEWCON_URL, NEWCON_USER e NEWCON_PASS (docs/producao.md)"
  chmod 600 "$COMP/.env" "$COMP/canopus.env"
}

ativar() {
  local versao=$1
  local dir=$BASE/releases/$versao
  [ -d "$dir" ] || falhar "versão $versao não encontrada em $BASE/releases"
  preparar_segredos "$dir"
  ln -sfn "$COMP/.env" "$dir/deploy/.env"
  ln -sfn "$COMP/canopus.env" "$dir/workers/canopus/.env"
  cd "$dir"

  HOST_UID=$(id -u)
  HOST_GID=$(id -g)
  export HOST_UID HOST_GID
  local compose=(docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.prod.yml)

  # Não publica no meio de uma execução (o worker seria reiniciado entre cotas de um lance real).
  local em_andamento=0
  if docker ps --format '{{.Names}}' | grep -qx travus-postgres-1; then
    em_andamento=$("${compose[@]}" exec -T postgres sh -c \
      "psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -tAc \"SELECT count(*) FROM execucoes WHERE status = 'em_andamento'\"" \
      2> /dev/null || echo 0)
  fi
  if [ "${em_andamento:-0}" != 0 ] && [ "${FORCAR:-}" != 1 ]; then
    falhar "há $em_andamento execução(ões) em andamento: espere terminar (ou publique com FORCAR=1)"
  fi

  if grep -q '^LANCE_REAL_HABILITADO=true' "$COMP/.env"; then
    echo "ATENÇÃO: LANCE_REAL_HABILITADO=true nesta VM: o worker registra lances reais aprovados."
  fi
  local backups
  backups=$(sed -n 's/^BACKUP_DIR=//p' "$COMP/.env")
  mkdir -p "${backups:-$BASE/backups}"

  echo "== Subindo a versão $versao"
  "${compose[@]}" up -d --build --wait --remove-orphans

  ln -sfn "$dir" "$BASE/atual"
  echo "$versao" >> "$HISTORICO"

  # Guarda as últimas versões publicadas; nunca apaga a atual.
  local atual
  atual=$(readlink -f "$BASE/atual")
  ls -1dt "$BASE"/releases/*/ | tail -n +"$((MANTER + 1))" | while read -r velha; do
    [ "$(readlink -f "$velha")" = "$atual" ] || rm -rf "$velha"
  done
  docker image prune -f > /dev/null

  "${compose[@]}" ps --format 'table {{.Service}}\t{{.Status}}'
  echo "Versão $versao publicada."
}

case "${1:-}" in
  --voltar)
    anterior=$(versao_anterior)
    [ -n "$anterior" ] || falhar "não há versão anterior registrada em $HISTORICO"
    echo "Voltando para a versão $anterior"
    ativar "$anterior"
    ;;
  "" | -*) falhar "uso: publicar.sh <versão> | --voltar" ;;
  *) ativar "$1" ;;
esac
