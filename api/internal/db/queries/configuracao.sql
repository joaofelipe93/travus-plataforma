-- Configuração do serviço Canopus: uma linha só, criada pela migração 00012.

-- name: GarantirConfiguracaoCanopus :exec
INSERT INTO configuracao_canopus (id) VALUES (true) ON CONFLICT (id) DO NOTHING;

-- name: BuscarConfiguracaoCanopus :one
SELECT c.*, u.nome AS atualizado_por_nome
FROM configuracao_canopus c
LEFT JOIN usuarios u ON u.id = c.atualizado_por
WHERE c.id;

-- name: AtualizarConfiguracaoCanopus :exec
UPDATE configuracao_canopus SET
    dry_run_automatico        = sqlc.arg(dry_run_automatico),
    dry_run_dia               = sqlc.arg(dry_run_dia),
    dry_run_hora              = sqlc.arg(dry_run_hora),
    dry_run_minuto            = sqlc.arg(dry_run_minuto),
    aviso_email               = sqlc.arg(aviso_email),
    drive_pasta_id            = sqlc.narg(drive_pasta_id),
    validade_dry_run_minutos  = sqlc.arg(validade_dry_run_minutos),
    retencao_screenshots_dias = sqlc.arg(retencao_screenshots_dias),
    atualizado_por            = sqlc.narg(atualizado_por),
    atualizado_em             = now()
WHERE id;

-- O laço do agendamento trava a linha para decidir sozinho se cria a execução do mês.
-- name: TravarConfiguracaoCanopus :one
SELECT * FROM configuracao_canopus WHERE id FOR UPDATE;

-- name: MarcarDisparoDryRun :exec
UPDATE configuracao_canopus SET
    ultimo_disparo_dia       = sqlc.arg(ultimo_disparo_dia),
    ultimo_disparo_em        = now(),
    ultimo_disparo_resultado = sqlc.arg(ultimo_disparo_resultado)
WHERE id;
