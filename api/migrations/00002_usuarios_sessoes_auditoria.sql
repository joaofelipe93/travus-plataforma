-- +goose Up
CREATE TYPE perfil_usuario AS ENUM ('admin', 'operador', 'leitura');

CREATE TABLE usuarios (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         citext         NOT NULL UNIQUE,
    nome          text           NOT NULL,
    senha_hash    text           NOT NULL,
    perfil        perfil_usuario NOT NULL,
    ativo         boolean        NOT NULL DEFAULT true,
    criado_em     timestamptz    NOT NULL DEFAULT now(),
    atualizado_em timestamptz    NOT NULL DEFAULT now()
);

-- O token da sessão só existe no cookie do navegador; aqui fica o SHA-256 dele.
-- Validade: 12 h sem uso e no máximo 7 dias desde a criação (regras na API).
CREATE TABLE sessoes (
    token_hash    bytea       PRIMARY KEY,
    usuario_id    bigint      NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    csrf_token    text        NOT NULL,
    criada_em     timestamptz NOT NULL DEFAULT now(),
    ultimo_uso_em timestamptz NOT NULL DEFAULT now(),
    ip            text,
    user_agent    text
);
CREATE INDEX sessoes_usuario_id_idx ON sessoes (usuario_id);

CREATE TABLE auditoria (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    usuario_id  bigint      REFERENCES usuarios (id) ON DELETE SET NULL,
    acao        text        NOT NULL,
    entidade    text,
    entidade_id text,
    detalhes    jsonb       NOT NULL DEFAULT '{}',
    ip          text,
    criado_em   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auditoria_criado_em_idx ON auditoria (criado_em DESC);

-- +goose Down
DROP TABLE auditoria;
DROP TABLE sessoes;
DROP TABLE usuarios;
DROP TYPE perfil_usuario;
