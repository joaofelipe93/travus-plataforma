#!/bin/sh
# Trava de segurança: neste container o script legado (src/index.js) só roda em dry-run.
# Lance real só vai existir no worker da plataforma (Etapa 3), com a proteção contra
# lance duplicado, e nunca por linha de comando.
set -eu

for arg in "$@"; do
  case "$arg" in
    --confirm | --confirm=* | real)
      echo "BLOQUEADO: este container não registra lance real ($arg). Use --dry-run." >&2
      exit 64
      ;;
  esac
done

exec "$@"
