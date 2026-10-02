-- +goose Up
-- Configuração do serviço Canopus, numa linha só (como checkin.configuracao): o que antes era
-- variável de ambiente ou número fixo no código e quem opera precisa ajustar sem deploy.
--
-- O dry-run automático nasce DESLIGADO: ligá-lo faz o worker entrar no Newcon no dia e na hora
-- marcados (dry-run, que nunca confirma lance). Quem liga é admin ou operador, pela tela.
CREATE TABLE configuracao_canopus (
    id                        boolean     PRIMARY KEY DEFAULT true CHECK (id),
    -- Dry-run automático mensal, no fuso de Brasília. Mês sem o dia escolhido (31 em
    -- fevereiro) usa o último dia do mês.
    dry_run_automatico        boolean     NOT NULL DEFAULT false,
    dry_run_dia               smallint    NOT NULL DEFAULT 10 CHECK (dry_run_dia BETWEEN 1 AND 31),
    dry_run_hora              smallint    NOT NULL DEFAULT 8 CHECK (dry_run_hora BETWEEN 0 AND 23),
    dry_run_minuto            smallint    NOT NULL DEFAULT 0 CHECK (dry_run_minuto BETWEEN 0 AND 59),
    -- Resumo do dry-run automático por e-mail (vigia, ALERTA_*).
    aviso_email               boolean     NOT NULL DEFAULT true,
    -- Pasta do Drive dos comprovantes. Nulo: vale GOOGLE_DRIVE_PASTA_ID (valor inicial).
    drive_pasta_id            text        CHECK (drive_pasta_id <> ''),
    -- Prazo para aprovar um dry-run como lance real e validade dos screenshots (dados
    -- pessoais): eram fixos no código.
    validade_dry_run_minutos  integer     NOT NULL DEFAULT 120 CHECK (validade_dry_run_minutos BETWEEN 30 AND 1440),
    retencao_screenshots_dias integer     NOT NULL DEFAULT 30 CHECK (retencao_screenshots_dias BETWEEN 1 AND 365),
    atualizado_por            bigint      REFERENCES usuarios (id),
    atualizado_em             timestamptz NOT NULL DEFAULT now()
);

-- A linha nasce com os padrões: a API sempre encontra uma configuração.
INSERT INTO configuracao_canopus (id) VALUES (true);

-- O assistente responde sobre o agendamento e os prazos (nada aqui é segredo).
GRANT SELECT ON configuracao_canopus TO assistente;

-- +goose Down
DROP TABLE configuracao_canopus;
