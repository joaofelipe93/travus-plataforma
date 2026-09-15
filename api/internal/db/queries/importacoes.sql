-- name: CriarImportacao :one
INSERT INTO importacoes (criada_por, arquivo_nome, arquivo_sha256, linhas, previa)
VALUES (@criada_por, @arquivo_nome, @arquivo_sha256, @linhas, @previa)
RETURNING id, status, criada_em;

-- name: BuscarImportacao :one
SELECT i.id, i.criada_por, u.nome AS criada_por_nome, i.arquivo_nome, i.arquivo_sha256,
       i.status, i.previa, i.criada_em, i.finalizada_em
FROM importacoes i
JOIN usuarios u ON u.id = i.criada_por
WHERE i.id = @id;

-- name: TravarImportacao :one
SELECT id, status, arquivo_nome, linhas, previa
FROM importacoes
WHERE id = @id
FOR UPDATE;

-- Serializa aplicações de importação (duas ao mesmo tempo disputariam as mesmas cotas).
-- name: TravarAplicacaoDeImportacoes :exec
SELECT pg_advisory_xact_lock(7301);

-- name: FinalizarImportacao :exec
UPDATE importacoes
SET status = @status, finalizada_em = now()
WHERE id = @id;

-- name: ListarImportacoes :many
SELECT i.id, u.nome AS criada_por_nome, i.arquivo_nome, i.status,
       i.previa -> 'totais' AS totais, i.criada_em, i.finalizada_em
FROM importacoes i
JOIN usuarios u ON u.id = i.criada_por
ORDER BY i.criada_em DESC
LIMIT 20;
