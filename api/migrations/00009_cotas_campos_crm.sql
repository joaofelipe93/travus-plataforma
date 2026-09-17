-- +goose Up
-- O Canopus virou um CRM: o cadastro é digitado pelo admin/operador, não mais importado de
-- planilha. Os campos que a planilha trazia soltos em cotas.dados_planilha viram colunas de
-- verdade, com validação. dados_planilha continua como está (histórico bruto da importação):
-- apagar as chaves quebraria uma versão anterior do código, que ainda lê de lá.
ALTER TABLE cotas
    ADD COLUMN vendedor           text,
    ADD COLUMN forma_pagamento    text,
    ADD COLUMN vencimento_parcela smallint CHECK (vencimento_parcela BETWEEN 1 AND 31),
    ADD COLUMN dia_assembleia     smallint CHECK (dia_assembleia BETWEEN 1 AND 31),
    ADD COLUMN contratacao        date;

-- Traz o que já foi importado. As chaves são as do cabeçalho normalizado pela importação
-- (minúsculas, sem acento, espaços únicos); a contratação vem no formato da planilha (M/D/AAAA).
UPDATE cotas
SET vendedor = nullif(trim(dados_planilha ->> 'vendedor'), ''),
    forma_pagamento = nullif(trim(dados_planilha ->> 'forma de pagamento'), ''),
    vencimento_parcela = CASE
        WHEN trim(coalesce(dados_planilha ->> 'vencimento da parcela', '')) ~ '^([1-9]|[12][0-9]|3[01])$'
        THEN (trim(dados_planilha ->> 'vencimento da parcela'))::smallint
    END,
    dia_assembleia = CASE
        WHEN trim(coalesce(dados_planilha ->> 'dia da assembleia', '')) ~ '^([1-9]|[12][0-9]|3[01])$'
        THEN (trim(dados_planilha ->> 'dia da assembleia'))::smallint
    END,
    contratacao = CASE
        WHEN trim(coalesce(dados_planilha ->> 'contratacao', '')) ~ '^[0-1]?[0-9]/[0-3]?[0-9]/[0-9]{4}$'
        THEN to_date(trim(dados_planilha ->> 'contratacao'), 'FMMM/FMDD/YYYY')
    END
WHERE dados_planilha <> '{}'::jsonb;

-- +goose Down
ALTER TABLE cotas
    DROP COLUMN contratacao,
    DROP COLUMN dia_assembleia,
    DROP COLUMN vencimento_parcela,
    DROP COLUMN forma_pagamento,
    DROP COLUMN vendedor;
