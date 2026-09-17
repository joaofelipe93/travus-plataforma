-- name: ListarCotas :many
SELECT q.id, q.cliente_id, c.nome AS cliente_nome, q.administradora, q.grupo, q.cota, q.versao,
       q.tipo_consorcio, q.modalidade_padrao, q.ativa, q.atualizado_em,
       q.vendedor, q.forma_pagamento, q.vencimento_parcela, q.dia_assembleia, q.contratacao
FROM cotas q
JOIN clientes c ON c.id = q.cliente_id
WHERE (sqlc.narg(cliente_id)::bigint IS NULL OR q.cliente_id = sqlc.narg(cliente_id)::bigint)
  AND (sqlc.narg(grupo)::text IS NULL OR q.grupo = sqlc.narg(grupo)::text)
  AND (sqlc.narg(ativa)::boolean IS NULL OR q.ativa = sqlc.narg(ativa)::boolean)
  AND (sqlc.narg(busca)::text IS NULL
       OR c.nome ILIKE '%' || sqlc.narg(busca)::text || '%'
       OR q.grupo LIKE '%' || sqlc.narg(busca)::text || '%'
       OR q.cota LIKE '%' || sqlc.narg(busca)::text || '%')
ORDER BY c.nome, q.grupo, q.cota, q.versao;

-- name: ListarGrupos :many
SELECT DISTINCT grupo FROM cotas ORDER BY grupo;

-- name: DefinirCotaAtiva :one
UPDATE cotas
SET ativa = @ativa, atualizado_em = now()
WHERE id = @id
RETURNING id, grupo, cota, versao, ativa;

-- name: ListarCotasParaImportacao :many
SELECT q.id, q.cliente_id, c.nome AS cliente_nome, c.nome_normalizado AS cliente_nome_normalizado,
       q.administradora, q.grupo, q.cota, q.versao, q.tipo_consorcio, q.ativa, q.dados_planilha
FROM cotas q
JOIN clientes c ON c.id = q.cliente_id
ORDER BY q.administradora, q.grupo, q.cota, q.versao;

-- name: InserirCota :one
INSERT INTO cotas (cliente_id, administradora, grupo, cota, versao, tipo_consorcio, dados_planilha, importacao_id)
VALUES (@cliente_id, @administradora, @grupo, @cota, @versao, sqlc.narg(tipo_consorcio), @dados_planilha, @importacao_id)
RETURNING id;

-- name: AtualizarCotaImportada :exec
UPDATE cotas
SET cliente_id     = @cliente_id,
    tipo_consorcio = sqlc.narg(tipo_consorcio),
    dados_planilha = @dados_planilha,
    importacao_id  = @importacao_id,
    atualizado_em  = now()
WHERE id = @id;

-- name: BuscarCota :one
SELECT q.id, q.cliente_id, c.nome AS cliente_nome, q.administradora, q.grupo, q.cota, q.versao,
       q.tipo_consorcio, q.modalidade_padrao, q.ativa, q.atualizado_em,
       q.vendedor, q.forma_pagamento, q.vencimento_parcela, q.dia_assembleia, q.contratacao
FROM cotas q
JOIN clientes c ON c.id = q.cliente_id
WHERE q.id = @id;

-- name: CriarCota :one
INSERT INTO cotas (cliente_id, administradora, grupo, cota, versao, tipo_consorcio,
                   modalidade_padrao, ativa, vendedor, forma_pagamento,
                   vencimento_parcela, dia_assembleia, contratacao)
VALUES (@cliente_id, @administradora, @grupo, @cota, @versao, sqlc.narg(tipo_consorcio),
        @modalidade_padrao, @ativa, sqlc.narg(vendedor), sqlc.narg(forma_pagamento),
        sqlc.narg(vencimento_parcela), sqlc.narg(dia_assembleia), sqlc.narg(contratacao))
RETURNING id;

-- name: AtualizarCota :one
UPDATE cotas
SET cliente_id         = @cliente_id,
    administradora     = @administradora,
    grupo              = @grupo,
    cota               = @cota,
    versao             = @versao,
    tipo_consorcio     = sqlc.narg(tipo_consorcio),
    modalidade_padrao  = @modalidade_padrao,
    ativa              = @ativa,
    vendedor           = sqlc.narg(vendedor),
    forma_pagamento    = sqlc.narg(forma_pagamento),
    vencimento_parcela = sqlc.narg(vencimento_parcela),
    dia_assembleia     = sqlc.narg(dia_assembleia),
    contratacao        = sqlc.narg(contratacao),
    atualizado_em      = now()
WHERE id = @id
RETURNING id;

-- name: ExcluirCota :execrows
DELETE FROM cotas WHERE id = @id;

-- Cota com lance registrado ou já usada numa execução não se apaga: é histórico.
-- name: ContarUsoDaCota :one
SELECT (SELECT count(*) FROM lances l WHERE l.cota_id = @cota_id)::int         AS lances,
       (SELECT count(*) FROM execucao_cotas ec WHERE ec.cota_id = @cota_id)::int AS execucoes;
