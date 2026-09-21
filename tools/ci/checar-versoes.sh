#!/usr/bin/env bash
# CI: versões que precisam ser iguais em arquivos diferentes. Sem isto, uma atualização
# automática (Dependabot) ou manual passa em um lugar e esquece o outro, e o erro só aparece
# no build ou, pior, em produção.
#
#   1. Node: web/Dockerfile, workers/checkin-whatsapp/Dockerfile e NODE_VERSION da CI;
#   2. Go: api/Dockerfile, o serviço api-teste do compose e GO_VERSION da CI (e a linha "go"
#      do go.mod não pode ser maior);
#   3. Playwright: a tag da imagem do worker Canopus e o pacote no package-lock.json
#      (a imagem traz os navegadores; versões diferentes quebram o worker no Newcon).
#
#   bash tools/ci/checar-versoes.sh
set -euo pipefail
cd "$(dirname "$0")/../.."

falhas=0
conferir() { # descrição esperado obtido onde
  if [ "$2" = "$3" ]; then
    echo "✓ $1: $2"
  else
    echo "x $1: $4 tem $3, esperado $2"
    falhas=$((falhas + 1))
  fi
}

ci_node=$(sed -nE 's/^ *NODE_VERSION: *"([^"]+)".*/\1/p' .github/workflows/ci.yml | head -1)
ci_go=$(sed -nE 's/^ *GO_VERSION: *"([^"]+)".*/\1/p' .github/workflows/ci.yml | head -1)
[ -n "$ci_node" ] && [ -n "$ci_go" ] || { echo "x não achei NODE_VERSION/GO_VERSION em .github/workflows/ci.yml"; exit 1; }

for arquivo in web/Dockerfile workers/checkin-whatsapp/Dockerfile; do
  for versao in $(sed -nE 's/^FROM node:([0-9.]+)-.*/\1/p' "$arquivo" | sort -u); do
    conferir "Node da CI e do $arquivo" "$ci_node" "$versao" "$arquivo"
  done
done

versao_go_imagem=$(sed -nE 's/^FROM golang:([0-9.]+)-.*/\1/p' api/Dockerfile | head -1)
conferir "Go da CI e do api/Dockerfile" "$ci_go" "$versao_go_imagem" "api/Dockerfile"

go_compose=$(sed -nE 's/^ *image: golang:([0-9.]+)-.*/\1/p' deploy/docker-compose.yml | head -1)
conferir "Go da CI e do serviço api-teste" "$ci_go" "$go_compose" "deploy/docker-compose.yml"

# A linha "go" do go.mod é o piso da linguagem: não pode pedir mais do que a imagem/CI usam.
go_mod=$(sed -nE 's/^go ([0-9.]+).*/\1/p' api/go.mod | head -1)
maior=$(printf '%s\n%s\n' "$go_mod" "$ci_go" | sort -V | tail -1)
if [ "$maior" = "$go_mod" ] && [ "$go_mod" != "$ci_go" ]; then
  echo "x api/go.mod pede Go $go_mod, mais novo que o $ci_go da CI e da imagem"
  falhas=$((falhas + 1))
else
  echo "✓ go.mod ($go_mod) cabe no Go $ci_go"
fi

pw_imagem=$(sed -nE 's#^FROM mcr\.microsoft\.com/playwright:v([0-9.]+)-.*#\1#p' workers/canopus/Dockerfile | head -1)
pw_pacote=$(node -p "require('./workers/canopus/package-lock.json').packages['node_modules/playwright'].version" 2> /dev/null || echo "?")
conferir "Playwright da imagem e do package-lock do worker Canopus" "$pw_pacote" "$pw_imagem" "workers/canopus/Dockerfile"

if [ "$falhas" -gt 0 ]; then
  echo "$falhas versão(ões) fora de sincronia."
  exit 1
fi
