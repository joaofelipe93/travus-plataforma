#!/usr/bin/env bash
# CI: todo serviço nosso que roda em produção precisa de imagem publicada no GHCR.
#
# Existe por causa de uma falha real (v0.13.0, 16/10/2026): os dois serviços de boletos foram
# ligados em produção, mas o deploy pelo GitHub não compilava as imagens deles e o
# docker-compose.imagens.yml não as mapeava. A VM tentou baixar a tag local `travus/...:dev` do
# Docker Hub, levou "pull access denied" e o deploy caiu no meio da troca (o rollback automático
# devolveu a versão anterior, mas a versão nova não subiu).
#
# A regra é simples: se o serviço aparece no compose de produção e a imagem dele é nossa
# (`travus/...`), então
#   1. `deploy/docker-compose.imagens.yml` tem de trocar essa imagem por uma do registro; e
#   2. `.github/workflows/deploy.yml` tem de compilar essa imagem.
#
#   bash tools/ci/checar-imagens-deploy.sh
set -euo pipefail
cd "$(dirname "$0")/../.."

falhas=0
falha() { echo "x $*"; falhas=$((falhas + 1)); }

# Variáveis só para o compose interpolar; nada aqui vai para lugar nenhum.
export POSTGRES_PASSWORD=x WORKER_TOKEN=x CHAVE_CRIPTOGRAFIA=x \
  CHECKIN_WEBHOOK_SECRET=x CHECKIN_ADMIN_TOKEN=x CHECKIN_DB_SENHA=x ASSISTENTE_DB_SENHA=x \
  BOLETOS_ADMIN_TOKEN=x BOLETOS_DB_SENHA=x \
  DOMINIO_APP=app.exemplo.com.br DOMINIO_API=api.exemplo.com.br BACKUP_DIR=/tmp

prod=$(docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.prod.yml config 2> /dev/null)
[ -n "$prod" ] || { echo "x não consegui ler o compose de produção"; exit 1; }

# Serviços do compose de produção com imagem nossa (travus/...), na ordem em que aparecem.
# O config normaliza tudo em "  <servico>:" e "    image: ...".
nossos=$(printf '%s\n' "$prod" | awk '
  /^  [a-z0-9_-]+:$/ { servico = $1; sub(":", "", servico) }
  /^    image: travus\// { print servico }
' | sort -u)
[ -n "$nossos" ] || { echo "x nenhum serviço com imagem travus/ no compose de produção (isso não deveria acontecer)"; exit 1; }

for servico in $nossos; do
  # 1. o compose de imagens troca a imagem desse serviço?
  if ! awk -v s="  $servico:" '
      $0 == s { achou = 1; next }
      achou && /^  [a-z0-9_-]+:$/ { exit }
      achou && /image:.*travus-/ { print; found = 1; exit }
      END { exit !found }
    ' deploy/docker-compose.imagens.yml > /dev/null; then
    falha "$servico roda em produção com imagem nossa, mas deploy/docker-compose.imagens.yml não aponta para o registro: a VM tentaria baixar a tag :dev"
    continue
  fi

  # 2. a imagem que ele usa é compilada pelo workflow de deploy?
  imagem=$(awk -v s="  $servico:" '
      $0 == s { achou = 1; next }
      achou && /^  [a-z0-9_-]+:$/ { exit }
      achou && /image:/ { print; exit }
    ' deploy/docker-compose.imagens.yml | sed -nE 's#.*/travus-([a-z0-9-]+):.*#\1#p')
  if [ -z "$imagem" ]; then
    falha "$servico: não consegui ler o nome da imagem em deploy/docker-compose.imagens.yml"
    continue
  fi
  if ! grep -qE "(^|[[:space:]])$imagem:[a-zA-Z0-9./_-]+" .github/workflows/deploy.yml; then
    falha "a imagem travus-$imagem ($servico) não é compilada pelo .github/workflows/deploy.yml"
    continue
  fi
  echo "✓ $servico → travus-$imagem (mapeada e compilada)"
done

if [ "$falhas" -gt 0 ]; then
  echo "$falhas serviço(s) sem imagem publicada: o deploy pelo GitHub falharia no meio da troca."
  exit 1
fi
echo "Imagens do deploy ok."
