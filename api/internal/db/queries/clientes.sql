-- O cadastro é um só para a plataforma: a lista já diz em que serviços o cliente está ativo
-- (migração 00018) e aceita o filtro por serviço, que é como cada espaço mostra só a carteira
-- dele. As contagens são DISTINCT porque agora há duas junções que multiplicam linhas.
-- name: ListarClientes :many
SELECT c.id, c.nome, c.telefone, c.email, c.cpf_cnpj,
       count(DISTINCT q.id)::int                          AS total_cotas,
       (count(DISTINCT q.id) FILTER (WHERE q.ativa))::int AS cotas_ativas,
       coalesce(array_agg(DISTINCT cs.servico) FILTER (WHERE cs.ativo), '{}')::text[] AS servicos
FROM clientes c
LEFT JOIN cotas q ON q.cliente_id = c.id
LEFT JOIN cliente_servicos cs ON cs.cliente_id = c.id
WHERE (sqlc.narg(busca)::text IS NULL
       OR c.nome ILIKE '%' || sqlc.narg(busca)::text || '%')
  AND (sqlc.narg(servico)::text IS NULL
       OR EXISTS (SELECT 1 FROM cliente_servicos a
                  WHERE a.cliente_id = c.id AND a.servico = sqlc.narg(servico)::text AND a.ativo))
GROUP BY c.id
ORDER BY c.nome;

-- name: ListarServicosDoCliente :many
SELECT cs.servico, cs.ativo, cs.atualizado_em, u.nome AS alterado_por_nome
FROM cliente_servicos cs
LEFT JOIN usuarios u ON u.id = cs.alterado_por
WHERE cs.cliente_id = @cliente_id
ORDER BY cs.servico;

-- Ligar e desligar passam pelo mesmo caminho: a linha nasce na primeira vez e depois só muda.
-- name: DefinirServicoDoCliente :one
INSERT INTO cliente_servicos (cliente_id, servico, ativo, alterado_por)
VALUES (@cliente_id, @servico, @ativo, sqlc.narg(alterado_por))
ON CONFLICT (cliente_id, servico) DO UPDATE
SET ativo         = excluded.ativo,
    alterado_por  = excluded.alterado_por,
    atualizado_em = now()
RETURNING servico, ativo, atualizado_em;

-- name: ContarCotasAtivasDoCliente :one
SELECT count(*)::int FROM cotas WHERE cliente_id = @cliente_id AND ativa;

-- name: BuscarCliente :one
SELECT id, nome, telefone, email, cpf_cnpj, origem, criado_em, atualizado_em
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

-- No CRM o cadastro é digitado: telefone/e-mail em branco no formulário apagam o que estava lá
-- (diferente da importação, em que coluna vazia não mexia no cadastro).
-- name: CriarClienteCadastro :one
INSERT INTO clientes (nome, nome_normalizado, telefone, email, cpf_cnpj, origem)
VALUES (@nome, @nome_normalizado, sqlc.narg(telefone), sqlc.narg(email), sqlc.narg(cpf_cnpj), 'cadastro')
RETURNING id, nome, telefone, email, cpf_cnpj, origem, criado_em, atualizado_em;

-- name: AtualizarCliente :one
UPDATE clientes
SET nome             = @nome,
    nome_normalizado = @nome_normalizado,
    telefone         = sqlc.narg(telefone),
    email            = sqlc.narg(email),
    cpf_cnpj         = sqlc.narg(cpf_cnpj),
    atualizado_em    = now()
WHERE id = @id
RETURNING id, nome, telefone, email, cpf_cnpj, origem, criado_em, atualizado_em;

-- name: ExcluirCliente :execrows
DELETE FROM clientes WHERE id = @id;

-- name: ContarCotasDoCliente :one
SELECT count(*)::int FROM cotas WHERE cliente_id = @cliente_id;
