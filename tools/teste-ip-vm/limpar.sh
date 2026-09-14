#!/usr/bin/env bash
# Desfaz o teste de login feito sem Docker nesta VM:
#   1. remove os pacotes do sistema (apt) que o teste instalou para o Chromium,
#      conforme a lista gravada durante a instalação;
#   2. apaga a pasta do teste inteira (node_modules, Chromium, cache do npm, scripts).
#
# Uso na VM:
#   bash /root/teste-ip-vm/limpar.sh --simular   # só verifica e mostra o que faria
#   bash /root/teste-ip-vm/limpar.sh             # verifica e, se estiver tudo seguro, remove
#
# Segurança (há outra aplicação rodando nesta VM com PM2):
#   - NÃO mexe em PM2, Node, /root/.npm nem em nada fora da pasta e da lista de pacotes.
#   - ABORTA sem remover nada se algum processo em execução usar bibliotecas desses pacotes,
#     ou se a remoção for levar junto algum pacote que não foi instalado pelo teste.
#   - Não desfaz a chave SSH autorizada em /root/.ssh.

set -euo pipefail
export DEBIAN_FRONTEND=noninteractive LC_ALL=C

DIR=/root/teste-ip-vm
LISTA="$DIR/pacotes-instalados-pelo-teste.txt"
SIMULAR=0
[[ "${1:-}" == "--simular" ]] && SIMULAR=1

if [[ -s "$LISTA" ]]; then
  echo "== Verificando $(wc -l < "$LISTA") pacote(s) instalados pelo teste"

  # 1) Algum processo em execução (ex.: a aplicação do PM2) usa bibliotecas desses pacotes?
  LIBS=$(xargs -a "$LISTA" dpkg -L 2>/dev/null | grep -E '\.so(\.|$)' | sort -u || true)
  PIDS=""
  if [[ -n "$LIBS" ]]; then
    PIDS=$(grep -lFf <(printf '%s\n' "$LIBS") /proc/[0-9]*/maps 2>/dev/null | cut -d/ -f3 | sort -un || true)
  fi
  if [[ -n "$PIDS" ]]; then
    echo "ABORTADO: processos em execução usam bibliotecas desses pacotes:"
    for p in $PIDS; do ps -o pid=,user=,args= -p "$p" 2>/dev/null | cut -c1-150 | sed 's/^/  /'; done
    echo "Nada foi removido."
    exit 1
  fi
  echo "  ok: nenhum processo em execução usa essas bibliotecas"

  # 2) A remoção levaria junto algum pacote que NÃO foi instalado pelo teste?
  EXTRA=$(xargs -a "$LISTA" apt-get -s purge 2>/dev/null | awk '/^Purg /{print $2}' | sort -u \
    | comm -23 - <(sort -u "$LISTA") || true)
  if [[ -n "$EXTRA" ]]; then
    echo "ABORTADO: a remoção levaria junto pacotes que já existiam antes do teste:"
    printf '%s\n' "$EXTRA" | sed 's/^/  /'
    echo "Nada foi removido."
    exit 1
  fi
  echo "  ok: a remoção afeta só pacotes da lista do teste"
else
  echo "== Nenhum pacote do sistema registrado pelo teste."
fi

echo "== Pasta a apagar: $DIR ($(du -sh "$DIR" | cut -f1))"

if [[ $SIMULAR -eq 1 ]]; then
  echo "== Simulação: nada foi removido. Rode sem --simular para limpar."
  exit 0
fi

if [[ -s "$LISTA" ]]; then
  echo "== Removendo os pacotes do teste..."
  xargs -a "$LISTA" apt-get purge -y
  apt-get clean
fi

cd /
rm -rf "$DIR"
echo "== Pronto: pacotes do teste removidos e pasta $DIR apagada."
