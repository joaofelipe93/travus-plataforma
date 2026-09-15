-- ===== Tela (usuário logado) =====

-- name: CriarExecucao :one
INSERT INTO execucoes (tipo, criada_por)
VALUES (@tipo, @criada_por)
RETURNING id, tipo, status, criada_em;

-- Só cotas ativas entram. Ordem: cliente, grupo, cota, versão.
-- name: InserirCotasNaExecucao :execrows
INSERT INTO execucao_cotas (execucao_id, cota_id, ordem, grupo, cota, versao, cliente_nome, modalidade)
SELECT @execucao_id::bigint, q.id,
       (row_number() OVER (ORDER BY c.nome, q.grupo, q.cota, q.versao))::int,
       q.grupo, q.cota, q.versao, c.nome, q.modalidade_padrao
FROM cotas q
JOIN clientes c ON c.id = q.cliente_id
WHERE q.id = ANY(@cota_ids::bigint[]) AND q.ativa;

-- name: ListarExecucoes :many
SELECT e.id, e.tipo, e.status, e.criada_em, e.iniciada_em, e.finalizada_em, e.cancelamento_solicitado, e.erro,
       u.nome                                                                                    AS criada_por_nome,
       count(ec.id)::int                                                                         AS total,
       (count(ec.id) FILTER (WHERE ec.status IN ('verificada', 'confirmada')))::int              AS sucesso,
       (count(ec.id) FILTER (WHERE ec.status IN ('erro_antes_confirmar', 'erro_apos_confirmar')))::int AS com_erro,
       (count(ec.id) FILTER (WHERE ec.status IN ('pendente', 'em_andamento')))::int              AS restantes
FROM execucoes e
JOIN usuarios u ON u.id = e.criada_por
LEFT JOIN execucao_cotas ec ON ec.execucao_id = e.id
GROUP BY e.id, u.nome
ORDER BY e.criada_em DESC
LIMIT 50;

-- name: BuscarExecucao :one
SELECT e.id, e.tipo, e.status, e.criada_em, e.iniciada_em, e.finalizada_em, e.cancelamento_solicitado, e.erro,
       u.nome AS criada_por_nome, uc.nome AS cancelada_por_nome
FROM execucoes e
JOIN usuarios u ON u.id = e.criada_por
LEFT JOIN usuarios uc ON uc.id = e.cancelada_por
WHERE e.id = @id;

-- name: PosicaoNaFila :one
SELECT count(*)::int
FROM execucoes o, execucoes e
WHERE e.id = @id AND o.status = 'na_fila' AND (o.criada_em, o.id) <= (e.criada_em, e.id);

-- name: ListarCotasDaExecucao :many
SELECT id, cota_id, ordem, grupo, cota, versao, cliente_nome, modalidade, status, erro_tipo, erro,
       detalhes, screenshot_id, tentativas, iniciada_em, finalizada_em
FROM execucao_cotas
WHERE execucao_id = @execucao_id
ORDER BY ordem;

-- name: TravarExecucao :one
SELECT id, tipo, status, worker, cancelamento_solicitado
FROM execucoes
WHERE id = @id
FOR UPDATE;

-- name: CancelarExecucaoNaFila :exec
UPDATE execucoes
SET status = 'cancelada', cancelamento_solicitado = true, cancelada_por = @usuario_id, finalizada_em = now()
WHERE id = @id;

-- O worker vê o pedido ao iniciar a próxima cota ou ao renovar a trava.
-- name: SolicitarCancelamento :exec
UPDATE execucoes
SET cancelamento_solicitado = true, cancelada_por = @usuario_id
WHERE id = @id;

-- name: CancelarCotasPendentes :execrows
UPDATE execucao_cotas
SET status = 'cancelada', finalizada_em = now()
WHERE execucao_id = @execucao_id AND status = 'pendente';

-- name: InserirEvento :one
INSERT INTO execucao_eventos (execucao_id, execucao_cota_id, nivel, mensagem, dados)
VALUES (@execucao_id, sqlc.narg(execucao_cota_id), @nivel, @mensagem, @dados)
RETURNING id;

-- name: ListarEventos :many
SELECT id, execucao_cota_id, nivel, mensagem, dados, criado_em
FROM execucao_eventos
WHERE execucao_id = @execucao_id AND id > @apos_id
ORDER BY id
LIMIT @limite;

-- name: BuscarArquivo :one
SELECT id, tipo, nome, content_type, tamanho, conteudo, criado_em
FROM arquivos
WHERE id = @id;

-- name: ApagarArquivosExpirados :execrows
DELETE FROM arquivos WHERE expira_em < now();

-- ===== Worker (rotas internas) =====

-- Worker caiu (trava vencida): cota em_andamento volta a pendente (a confirmação não
-- tinha começado). Cota em confirmacao_iniciada NUNCA volta: vira erro_apos_confirmar.
-- name: RecuperarCotasDeTravasVencidas :execrows
UPDATE execucao_cotas ec
SET status    = CASE WHEN ec.status = 'em_andamento' THEN 'pendente' ELSE 'erro_apos_confirmar' END,
    erro_tipo = CASE WHEN ec.status = 'em_andamento' THEN NULL ELSE 'inesperado' END,
    erro      = CASE WHEN ec.status = 'em_andamento' THEN NULL
                     ELSE 'o worker parou depois de iniciar a confirmação: o lance pode ter sido registrado, confira no Histórico' END
FROM execucoes e
WHERE e.id = ec.execucao_id
  AND e.status = 'em_andamento' AND e.trava_ate < now()
  AND ec.status IN ('em_andamento', 'confirmacao_iniciada');

-- name: DevolverExecucoesComTravaVencida :many
UPDATE execucoes
SET status        = CASE WHEN cancelamento_solicitado THEN 'cancelada' ELSE 'na_fila' END,
    finalizada_em = CASE WHEN cancelamento_solicitado THEN now() END,
    worker        = NULL,
    trava_ate     = NULL
WHERE status = 'em_andamento' AND trava_ate < now()
RETURNING id, status;

-- name: ReservarProximaExecucao :one
UPDATE execucoes
SET status = 'em_andamento', worker = @worker, trava_ate = now() + interval '2 minutes',
    iniciada_em = COALESCE(iniciada_em, now())
WHERE id = (
    SELECT p.id
    FROM execucoes p
    WHERE p.status = 'na_fila' AND NOT p.cancelamento_solicitado AND p.tipo = ANY(@tipos::text[])
      AND NOT EXISTS (SELECT 1 FROM execucoes o WHERE o.credencial = p.credencial AND o.status = 'em_andamento')
    ORDER BY p.criada_em, p.id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, tipo, credencial;

-- name: RenovarTrava :one
UPDATE execucoes
SET trava_ate = now() + interval '2 minutes'
WHERE id = @id AND worker = @worker AND status = 'em_andamento'
RETURNING cancelamento_solicitado;

-- name: BuscarExecucaoCotaParaWorker :one
SELECT ec.id, ec.execucao_id, ec.status, ec.grupo, ec.cota, ec.versao,
       e.tipo, e.status AS execucao_status, e.worker, e.cancelamento_solicitado
FROM execucao_cotas ec
JOIN execucoes e ON e.id = ec.execucao_id
WHERE ec.id = @id
FOR UPDATE OF e;

-- name: IniciarExecucaoCota :execrows
UPDATE execucao_cotas
SET status = 'em_andamento', tentativas = tentativas + 1, iniciada_em = now(),
    finalizada_em = NULL, erro = NULL, erro_tipo = NULL
WHERE id = @id AND status IN ('pendente', 'erro_antes_confirmar');

-- name: ConcluirExecucaoCota :execrows
UPDATE execucao_cotas
SET status = @status, erro_tipo = sqlc.narg(erro_tipo), erro = sqlc.narg(erro), detalhes = @detalhes, finalizada_em = now()
WHERE id = @id AND status = 'em_andamento';

-- name: InserirArquivo :one
INSERT INTO arquivos (tipo, nome, content_type, tamanho, sha256, conteudo, expira_em)
VALUES (@tipo, @nome, @content_type, @tamanho, @sha256, @conteudo, sqlc.narg(expira_em))
RETURNING id;

-- name: DefinirScreenshotExecucaoCota :exec
UPDATE execucao_cotas SET screenshot_id = @screenshot_id WHERE id = @id;

-- name: ResumoCotasDaExecucao :one
SELECT (count(*) FILTER (WHERE status IN ('erro_antes_confirmar', 'erro_apos_confirmar')))::int AS com_erro,
       (count(*) FILTER (WHERE status IN ('pendente', 'em_andamento')))::int               AS restantes,
       (count(*) FILTER (WHERE status = 'confirmacao_iniciada'))::int                     AS em_confirmacao
FROM execucao_cotas
WHERE execucao_id = @execucao_id;

-- name: FinalizarExecucao :exec
UPDATE execucoes
SET status = @status, erro = sqlc.narg(erro), finalizada_em = now(), trava_ate = NULL
WHERE id = @id;

-- name: DevolverCotasEmAndamento :execrows
UPDATE execucao_cotas
SET status = 'pendente'
WHERE execucao_id = @execucao_id AND status = 'em_andamento';

-- SIGTERM no worker: termina a cota atual e devolve a execução para a fila.
-- name: DevolverExecucaoParaFila :execrows
UPDATE execucoes
SET status = 'na_fila', worker = NULL, trava_ate = NULL
WHERE id = @id AND worker = @worker AND status = 'em_andamento';
