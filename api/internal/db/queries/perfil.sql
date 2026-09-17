-- name: BuscarPerfil :one
SELECT id, email, nome, perfil, ativo, telefone, cargo, observacoes, criado_em, atualizado_em
FROM usuarios
WHERE id = @id;

-- name: AtualizarPerfil :one
UPDATE usuarios
SET nome = @nome,
    telefone = sqlc.narg(telefone),
    cargo = sqlc.narg(cargo),
    observacoes = sqlc.narg(observacoes),
    atualizado_em = now()
WHERE id = @id
RETURNING id, email, nome, perfil, ativo, telefone, cargo, observacoes, criado_em, atualizado_em;

-- name: ListarCredenciaisDoUsuario :many
SELECT credencial, dica, criado_em, atualizado_em
FROM credenciais_usuario
WHERE usuario_id = @usuario_id
ORDER BY credencial;

-- name: BuscarCredencialDoUsuario :one
SELECT dados_cifrados, dica, criado_em, atualizado_em
FROM credenciais_usuario
WHERE usuario_id = @usuario_id AND credencial = @credencial;

-- name: GravarCredencialDoUsuario :one
INSERT INTO credenciais_usuario (usuario_id, credencial, dados_cifrados, dica)
VALUES (@usuario_id, @credencial, @dados_cifrados, @dica)
ON CONFLICT (usuario_id, credencial) DO UPDATE
    SET dados_cifrados = EXCLUDED.dados_cifrados,
        dica = EXCLUDED.dica,
        atualizado_em = now()
RETURNING credencial, dica, criado_em, atualizado_em;

-- name: ApagarCredencialDoUsuario :execrows
DELETE FROM credenciais_usuario
WHERE usuario_id = @usuario_id AND credencial = @credencial;

-- name: ListarUsuariosComCredencial :many
-- Visão do admin: uma linha por usuário ativo, com ou sem a credencial (nunca o valor).
SELECT u.id, u.email, u.nome, u.perfil, (c.usuario_id IS NOT NULL)::boolean AS cadastrada, c.atualizado_em
FROM usuarios u
LEFT JOIN credenciais_usuario c ON c.usuario_id = u.id AND c.credencial = @credencial
WHERE u.ativo
ORDER BY u.nome, u.email;
