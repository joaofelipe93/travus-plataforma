-- Números do Canopus para o assistente da tela inicial (ferramenta resumo_canopus). Período
-- opcional: sem desde/ate, o histórico inteiro.

-- name: AssistenteCadastro :one
SELECT (SELECT count(*) FROM clientes)::int                        AS clientes,
       (SELECT count(*) FROM cotas)::int                           AS cotas,
       (SELECT count(*) FROM cotas WHERE ativa)::int               AS cotas_ativas;

-- name: AssistenteExecucoesPorTipo :many
SELECT tipo, status, count(*)::int AS quantidade
FROM execucoes
WHERE criada_em >= coalesce(sqlc.narg(desde)::timestamptz, '-infinity')
  AND criada_em <  coalesce(sqlc.narg(ate)::timestamptz, 'infinity')
GROUP BY tipo, status
ORDER BY tipo, status;

-- Cotas processadas no período (pela data da execução).
-- name: AssistenteCotasPorStatus :many
SELECT e.tipo, ec.status, count(*)::int AS quantidade
FROM execucao_cotas ec
JOIN execucoes e ON e.id = ec.execucao_id
WHERE e.criada_em >= coalesce(sqlc.narg(desde)::timestamptz, '-infinity')
  AND e.criada_em <  coalesce(sqlc.narg(ate)::timestamptz, 'infinity')
GROUP BY e.tipo, ec.status
ORDER BY e.tipo, ec.status;

-- Lances registrados no período (pela data de registro, ou de criação quando falta).
-- name: AssistenteLances :one
SELECT count(*)::int                                                   AS total,
       (count(*) FILTER (WHERE origem = 'plataforma'))::int            AS pela_plataforma,
       (count(*) FILTER (WHERE origem = 'historico'))::int             AS do_historico,
       (count(*) FILTER (WHERE pdf_id IS NOT NULL))::int               AS com_pdf,
       (count(*) FILTER (WHERE drive_status = 'enviado'))::int         AS drive_enviados,
       (count(*) FILTER (WHERE drive_status IN ('pendente', 'enviando')))::int AS drive_na_fila,
       (count(*) FILTER (WHERE drive_status = 'erro'))::int            AS drive_com_erro,
       (count(*) FILTER (WHERE drive_status = 'nao_enviar'))::int      AS drive_nao_enviar,
       (count(*) FILTER (WHERE drive_status = 'sem_pdf'))::int         AS drive_sem_pdf
FROM lances
WHERE coalesce(registrado_em, criado_em) >= coalesce(sqlc.narg(desde)::timestamptz, '-infinity')
  AND coalesce(registrado_em, criado_em) <  coalesce(sqlc.narg(ate)::timestamptz, 'infinity');

-- name: AssistenteUltimasExecucoes :many
SELECT e.id, e.tipo, e.status, e.criada_em, e.iniciada_em, e.finalizada_em, e.erro,
       u.nome AS criada_por,
       (SELECT count(*) FROM execucao_cotas ec WHERE ec.execucao_id = e.id)::int AS cotas,
       (SELECT count(*) FROM execucao_cotas ec WHERE ec.execucao_id = e.id
          AND ec.status IN ('erro_antes_confirmar', 'erro_apos_confirmar'))::int  AS cotas_com_erro
FROM execucoes e
JOIN usuarios u ON u.id = e.criada_por
ORDER BY e.id DESC
LIMIT 10;
