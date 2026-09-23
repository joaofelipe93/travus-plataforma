-- Reservas recebidas pelo notificador de check-in (tela Reservas). O payload vem cru do PMS:
-- quem interpreta os campos e junta reserva e cancelamento é a API (reservas.go). A mensagem
-- mais recente de cada evento diz se o aviso saiu no grupo ('' = evento sem mensagem, gravado
-- enquanto não havia grupo escolhido).

-- name: ListarEventosReserva :many
SELECT e.id, e.payload, e.recebido_em, COALESCE(m.status, '')::text AS mensagem_status
FROM checkin.eventos e
LEFT JOIN LATERAL (
    SELECT status FROM checkin.mensagens WHERE evento_id = e.id ORDER BY id DESC LIMIT 1
) m ON true
ORDER BY e.id DESC
LIMIT sqlc.arg(limite)::int;
