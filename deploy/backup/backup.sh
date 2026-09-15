#!/bin/sh
# Backup do Postgres na própria VM (decisão do usuário; a cópia para fora é manual:
# make backup-baixar). Roda no container "backup" (imagem postgres, variáveis PG*).
#
#   backup.sh agora               um backup já (make backup)
#   backup.sh diario              laço: um backup por dia, a partir de BACKUP_HORA (padrão 3 h)
#   backup.sh testar-restauracao  restaura o último backup num banco temporário (make restaurar-teste)
#
# Em /backups: diario/ (7), semanal/ (4, domingos), mensal/ (6, dia 1), arquivos .dump
# (pg_dump -Fc) só para o dono. O vigia da API lê ultimo-ok e ultimo-erro.
set -eu
DIR=/backups

log() { echo "$(date '+%Y-%m-%d %H:%M:%S') $*"; }

registrar_erro() {
  (umask 022 && printf '%s %s\n' "$(date -Iseconds)" "$1" > "$DIR/ultimo-erro")
  log "ERRO: $1"
}

manter() { # pasta quantidade
  ls -1 "$DIR/$1"/travus-*.dump 2>/dev/null | sort -r | tail -n +"$(($2 + 1))" | while read -r velho; do
    rm -f "$velho"
    log "apagado $velho"
  done
}

fazer_backup() {
  hoje=$(date +%F)
  (umask 077 && mkdir -p "$DIR/diario" "$DIR/semanal" "$DIR/mensal")
  destino="$DIR/diario/travus-$hoje.dump"
  parcial="$destino.parcial"
  if ! (umask 077 && pg_dump -Fc -Z 6 -f "$parcial"); then
    rm -f "$parcial"
    registrar_erro "pg_dump falhou"
    return 1
  fi
  if ! pg_restore -l "$parcial" > /dev/null; then
    rm -f "$parcial"
    registrar_erro "o arquivo gerado pelo pg_dump não é um backup válido"
    return 1
  fi
  mv "$parcial" "$destino"
  if [ "$(date +%u)" = 7 ]; then cp -p "$destino" "$DIR/semanal/"; fi
  if [ "$(date +%d)" = 01 ]; then cp -p "$destino" "$DIR/mensal/"; fi
  manter diario 7
  manter semanal 4
  manter mensal 6
  (umask 022 && date -Iseconds > "$DIR/ultimo-ok")
  log "backup ok: $destino ($(du -h "$destino" | cut -f1))"
}

diario() {
  hora=${BACKUP_HORA:-3}
  log "backup diário a partir das ${hora} h, em $DIR"
  while true; do
    if [ ! -f "$DIR/diario/travus-$(date +%F).dump" ] && [ "$(expr "$(date +%H)" + 0)" -ge "$hora" ]; then
      fazer_backup || true
    fi
    sleep 600
  done
}

testar_restauracao() {
  ultimo=$(ls -1 "$DIR"/diario/travus-*.dump 2>/dev/null | sort -r | head -n 1)
  if [ -z "$ultimo" ]; then
    log "nenhum backup em $DIR/diario (rode make backup)"
    exit 1
  fi
  teste=travus_restauracao_teste
  dropdb --if-exists "$teste"
  createdb "$teste"
  trap 'dropdb --if-exists "$teste" > /dev/null 2>&1 || true' EXIT
  pg_restore --no-owner --exit-on-error -d "$teste" "$ultimo"
  contagem="SELECT (SELECT count(*) FROM clientes) || ' clientes, ' || (SELECT count(*) FROM cotas) || ' cotas, '
    || (SELECT count(*) FROM execucoes) || ' execuções, ' || (SELECT count(*) FROM lances) || ' lances, '
    || (SELECT count(*) FROM arquivos) || ' arquivos, migração ' || (SELECT max(version_id) FROM goose_db_version WHERE is_applied)"
  log "restaurado de $(basename "$ultimo"): $(psql -d "$teste" -tAc "$contagem")"
  log "banco em uso agora:            $(psql -tAc "$contagem")"
  log "restauração ok (o banco em uso pode ter mudado depois do backup; o banco temporário é apagado)"
}

case "${1:-}" in
  agora) fazer_backup ;;
  diario) diario ;;
  testar-restauracao) testar_restauracao ;;
  *)
    echo "uso: backup.sh agora | diario | testar-restauracao" >&2
    exit 2
    ;;
esac
