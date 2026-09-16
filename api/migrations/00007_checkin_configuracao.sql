-- +goose Up
-- Grupo do WhatsApp que recebe as notificações, escolhido pelo admin na tela (a variável
-- CHECKIN_WHATSAPP_GROUP_JID vira só o valor usado enquanto ninguém escolheu). Uma linha só.
CREATE TABLE checkin.configuracao (
    id            boolean     PRIMARY KEY DEFAULT true CHECK (id),
    grupo_jid     text        NOT NULL CHECK (grupo_jid ~ '^[0-9-]+@g\.us$'),
    grupo_nome    text        NOT NULL,
    atualizado_em timestamptz NOT NULL DEFAULT now()
);

GRANT SELECT, INSERT, UPDATE ON checkin.configuracao TO checkin;

-- +goose Down
DROP TABLE checkin.configuracao;
-- teste da CI: editar migração existente deve reprovar
