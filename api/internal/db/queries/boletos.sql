-- Serviço de boletos da Canopus (schema boletos, migração 00016). O worker não fala com o
-- banco: tudo passa por aqui, pelas rotas internas da API. Ver docs/canopus-boleto-facil.md.

-- Clientes que podem entrar numa rodada: com documento cadastrado e com pelo menos uma cota.
-- De propósito NÃO filtra cotas.ativa: aquela coluna diz "entra nas execuções de lance", e cota
-- fora do lance continua pagando boleto.
-- name: ClientesParaBoletos :many
SELECT c.id, c.nome, c.cpf_cnpj::text AS cpf_cnpj, count(q.id)::int AS cotas
FROM clientes c
JOIN cotas q ON q.cliente_id = c.id
WHERE c.cpf_cnpj IS NOT NULL
GROUP BY c.id
ORDER BY c.nome;

-- name: CriarRodada :one
INSERT INTO boletos.rodadas (tipo, referencia, origem, criada_por)
VALUES (@tipo, @referencia, @origem, @criada_por)
RETURNING id, tipo, referencia, status, origem, criada_em;

-- name: CriarRodadaCliente :one
INSERT INTO boletos.rodada_clientes (rodada_id, cliente_id, ordem, cliente_nome)
VALUES (@rodada_id, @cliente_id, @ordem, @cliente_nome)
RETURNING id;

-- name: BuscarRodada :one
SELECT r.id, r.tipo, r.referencia, r.status, r.origem, r.cancelamento_solicitado, r.erro,
       r.criada_em, r.iniciada_em, r.finalizada_em,
       u.nome AS criada_por_nome, uc.nome AS cancelada_por_nome
FROM boletos.rodadas r
JOIN usuarios u ON u.id = r.criada_por
LEFT JOIN usuarios uc ON uc.id = r.cancelada_por
WHERE r.id = @id;

-- name: ListarRodadas :many
SELECT r.id, r.tipo, r.referencia, r.status, r.origem, r.criada_em, r.finalizada_em, r.erro,
       u.nome AS criada_por_nome,
       count(rc.id)::int                                            AS total,
       (count(rc.id) FILTER (WHERE rc.status = 'consultado'))::int   AS consultados,
       (count(rc.id) FILTER (WHERE rc.status = 'sem_boleto'))::int   AS sem_boleto,
       (count(rc.id) FILTER (WHERE rc.status = 'erro'))::int         AS com_erro,
       (SELECT count(*) FROM boletos.parcelas p
          JOIN boletos.rodada_clientes rc2 ON rc2.id = p.rodada_cliente_id
         WHERE rc2.rodada_id = r.id)::int                            AS parcelas
FROM boletos.rodadas r
JOIN usuarios u ON u.id = r.criada_por
LEFT JOIN boletos.rodada_clientes rc ON rc.rodada_id = r.id
GROUP BY r.id, u.nome
ORDER BY r.criada_em DESC
LIMIT @limite;

-- name: ListarRodadaClientes :many
SELECT rc.id, rc.cliente_id, rc.ordem, rc.cliente_nome, rc.status, rc.erro_tipo, rc.erro,
       rc.cotas_no_site, rc.cotas_casadas, rc.tentativas, rc.iniciado_em, rc.finalizado_em
FROM boletos.rodada_clientes rc
WHERE rc.rodada_id = @rodada_id
ORDER BY rc.ordem;

-- name: ListarParcelasDaRodada :many
SELECT p.id, p.rodada_cliente_id, p.cota_id, p.grupo, p.cota, p.versao, p.parcela, p.vencimento,
       p.valor_centavos, p.tipo, p.pdf_id, p.criada_em
FROM boletos.parcelas p
JOIN boletos.rodada_clientes rc ON rc.id = p.rodada_cliente_id
WHERE rc.rodada_id = @rodada_id
ORDER BY rc.ordem, p.grupo, p.cota, p.vencimento;

-- Parcelas da última rodada de ENVIO concluída do mês: é com elas que a cobrança compara.
-- A parcela que não voltar mais como aberta no site foi paga (o site não tem campo de "pago").
-- name: ParcelasDoEnvio :many
SELECT p.cota_id, p.parcela, p.vencimento, p.valor_centavos, rc.cliente_id
FROM boletos.parcelas p
JOIN boletos.rodada_clientes rc ON rc.id = p.rodada_cliente_id
JOIN boletos.rodadas r ON r.id = rc.rodada_id
WHERE r.tipo = 'envio' AND r.referencia = @referencia
  AND r.status IN ('concluida', 'concluida_com_erros')
ORDER BY r.criada_em DESC;

-- name: CancelarRodada :execrows
UPDATE boletos.rodadas
SET cancelamento_solicitado = true,
    cancelada_por           = @cancelada_por,
    status                  = CASE WHEN status = 'na_fila' THEN 'cancelada' ELSE status END,
    finalizada_em           = CASE WHEN status = 'na_fila' THEN now() ELSE finalizada_em END
WHERE id = @id AND status IN ('na_fila', 'em_andamento');

-- name: CancelarClientesPendentes :execrows
UPDATE boletos.rodada_clientes
SET status = 'cancelado', finalizado_em = now()
WHERE rodada_id = @rodada_id AND status = 'pendente';

-- Fila do worker: uma rodada em andamento por vez (o site tem limite de taxa).
-- name: ReservarProximaRodada :one
UPDATE boletos.rodadas
SET status = 'em_andamento', worker = @worker, trava_ate = now() + interval '2 minutes',
    iniciada_em = COALESCE(iniciada_em, now())
WHERE id = (
    SELECT p.id
    FROM boletos.rodadas p
    WHERE p.status = 'na_fila' AND NOT p.cancelamento_solicitado
      AND NOT EXISTS (SELECT 1 FROM boletos.rodadas o WHERE o.status = 'em_andamento')
    ORDER BY p.criada_em, p.id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, tipo, referencia;

-- name: RenovarTravaRodada :one
UPDATE boletos.rodadas
SET trava_ate = now() + interval '2 minutes'
WHERE id = @id AND worker = @worker AND status = 'em_andamento'
RETURNING cancelamento_solicitado;

-- name: DevolverRodadasComTravaVencida :many
UPDATE boletos.rodadas
SET status    = CASE WHEN cancelamento_solicitado THEN 'cancelada' ELSE 'na_fila' END,
    finalizada_em = CASE WHEN cancelamento_solicitado THEN now() END,
    worker    = NULL,
    trava_ate = NULL
WHERE status = 'em_andamento' AND trava_ate < now()
RETURNING id, status;

-- Cliente que ficou em andamento com a trava vencida volta para a fila: nada aqui é
-- irreversível (não existe "boleto meio gerado"), então repetir é seguro.
-- name: DevolverClientesComTravaVencida :execrows
UPDATE boletos.rodada_clientes rc
SET status = 'pendente', iniciado_em = NULL
FROM boletos.rodadas r
WHERE r.id = rc.rodada_id AND r.status = 'em_andamento' AND r.trava_ate < now()
  AND rc.status = 'em_andamento';

-- name: LiberarRodada :execrows
UPDATE boletos.rodadas
SET status = CASE WHEN cancelamento_solicitado THEN 'cancelada' ELSE 'na_fila' END,
    finalizada_em = CASE WHEN cancelamento_solicitado THEN now() END,
    worker = NULL, trava_ate = NULL
WHERE id = @id AND worker = @worker AND status = 'em_andamento';

-- Próximo cliente da rodada, com o documento e as cotas cadastradas dele.
-- name: ProximoClienteDaRodada :one
SELECT rc.id, rc.cliente_id, rc.cliente_nome, c.cpf_cnpj::text AS cpf_cnpj
FROM boletos.rodada_clientes rc
JOIN boletos.rodadas r ON r.id = rc.rodada_id
JOIN clientes c ON c.id = rc.cliente_id
WHERE rc.rodada_id = @rodada_id AND rc.status = 'pendente'
  AND r.worker = @worker AND r.status = 'em_andamento' AND NOT r.cancelamento_solicitado
  AND c.cpf_cnpj IS NOT NULL
ORDER BY rc.ordem
LIMIT 1;

-- name: CotasDoCliente :many
SELECT id, grupo, cota, versao
FROM cotas
WHERE cliente_id = @cliente_id
ORDER BY grupo, cota, versao;

-- name: IniciarRodadaCliente :execrows
UPDATE boletos.rodada_clientes
SET status = 'em_andamento', tentativas = tentativas + 1, iniciado_em = now(),
    erro = NULL, erro_tipo = NULL
WHERE id = @id AND status = 'pendente';

-- name: ConcluirRodadaCliente :execrows
UPDATE boletos.rodada_clientes
SET status        = @status,
    erro_tipo     = sqlc.narg(erro_tipo),
    erro          = sqlc.narg(erro),
    cotas_no_site = @cotas_no_site,
    cotas_casadas = @cotas_casadas,
    finalizado_em = now()
WHERE id = @id AND status = 'em_andamento';

-- name: CriarParcela :one
INSERT INTO boletos.parcelas (rodada_cliente_id, cota_id, grupo, cota, versao, parcela,
                              vencimento, valor_centavos, tipo, charge_id, transaction_id)
VALUES (@rodada_cliente_id, @cota_id, @grupo, @cota, @versao, @parcela, @vencimento,
        @valor_centavos, @tipo, sqlc.narg(charge_id), sqlc.narg(transaction_id))
ON CONFLICT (rodada_cliente_id, cota_id, parcela, vencimento) DO UPDATE
SET valor_centavos = excluded.valor_centavos, tipo = excluded.tipo,
    charge_id = excluded.charge_id, transaction_id = excluded.transaction_id
RETURNING id;

-- name: DefinirPdfDaParcela :execrows
UPDATE boletos.parcelas SET pdf_id = @pdf_id WHERE id = @id;

-- name: BuscarParcelaParaWorker :one
SELECT p.id, p.rodada_cliente_id, p.grupo, p.cota, p.versao, p.parcela, p.vencimento, p.pdf_id,
       rc.rodada_id, r.worker, r.status AS rodada_status
FROM boletos.parcelas p
JOIN boletos.rodada_clientes rc ON rc.id = p.rodada_cliente_id
JOIN boletos.rodadas r ON r.id = rc.rodada_id
WHERE p.id = @id;

-- Dono da rodada: toda rota interna confere se a rodada ainda é deste worker antes de gravar.
-- name: RodadaParaWorker :one
SELECT id, tipo, referencia, status, worker, cancelamento_solicitado
FROM boletos.rodadas
WHERE id = @id;

-- name: ClienteDaRodadaParaWorker :one
SELECT rc.id, rc.rodada_id, rc.cliente_id, rc.status, rc.cliente_nome,
       r.tipo, r.status AS rodada_status, r.worker, r.cancelamento_solicitado
FROM boletos.rodada_clientes rc
JOIN boletos.rodadas r ON r.id = rc.rodada_id
WHERE rc.id = @id;

-- name: RegistrarEventoBoleto :exec
INSERT INTO boletos.eventos (rodada_id, rodada_cliente_id, nivel, mensagem, dados)
VALUES (@rodada_id, sqlc.narg(rodada_cliente_id), @nivel, @mensagem, @dados);

-- name: ListarEventosDaRodada :many
SELECT id, rodada_cliente_id, nivel, mensagem, dados, criado_em
FROM boletos.eventos
WHERE rodada_id = @rodada_id
ORDER BY id;

-- name: ContarClientesDaRodada :one
SELECT count(*)::int                                            AS total,
       (count(*) FILTER (WHERE status = 'pendente'))::int        AS pendentes,
       (count(*) FILTER (WHERE status = 'em_andamento'))::int    AS em_andamento,
       (count(*) FILTER (WHERE status = 'erro'))::int            AS com_erro
FROM boletos.rodada_clientes
WHERE rodada_id = @rodada_id;

-- name: FinalizarRodada :execrows
UPDATE boletos.rodadas
SET status        = @status,
    erro          = sqlc.narg(erro),
    finalizada_em = now(),
    worker        = NULL,
    trava_ate     = NULL
WHERE id = @id AND worker = @worker AND status = 'em_andamento';
