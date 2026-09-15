-- name: ListarCotas :many
SELECT q.id, q.cliente_id, c.nome AS cliente_nome, q.administradora, q.grupo, q.cota, q.versao,
       q.tipo_consorcio, q.modalidade_padrao, q.ativa, q.atualizado_em
FROM cotas q
JOIN clientes c ON c.id = q.cliente_id
WHERE (sqlc.narg(cliente_id)::bigint IS NULL OR q.cliente_id = sqlc.narg(cliente_id)::bigint)
  AND (sqlc.narg(grupo)::text IS NULL OR q.grupo = sqlc.narg(grupo)::text)
  AND (sqlc.narg(ativa)::boolean IS NULL OR q.ativa = sqlc.narg(ativa)::boolean)
  AND (sqlc.narg(busca)::text IS NULL
       OR c.nome ILIKE '%' || sqlc.narg(busca)::text || '%'
       OR q.grupo LIKE '%' || sqlc.narg(busca)::text || '%'
       OR q.cota LIKE '%' || sqlc.narg(busca)::text || '%')
ORDER BY c.nome, q.grupo, q.cota, q.versao;

-- name: ListarGrupos :many
SELECT DISTINCT grupo FROM cotas ORDER BY grupo;

-- name: DefinirCotaAtiva :one
UPDATE cotas
SET ativa = @ativa, atualizado_em = now()
WHERE id = @id
RETURNING id, grupo, cota, versao, ativa;

-- name: ListarCotasParaImportacao :many
SELECT q.id, q.cliente_id, c.nome AS cliente_nome, c.nome_normalizado AS cliente_nome_normalizado,
       q.administradora, q.grupo, q.cota, q.versao, q.tipo_consorcio, q.ativa, q.dados_planilha
FROM cotas q
JOIN clientes c ON c.id = q.cliente_id
ORDER BY q.administradora, q.grupo, q.cota, q.versao;

-- name: InserirCota :one
INSERT INTO cotas (cliente_id, administradora, grupo, cota, versao, tipo_consorcio, dados_planilha, importacao_id)
VALUES (@cliente_id, @administradora, @grupo, @cota, @versao, sqlc.narg(tipo_consorcio), @dados_planilha, @importacao_id)
RETURNING id;

-- name: AtualizarCotaImportada :exec
UPDATE cotas
SET cliente_id     = @cliente_id,
    tipo_consorcio = sqlc.narg(tipo_consorcio),
    dados_planilha = @dados_planilha,
    importacao_id  = @importacao_id,
    atualizado_em  = now()
WHERE id = @id;
