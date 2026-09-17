#!/usr/bin/env bash
# Porta de entrada do deploy pelo GitHub Actions na VM. É o "command=" forçado da chave de deploy
# no authorized_keys do travus (instalado por `make configurar-ci`): quem tem essa chave só
# consegue pedir uma destas ações, nunca um terminal nem um comando qualquer.
#
#   publicar vX.Y.Z   baixa o código da TAG vX.Y.Z direto do GitHub (a chave não envia código:
#                     só escolhe entre versões já lançadas) e roda publicar.sh --imagens
#   voltar            publicar.sh --voltar (versão anterior)
#   estado            versão no ar e histórico
#
# Instalado em /opt/travus/bin/entrada-ci.sh (fora das releases). Códigos de saída: os do
# publicar.sh (3 = recusado sem mudar nada).
set -euo pipefail

REPO=joaofelipe93/travus-plataforma
BASE=/opt/travus
HISTORICO=$BASE/versoes-publicadas

recusar() { echo "recusado (nada foi alterado): $*" >&2; exit 3; }

read -r acao argumento resto <<< "${SSH_ORIGINAL_COMMAND:-}" || true
[ -z "${resto:-}" ] || recusar "argumentos demais"

case "${acao:-}" in
  publicar)
    [[ "${argumento:-}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || recusar "uso: publicar vX.Y.Z"
    destino=$BASE/releases/$argumento
    if [ ! -d "$destino" ]; then
      temporario=$(mktemp -d "$BASE/releases/.baixando-XXXXXX")
      trap 'rm -rf "$temporario"' EXIT
      echo "== Baixando o código da tag $argumento de github.com/$REPO"
      curl -fsSL --retry 3 "https://codeload.github.com/$REPO/tar.gz/refs/tags/$argumento" \
        | tar -xz -C "$temporario" --strip-components=1 \
        || recusar "a tag $argumento não existe ou o GitHub não respondeu"
      commit=$(curl -fsSL --retry 3 -H 'Accept: application/vnd.github+json' \
        "https://api.github.com/repos/$REPO/commits/$argumento" \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)["sha"][:12])') \
        || recusar "não consegui ler o commit da tag $argumento no GitHub"
      echo "$commit" > "$temporario/COMMIT"
      mv "$temporario" "$destino"
      trap - EXIT
    fi
    exec bash "$destino/deploy/vm/publicar.sh" --imagens "$argumento"
    ;;
  voltar)
    [ -z "${argumento:-}" ] || recusar "uso: voltar"
    [ -e "$BASE/atual" ] || recusar "não há versão publicada"
    exec bash "$BASE/atual/deploy/vm/publicar.sh" --voltar
    ;;
  estado)
    [ -z "${argumento:-}" ] || recusar "uso: estado"
    echo "no ar: $(basename "$(readlink -f "$BASE/atual" 2> /dev/null || echo nenhuma)")"
    echo "últimas publicações:"
    tail -n 5 "$HISTORICO" 2> /dev/null | sed 's/^/  /' || true
    ;;
  *)
    recusar "ação desconhecida (use publicar vX.Y.Z, voltar ou estado)"
    ;;
esac
