-- Checagens do vigia (alertas por e-mail).

-- name: VigiaFila :one
SELECT (count(*) FILTER (WHERE status = 'na_fila' AND criada_em < now() - interval '30 minutes'))::int     AS na_fila_antigas,
       (count(*) FILTER (WHERE status = 'em_andamento' AND trava_ate < now() - interval '5 minutes'))::int AS travas_vencidas
FROM execucoes;

-- Cotas que clicaram em Confirmar e não tiveram resultado confirmado, nas últimas 24 h.
-- name: VigiaCotasAposConfirmar :many
SELECT id, execucao_id, grupo, cota, versao, erro
FROM execucao_cotas
WHERE status = 'erro_apos_confirmar' AND iniciada_em > now() - interval '24 hours'
ORDER BY id;

-- name: VigiaDrive :one
SELECT (count(*) FILTER (WHERE drive_status = 'erro' AND drive_tentativas >= 5))::int AS desistidos,
       (count(*) FILTER (WHERE drive_status IN ('pendente', 'erro') AND drive_tentativas < 5
                           AND atualizado_em < now() - interval '1 hour'))::int     AS atrasados
FROM lances;
