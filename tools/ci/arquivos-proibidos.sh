#!/usr/bin/env bash
# CI: falha se algum arquivo que nunca pode ir para o git (segredos, dados de clientes e de
# hóspedes, saídas do worker, backups) estiver versionado. O repositório é público.
#
#   bash tools/ci/arquivos-proibidos.sh
set -euo pipefail
cd "$(dirname "$0")/../.."

proibidos=$(git ls-files | grep -E \
  -e '(^|/)\.env(\.[^/]*)?$' \
  -e '(^|/)(token|credentials)\.json$' \
  -e '(^|/)client_secret[^/]*\.json$' \
  -e '(^|/)(cotas|cotasreal)\.csv$' \
  -e '\.(pdf|dump|pem|key|log|jsonl)$' \
  -e '(^|/)acme[^/]*\.json$' \
  -e '(^|/)(screenshots|downloads|logs)/' \
  -e '^deploy/(data|backups)/' \
  -e '^workers/checkin-whatsapp/data/' \
  -e '(^|/)auth_info/' \
  | grep -vE '(^|/)\.env\.(example|producao\.example)$' || true)

if [ -n "$proibidos" ]; then
  echo "Arquivos que não podem estar no git (segredos ou dados de clientes):"
  printf '  %s\n' $proibidos
  echo "Tire do índice com git rm --cached e confira o .gitignore. Se era segredo, troque o segredo."
  exit 1
fi
echo "Nenhum arquivo proibido versionado."
