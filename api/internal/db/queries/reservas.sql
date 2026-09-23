-- Reservas recebidas pelo notificador de check-in (tela Reservas). O payload vem cru do PMS:
-- quem interpreta os campos e junta reserva e cancelamento é a API (reservas.go). A mensagem
-- mais recente de cada evento diz se o aviso saiu no grupo. Sem mensagem própria: 'resumo' se o
-- evento foi processado (a reserva entrou no resumo diário, migração 00010) e '' se foi gravado
-- enquanto não havia grupo escolhido.

-- name: ListarEventosReserva :many
SELECT e.id, e.payload, e.recebido_em,
       COALESCE(m.status, CASE WHEN e.processado_em IS NOT NULL THEN 'resumo' ELSE '' END)::text AS mensagem_status
FROM checkin.eventos e
LEFT JOIN LATERAL (
    SELECT status FROM checkin.mensagens WHERE evento_id = e.id ORDER BY id DESC LIMIT 1
) m ON true
ORDER BY e.id DESC
LIMIT sqlc.arg(limite)::int;
