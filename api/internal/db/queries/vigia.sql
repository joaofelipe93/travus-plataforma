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

-- Notificador de check-in: mensagens que desistiram (6 tentativas) nas últimas 24 h e
-- mensagens paradas na fila (o WhatsApp conectado mas nada sai).
-- name: VigiaCheckin :one
SELECT (count(*) FILTER (WHERE status = 'falhou' AND criada_em > now() - interval '24 hours'))::int     AS falharam,
       (count(*) FILTER (WHERE status = 'pendente' AND criada_em < now() - interval '30 minutes'))::int AS paradas
FROM checkin.mensagens;

-- Eventos que chegaram no webhook e não viraram nada: nem mensagem no grupo, nem reserva.
-- É o sinal de que o PMS está mandando um payload que o notificador não entende (outra
-- automação apontada para a mesma URL, ou um campo que mudou de nome). Sem isto o caso ficaria
-- invisível: desde 29/09/2026 esses eventos são guardados sem avisar o grupo.
-- Um reenvio de evento antigo (mais velho que a situação já gravada) também cai aqui.
-- name: VigiaCheckinIgnorados :one
SELECT count(*)::int AS ignorados
FROM checkin.eventos e
WHERE e.recebido_em > now() - interval '24 hours'
  AND NOT EXISTS (SELECT 1 FROM checkin.mensagens m WHERE m.evento_id = e.id)
  AND NOT EXISTS (SELECT 1 FROM checkin.reservas r WHERE r.ultimo_evento_id = e.id);
