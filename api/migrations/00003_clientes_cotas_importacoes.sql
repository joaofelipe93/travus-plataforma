-- +goose Up
-- A planilha não tem CPF: o cliente é identificado pelo nome normalizado
-- (maiúsculas e espaços únicos). Mesmo nome = mesmo cliente.
CREATE TABLE clientes (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nome             text        NOT NULL,
    nome_normalizado text        NOT NULL UNIQUE,
    telefone         text,
    email            text,
    origem           text        NOT NULL,
    criado_em        timestamptz NOT NULL DEFAULT now(),
    atualizado_em    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE importacoes (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    criada_por     bigint      NOT NULL REFERENCES usuarios (id),
    arquivo_nome   text        NOT NULL,
    arquivo_sha256 text        NOT NULL,
    status         text        NOT NULL DEFAULT 'previa'
                   CHECK (status IN ('previa', 'aplicada', 'descartada')),
    -- Linhas já normalizadas (grupo/cota/versão com zeros) e a prévia calculada.
    linhas         jsonb       NOT NULL,
    previa         jsonb       NOT NULL,
    criada_em      timestamptz NOT NULL DEFAULT now(),
    finalizada_em  timestamptz
);

CREATE TABLE cotas (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    cliente_id        bigint      NOT NULL REFERENCES clientes (id),
    administradora    text        NOT NULL,
    grupo             text        NOT NULL CHECK (grupo ~ '^[0-9]{6,}$'),
    cota              text        NOT NULL CHECK (cota ~ '^[0-9]{4,}$'),
    versao            text        NOT NULL DEFAULT '00' CHECK (versao ~ '^[0-9]{2,}$'),
    tipo_consorcio    text,
    -- Modalidades da tela de credenciamento do Newcon. Todas começam em "2º Fixo".
    modalidade_padrao text        NOT NULL DEFAULT 'segundo_fixo'
                      CHECK (modalidade_padrao IN ('livre', 'fixo', 'segundo_fixo', 'limitado', 'fidelidade')),
    ativa             boolean     NOT NULL DEFAULT true,
    -- Colunas da planilha que o sistema ainda não usa (vencimento, vendedor...).
    dados_planilha    jsonb       NOT NULL DEFAULT '{}',
    importacao_id     bigint      REFERENCES importacoes (id),
    criado_em         timestamptz NOT NULL DEFAULT now(),
    atualizado_em     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (administradora, grupo, cota, versao)
);
CREATE INDEX cotas_cliente_id_idx ON cotas (cliente_id);

-- +goose Down
DROP TABLE cotas;
DROP TABLE importacoes;
DROP TABLE clientes;
