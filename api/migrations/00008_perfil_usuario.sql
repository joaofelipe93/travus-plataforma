-- +goose Up
-- Perfil de quem usa a plataforma: dados de contato para acionar a pessoa e credenciais
-- pessoais de serviços de terceiros (ex.: Trello). O cadastro de usuário continua no
-- terminal (make usuario); aqui a pessoa cuida só dos próprios dados.
ALTER TABLE usuarios
    ADD COLUMN telefone    text,
    ADD COLUMN cargo       text,
    ADD COLUMN observacoes text;

-- Uma credencial por pessoa e por tipo do catálogo (api/internal/credenciais). Os campos
-- (ex.: chave e token do Trello) vão num JSON cifrado com AES-256-GCM pela API
-- (CHAVE_CRIPTOGRAFIA), no contexto "credencial:<usuario_id>:<credencial>": o valor de uma
-- pessoa não decifra no lugar do de outra. A "dica" são os últimos caracteres do campo
-- principal, só para a pessoa reconhecer o que cadastrou; o valor nunca volta ao navegador.
CREATE TABLE credenciais_usuario (
    usuario_id     bigint      NOT NULL REFERENCES usuarios (id) ON DELETE CASCADE,
    credencial     text        NOT NULL,
    dados_cifrados bytea       NOT NULL,
    dica           text        NOT NULL DEFAULT '',
    criado_em      timestamptz NOT NULL DEFAULT now(),
    atualizado_em  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (usuario_id, credencial)
);

-- Visão do admin: quem já cadastrou uma credencial e quem ainda falta.
CREATE INDEX credenciais_usuario_credencial_idx ON credenciais_usuario (credencial);

-- +goose Down
DROP TABLE credenciais_usuario;
ALTER TABLE usuarios
    DROP COLUMN observacoes,
    DROP COLUMN cargo,
    DROP COLUMN telefone;
