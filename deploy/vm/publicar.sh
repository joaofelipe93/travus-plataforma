#!/usr/bin/env bash
# Ativa uma versão da plataforma na VM de produção. Roda na VM como o usuário travus.
#
#   publicar.sh --imagens vX.Y.Z  deploy pelo GitHub (padrão): /opt/travus/releases/vX.Y.Z (baixada da
#                                 tag pelo entrada-ci.sh) com as imagens prontas do GHCR
#   publicar.sh <commit>          deploy manual (plano B, make deploy): releases/<commit> enviada da
#                                 sua máquina, imagens compiladas na VM, uma por vez
#   publicar.sh --voltar          sobe de novo a versão publicada antes da atual, do mesmo jeito que
#                                 ela foi publicada (migrações não são desfeitas)
#
# Segredos e dados ficam fora das versões, em /opt/travus/compartilhado (só na VM):
#   .env          deploy/.env de produção (criado na primeira vez a partir do .env.producao.example)
#   canopus.env   credenciais do Newcon do worker (NEWCON_URL, NEWCON_USER, NEWCON_PASS)
#
# Códigos de saída: 0 publicada; 3 recusada ANTES de mudar qualquer coisa (execução em andamento,
# segredos, download das imagens, backup): nada a desfazer; outro código: falhou no meio da troca
# (o deploy pelo GitHub volta para a versão anterior). Recusa com execução em andamento
# (FORCAR=1 publica mesmo assim; só no deploy manual).
set -euo pipefail

BASE=/opt/travus
COMP=$BASE/compartilhado
HISTORICO=$BASE/versoes-publicadas
MANTER=5

falhar() { echo "erro: $*" >&2; exit 1; }
recusar() { echo "recusado (nada foi alterado): $*" >&2; exit 3; }

# Linha do histórico: "<id> <modo>" (modo: imagens ou compilar; linhas antigas, só com o id, são
# de deploy compilado).
versao_anterior() {
  local atual
  atual=$(basename "$(readlink -f "$BASE/atual" 2> /dev/null || true)")
  tac "$HISTORICO" 2> /dev/null | awk -v atual="$atual" '$1 != atual { print $1, ($2 == "" ? "compilar" : $2); exit }'
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
    exit 3
  fi
  # Segredos que versões novas passaram a exigir: acrescenta o que faltar (nunca troca um existente).
  local nome
  for nome in CHECKIN_WEBHOOK_SECRET CHECKIN_ADMIN_TOKEN CHECKIN_DB_SENHA ASSISTENTE_DB_SENHA; do
    if ! grep -q "^$nome=" "$COMP/.env"; then
      (umask 077; printf '%s=%s\n' "$nome" "$(openssl rand -hex 32)" >> "$COMP/.env")
      echo "Acrescentado $nome em $COMP/.env."
    fi
  done
  if grep -q '^DOMINIO_APP=app\.exemplo\.com\.br$' "$COMP/.env"; then
    recusar "preencha DOMINIO_APP e DOMINIO_API em $COMP/.env"
  fi
  [ -f "$COMP/canopus.env" ] || recusar "falta $COMP/canopus.env com NEWCON_URL, NEWCON_USER e NEWCON_PASS (docs/producao.md)"
  chmod 600 "$COMP/.env" "$COMP/canopus.env"
}

ativar() { # id (vX.Y.Z ou commit), modo (imagens ou compilar)
  local id=$1 modo=$2
  local dir=$BASE/releases/$id
  [ -d "$dir" ] || recusar "versão $id não encontrada em $BASE/releases"
  preparar_segredos "$dir"
  ln -sfn "$COMP/.env" "$dir/deploy/.env"
  ln -sfn "$COMP/canopus.env" "$dir/workers/canopus/.env"
  cd "$dir"

  HOST_UID=$(id -u)
  HOST_GID=$(id -g)
  # Versão (version.txt da release, mantido pelo release-please) e commit nas imagens e no /health.
  VERSAO=$(cat version.txt 2> /dev/null || echo dev)
  if [ "$modo" = imagens ]; then
    [ "v$VERSAO" = "$id" ] || recusar "a tag $id tem version.txt $VERSAO: a tag não é uma versão do release-please"
    COMMIT=$(cat COMMIT 2> /dev/null || echo desconhecido)
  else
    COMMIT=$id
  fi
  export HOST_UID HOST_GID VERSAO COMMIT
  local compose=(docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.prod.yml)
  [ "$modo" = imagens ] && compose+=(-f deploy/docker-compose.imagens.yml)

  # Não publica no meio de uma execução (o worker seria reiniciado entre cotas de um lance real).
  local em_andamento=0 banco_no_ar=0
  if docker ps --format '{{.Names}}' | grep -qx travus-postgres-1; then
    banco_no_ar=1
    em_andamento=$("${compose[@]}" exec -T postgres sh -c \
      "psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -tAc \"SELECT count(*) FROM execucoes WHERE status = 'em_andamento'\"" \
      2> /dev/null || echo 0)
  fi
  if [ "${em_andamento:-0}" != 0 ] && [ "${FORCAR:-}" != 1 ]; then
    recusar "há $em_andamento execução(ões) em andamento: espere terminar"
  fi

  if grep -q '^LANCE_REAL_HABILITADO=true' "$COMP/.env"; then
    echo "ATENÇÃO: LANCE_REAL_HABILITADO=true nesta VM: o worker registra lances reais aprovados."
  fi
  local backups
  backups=$(sed -n 's/^BACKUP_DIR=//p' "$COMP/.env")
  mkdir -p "${backups:-$BASE/backups}"

  if [ "$modo" = imagens ]; then
    # Tudo baixado antes de parar qualquer coisa: se o registro falhar, a versão atual segue no ar.
    echo "== Baixando as imagens da versão v$VERSAO (commit $COMMIT)"
    "${compose[@]}" pull --quiet migrate api web worker-canopus checkin-whatsapp \
      || recusar "não consegui baixar as imagens v$VERSAO do GHCR (os pacotes estão públicos?)"
  else
    # Uma imagem de cada vez: a VM tem 2 GB e a versão atual continua no ar durante o build. Em
    # paralelo, o next build, o Go e o npm ci juntos passam da memória e o build morre no meio.
    # Serviço sem "build" (postgres, traefik) não faz nada aqui.
    echo "== Compilando a versão v$VERSAO, commit $COMMIT (uma imagem por vez)"
    local servico
    for servico in $("${compose[@]}" config --services); do
      "${compose[@]}" build "$servico" || recusar "o build de $servico falhou"
    done
  fi

  # Backup antes de migrar: o rollback volta o código, não o banco.
  if [ "$banco_no_ar" = 1 ]; then
    echo "== Backup do Postgres antes do deploy"
    "${compose[@]}" --profile backup run --rm -T backup antes-do-deploy "v$VERSAO" \
      || recusar "o backup antes do deploy falhou"
  fi

  echo "== Subindo a versão v$VERSAO (commit $COMMIT)"
  "${compose[@]}" up -d --wait --remove-orphans --no-build

  ln -sfn "$dir" "$BASE/atual"
  echo "$id $modo" >> "$HISTORICO"

  # Guarda as últimas versões publicadas; nunca apaga a atual.
  local atual
  atual=$(readlink -f "$BASE/atual")
  ls -1dt "$BASE"/releases/*/ | tail -n +"$((MANTER + 1))" | while read -r velha; do
    [ "$(readlink -f "$velha")" = "$atual" ] || rm -rf "$velha"
  done
  # Imagens do GHCR de versões que não estão mais entre as guardadas (as delas ficam para o rollback).
  local manter_tags
  manter_tags=$(ls -1 "$BASE/releases" | sed -n 's/^v\([0-9][0-9.]*\)$/\1/p' | tr '\n' '|')
  # A limpeza nunca derruba um deploy que já subiu (grep sem resultado sai com 1 e o pipefail pegaria).
  { docker image ls --format '{{.Repository}}:{{.Tag}}' | grep -E '^ghcr\.io/[^/]+/travus-[a-z-]+:[0-9]+\.[0-9]+\.[0-9]+$' || true; } \
    | while read -r imagem; do
      case "|$manter_tags" in *"|${imagem##*:}|"*) ;; *) docker image rm "$imagem" > /dev/null 2>&1 || true ;; esac
    done
  docker image prune -f > /dev/null || true

  "${compose[@]}" ps --format 'table {{.Service}}\t{{.Status}}'
  echo "Versão v$VERSAO (commit $COMMIT) publicada."
}

case "${1:-}" in
  --voltar)
    read -r anterior modo_anterior <<< "$(versao_anterior)" || true
    [ -n "${anterior:-}" ] || recusar "não há versão anterior registrada em $HISTORICO"
    echo "Voltando para a versão $anterior ($modo_anterior)"
    ativar "$anterior" "$modo_anterior"
    ;;
  --imagens)
    [[ "${2:-}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || recusar "uso: publicar.sh --imagens vX.Y.Z"
    ativar "$2" imagens
    ;;
  "" | -*) recusar "uso: publicar.sh --imagens vX.Y.Z | <commit> | --voltar" ;;
  *) ativar "$1" compilar ;;
esac
