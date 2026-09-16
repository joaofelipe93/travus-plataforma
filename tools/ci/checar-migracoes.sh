#!/usr/bin/env bash
# CI: migrações seguras para rollback. O deploy volta o código, mas não desfaz o banco, então
# uma versão anterior precisa continuar funcionando com o banco já migrado. Regras:
#   1. migração já existente em <base> não pode ser editada, renomeada nem apagada;
#   2. migração nova tem número maior que todas as de <base> e sem repetição;
#   3. o "Up" de migração nova não pode destruir nem renomear (DROP TABLE/COLUMN/SCHEMA,
#      RENAME, ALTER ... TYPE, TRUNCATE, DELETE FROM). Faça em duas versões: primeiro
#      acrescente e pare de usar; só numa versão seguinte remova. Exceção consciente: uma
#      linha "-- ci: destrutiva-aprovada: <motivo>" na migração (aparece no PR para revisão).
#
#   bash tools/ci/checar-migracoes.sh <base>     (ex.: origin/main)
set -euo pipefail
cd "$(dirname "$0")/../.."

base=${1:?uso: checar-migracoes.sh <commit-ou-branch-base>}
dir=api/migrations
falhas=0
falha() { echo "x $*"; falhas=$((falhas + 1)); }

while IFS=$'\t' read -r status arquivo destino; do
  case "$arquivo" in $dir/*.sql) ;; *) continue ;; esac
  case "$status" in
    A) ;;
    *) falha "$arquivo: migração existente foi alterada ($status). Crie uma migração nova em vez de editar." ;;
  esac
done < <(git diff --name-status "$base" -- "$dir")

maior_base=$(git ls-tree --name-only "$base" "$dir/" | sed -nE 's#.*/([0-9]+)_[^/]*\.sql$#\1#p' | sort -n | tail -1)
maior_base=$((10#${maior_base:-0}))

numeros=$(ls "$dir" | sed -nE 's/^([0-9]+)_.*\.sql$/\1/p' | sort)
repetidos=$(printf '%s\n' "$numeros" | uniq -d)
[ -z "$repetidos" ] || falha "números de migração repetidos: $(echo $repetidos) (dois PRs criaram a mesma versão: renumere a sua)"

for novo in $(git diff --name-only --diff-filter=A "$base" -- "$dir" | grep -E '\.sql$' || true); do
  numero=$(basename "$novo" | sed -nE 's/^([0-9]+)_.*/\1/p')
  if [ -z "$numero" ] || [ $((10#$numero)) -le "$maior_base" ]; then
    falha "$novo: o número precisa ser maior que $maior_base (a última migração em $base)"
  fi
  if grep -q -- '-- ci: destrutiva-aprovada:' "$novo"; then
    echo "! $novo: marcada como destrutiva aprovada ($(grep -- '-- ci: destrutiva-aprovada:' "$novo" | head -1 | sed 's/.*aprovada: *//')). Revise com cuidado."
    continue
  fi
  destrutivo=$(sed '/-- +goose Down/,$d' "$novo" | grep -v '^\s*--' | grep -inE 'DROP[[:space:]]+(TABLE|COLUMN|SCHEMA|TYPE|VIEW|FUNCTION)|RENAME|ALTER[[:space:]]+COLUMN[^;]*[[:space:]]TYPE[[:space:]]|TRUNCATE|DELETE[[:space:]]+FROM' || true)
  [ -z "$destrutivo" ] || falha "$novo: comando destrutivo no Up (o rollback do código quebraria): $(echo "$destrutivo" | head -3 | tr '\n' ';')"
done

if [ "$falhas" -gt 0 ]; then
  echo "$falhas problema(s) nas migrações."
  exit 1
fi
echo "Migrações ok (base: $base, última migração da base: $maior_base)."
