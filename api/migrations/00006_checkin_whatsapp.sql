-- +goose Up
-- Notificador de check-in (workers/checkin-whatsapp): webhook de reserva do PMS → fila →
-- grupo do WhatsApp. Schema próprio e papel próprio: o serviço grava direto aqui e não
-- enxerga o resto da plataforma (clientes, cotas, lances, integrações).
CREATE SCHEMA checkin;

-- Todo webhook recebido, cru. A chave de deduplicação vem do id da reserva (com o status
-- quando não é confirmação) ou do SHA-256 do corpo: o provedor reenvia.
CREATE TABLE checkin.eventos (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    chave_dedup text        NOT NULL UNIQUE,
    origem      text        NOT NULL,
    payload     jsonb       NOT NULL,
    recebido_em timestamptz NOT NULL DEFAULT now()
);

-- Mensagens para o grupo. O webhook só enfileira; o laço do serviço envia quando o
-- WhatsApp está conectado (desconectado não gasta tentativa).
CREATE TABLE checkin.mensagens (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    evento_id            bigint      NOT NULL REFERENCES checkin.eventos (id),
    destino_jid          text        NOT NULL,
    texto                text        NOT NULL,
    status               text        NOT NULL DEFAULT 'pendente'
                                     CHECK (status IN ('pendente', 'enviada', 'falhou')),
    tentativas           integer     NOT NULL DEFAULT 0,
    proxima_tentativa_em timestamptz NOT NULL DEFAULT now(),
    ultimo_erro          text,
    enviada_em           timestamptz,
    criada_em            timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX mensagens_pendentes_idx ON checkin.mensagens (proxima_tentativa_em, id)
    WHERE status = 'pendente';
CREATE INDEX mensagens_evento_idx ON checkin.mensagens (evento_id);

-- Papel do serviço. Nasce sem login: `api migrate up` liga o login com a senha de
-- CHECKIN_DB_SENHA (a senha não fica na migração). O papel é do cluster inteiro, por isso
-- o "se não existir" (o banco de teste está no mesmo cluster).
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'checkin') THEN
        CREATE ROLE checkin NOLOGIN;
    END IF;
END
$$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA checkin TO checkin;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA checkin TO checkin;
ALTER DEFAULT PRIVILEGES IN SCHEMA checkin GRANT SELECT, INSERT, UPDATE ON TABLES TO checkin;

-- +goose Down
-- O papel fica (é do cluster e pode ter permissões em outro banco, como o de teste).
DROP SCHEMA checkin CASCADE;
