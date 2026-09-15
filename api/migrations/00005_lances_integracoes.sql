-- +goose Up
-- Etapa 3: execução real (sempre a partir de um dry-run revisado) e reimpressão de
-- comprovante pelo Histórico. O lance real fica desligado por padrão (LANCE_REAL_HABILITADO).
ALTER TABLE execucoes
    DROP CONSTRAINT execucoes_tipo_check,
    ADD CONSTRAINT execucoes_tipo_check CHECK (tipo IN ('dry_run', 'real', 'reimpressao')),
    ADD COLUMN dry_run_origem_id bigint REFERENCES execucoes (id);

ALTER TABLE execucao_cotas
    DROP CONSTRAINT execucao_cotas_status_check,
    ADD CONSTRAINT execucao_cotas_status_check
        CHECK (status IN ('pendente', 'em_andamento', 'verificada', 'erro_antes_confirmar',
                          'confirmacao_iniciada', 'confirmada', 'erro_apos_confirmar',
                          'reimpressa', 'cancelada')),
    -- Real: data da assembleia vista no dry-run e aprovada na revisão. O worker aborta a
    -- cota se a tela mostrar outra assembleia.
    ADD COLUMN assembleia_aprovada text,
    -- Real: autorização explícita para registrar mesmo com lance já existente na assembleia.
    ADD COLUMN permitir_lance_existente boolean NOT NULL DEFAULT false,
    -- Reimpressão: protocolo a reimprimir. Real: protocolo capturado.
    ADD COLUMN protocolo text;

-- Lances registrados pela plataforma ou comprovantes obtidos do Histórico do Newcon.
CREATE TABLE lances (
    id                         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    cota_id                    bigint      NOT NULL REFERENCES cotas (id),
    execucao_cota_id           bigint      REFERENCES execucao_cotas (id),
    origem                     text        NOT NULL CHECK (origem IN ('plataforma', 'historico')),
    administradora             text        NOT NULL,
    protocolo                  text        NOT NULL CHECK (protocolo ~ '^[0-9]+$'),
    assembleia_data            date,
    assembleia_numero          text,
    modalidade                 text        NOT NULL,
    percentual                 text,
    texto_protocolo            text,
    parcelas_em_atraso         boolean     NOT NULL DEFAULT false,
    lance_existente_autorizado boolean     NOT NULL DEFAULT false,
    pdf_id                     uuid        REFERENCES arquivos (id),
    -- nao_enviar: comprovante reimpresso sem pedido de envio (dá para enviar depois pela tela).
    drive_status               text        NOT NULL DEFAULT 'pendente'
                               CHECK (drive_status IN ('pendente', 'enviando', 'enviado', 'erro', 'sem_pdf', 'nao_enviar')),
    drive_arquivo_id           text,
    drive_link                 text,
    drive_erro                 text,
    drive_tentativas           integer     NOT NULL DEFAULT 0,
    registrado_em              timestamptz,
    criado_em                  timestamptz NOT NULL DEFAULT now(),
    atualizado_em              timestamptz NOT NULL DEFAULT now(),
    UNIQUE (administradora, protocolo)
);
CREATE INDEX lances_cota_idx ON lances (cota_id, registrado_em DESC);
CREATE INDEX lances_drive_fila_idx ON lances (atualizado_em) WHERE drive_status IN ('pendente', 'erro');

-- Credenciais de integrações (ex.: token OAuth do Google Drive), cifradas com AES-256-GCM
-- pela API (CHAVE_CRIPTOGRAFIA). Nunca em arquivo dentro de container.
CREATE TABLE integracoes (
    nome           text        PRIMARY KEY,
    dados_cifrados bytea       NOT NULL,
    atualizado_em  timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE integracoes;
DROP TABLE lances;
ALTER TABLE execucao_cotas
    DROP COLUMN protocolo,
    DROP COLUMN permitir_lance_existente,
    DROP COLUMN assembleia_aprovada,
    DROP CONSTRAINT execucao_cotas_status_check,
    ADD CONSTRAINT execucao_cotas_status_check
        CHECK (status IN ('pendente', 'em_andamento', 'verificada', 'erro_antes_confirmar',
                          'confirmacao_iniciada', 'confirmada', 'erro_apos_confirmar', 'cancelada'));
ALTER TABLE execucoes
    DROP COLUMN dry_run_origem_id,
    DROP CONSTRAINT execucoes_tipo_check,
    ADD CONSTRAINT execucoes_tipo_check CHECK (tipo IN ('dry_run', 'real'));
