-- name: ListarClientes :many
SELECT c.id, c.nome, c.telefone, c.email,
       count(q.id)::int                          AS total_cotas,
       (count(q.id) FILTER (WHERE q.ativa))::int AS cotas_ativas
FROM clientes c
LEFT JOIN cotas q ON q.cliente_id = c.id
WHERE sqlc.narg(busca)::text IS NULL
   OR c.nome ILIKE '%' || sqlc.narg(busca)::text || '%'
GROUP BY c.id
ORDER BY c.nome;

-- name: BuscarCliente :one
SELECT id, nome, telefone, email, origem, criado_em, atualizado_em
FROM clientes
WHERE id = @id;

-- name: ListarClientesParaImportacao :many
SELECT id, nome, nome_normalizado, telefone, email
FROM clientes;

-- name: CriarCliente :one
INSERT INTO clientes (nome, nome_normalizado, telefone, email, origem)
VALUES (@nome, @nome_normalizado, sqlc.narg(telefone), sqlc.narg(email), @origem)
RETURNING id;

-- Telefone/e-mail vazios na planilha não apagam o que já está no cadastro.
-- name: AtualizarContatoCliente :exec
UPDATE clientes
SET telefone      = COALESCE(sqlc.narg(telefone), telefone),
    email         = COALESCE(sqlc.narg(email), email),
    atualizado_em = now()
WHERE id = @id;
