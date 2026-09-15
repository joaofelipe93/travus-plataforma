-- name: CriarUsuario :one
INSERT INTO usuarios (email, nome, senha_hash, perfil)
VALUES (@email, @nome, @senha_hash, @perfil)
RETURNING id, email, nome, perfil, ativo, criado_em;

-- name: BuscarUsuarioPorEmail :one
SELECT id, email, nome, senha_hash, perfil, ativo
FROM usuarios
WHERE email = @email;

-- name: ListarUsuarios :many
SELECT id, email, nome, perfil, ativo, criado_em
FROM usuarios
ORDER BY email;

-- name: AtualizarSenhaUsuario :execrows
UPDATE usuarios
SET senha_hash = @senha_hash, atualizado_em = now()
WHERE email = @email;

-- name: DefinirUsuarioAtivo :execrows
UPDATE usuarios
SET ativo = @ativo, atualizado_em = now()
WHERE email = @email;

-- name: RegistrarAuditoria :exec
INSERT INTO auditoria (usuario_id, acao, entidade, entidade_id, detalhes, ip)
VALUES (sqlc.narg(usuario_id), @acao, sqlc.narg(entidade), sqlc.narg(entidade_id), @detalhes, sqlc.narg(ip));
