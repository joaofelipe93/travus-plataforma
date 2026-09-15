-- name: CriarSessao :exec
INSERT INTO sessoes (token_hash, usuario_id, csrf_token, ip, user_agent)
VALUES (@token_hash, @usuario_id, @csrf_token, sqlc.narg(ip), sqlc.narg(user_agent));

-- Sessão válida: usuário ativo, criada depois de @criada_apos (limite de 7 dias)
-- e usada depois de @usada_apos (limite de 12 h sem uso).
-- name: BuscarSessaoValida :one
SELECT s.usuario_id, s.csrf_token, s.ultimo_uso_em, u.email, u.nome, u.perfil
FROM sessoes s
JOIN usuarios u ON u.id = s.usuario_id
WHERE s.token_hash = @token_hash
  AND u.ativo
  AND s.criada_em > @criada_apos
  AND s.ultimo_uso_em > @usada_apos;

-- name: TocarSessao :exec
UPDATE sessoes SET ultimo_uso_em = now() WHERE token_hash = @token_hash;

-- name: ApagarSessao :exec
DELETE FROM sessoes WHERE token_hash = @token_hash;

-- name: ApagarSessoesDoUsuario :execrows
DELETE FROM sessoes
WHERE usuario_id = (SELECT id FROM usuarios WHERE email = @email);

-- name: ApagarSessoesExpiradas :execrows
DELETE FROM sessoes
WHERE criada_em <= @criada_apos OR ultimo_uso_em <= @usada_apos;
