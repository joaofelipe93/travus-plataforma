-- +goose Up
-- Etapa 0: só prepara o banco. citext será usado para e-mails sem diferenciar maiúsculas.
CREATE EXTENSION IF NOT EXISTS citext;

-- +goose Down
DROP EXTENSION IF EXISTS citext;
