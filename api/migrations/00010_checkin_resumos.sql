-- +goose Up
-- Resumo diário do notificador de check-in (issue #17): em vez de uma mensagem por webhook, o
-- grupo recebe "Reservas Confirmadas para Amanhã" (17h) e "para Hoje" (08h), com todas juntas.
-- Só cria e acrescenta: o código anterior continua funcionando com este schema (rollback).

-- Situação atual de cada reserva (uma linha por id da reserva do PMS). O webhook atualiza; o
-- cancelamento muda o status e a reserva sai do resumo. Os dados de hóspede já estão no
-- payload de checkin.eventos; aqui ficam só os campos que a mensagem mostra.
CREATE TABLE checkin.reservas (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    reserva_id       text        NOT NULL UNIQUE,
    status           text        NOT NULL CHECK (status IN ('confirmada', 'cancelada')),
    check_in         date        NOT NULL,
    check_out        date,
    imovel           text,
    hospede          text,
    hospedes         integer,
    canal            text,
    telefone         text,
    -- Evento que deu a situação atual: um evento mais antigo (reprocessado) não sobrescreve.
    ultimo_evento_id bigint      NOT NULL REFERENCES checkin.eventos (id),
    criada_em        timestamptz NOT NULL DEFAULT now(),
    atualizada_em    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX reservas_check_in_idx ON checkin.reservas (check_in) WHERE status = 'confirmada';

-- Resumos já montados, um por tipo e dia de check-in: nunca sai duas vezes, nem com reinício.
-- Dia sem reserva também fica registrado (sem mensagem), e é por aqui que o webhook sabe que
-- uma reserva chegou tarde demais para o resumo e precisa de mensagem avulsa.
CREATE TABLE checkin.resumos (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tipo      text        NOT NULL CHECK (tipo IN ('hoje', 'amanha')),
    data      date        NOT NULL,
    reservas  integer     NOT NULL,
    criado_em timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tipo, data)
);

-- A mensagem do resumo passa pela mesma fila (tentativas, proteção contra envio duplicado),
-- mas não vem de um evento só.
ALTER TABLE checkin.mensagens
    ALTER COLUMN evento_id DROP NOT NULL,
    ADD COLUMN resumo_id bigint REFERENCES checkin.resumos (id),
    ADD CONSTRAINT mensagens_origem_check CHECK (evento_id IS NOT NULL OR resumo_id IS NOT NULL);

-- Evento já tratado (virou mensagem avulsa ou entrou na reserva do resumo). O webhook descarta
-- como duplicado o evento repetido já processado; antes, só o que tinha virado mensagem.
ALTER TABLE checkin.eventos ADD COLUMN processado_em timestamptz;

GRANT SELECT, INSERT, UPDATE ON checkin.reservas, checkin.resumos TO checkin;

-- +goose Down
ALTER TABLE checkin.eventos DROP COLUMN processado_em;
-- As mensagens de resumo não têm evento: sem elas o NOT NULL volta.
DELETE FROM checkin.mensagens WHERE evento_id IS NULL;
ALTER TABLE checkin.mensagens
    DROP CONSTRAINT mensagens_origem_check,
    DROP COLUMN resumo_id,
    ALTER COLUMN evento_id SET NOT NULL;
DROP TABLE checkin.resumos;
DROP TABLE checkin.reservas;
