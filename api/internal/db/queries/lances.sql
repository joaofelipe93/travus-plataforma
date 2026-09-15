-- name: InserirLancePlataforma :one
INSERT INTO lances (cota_id, execucao_cota_id, origem, administradora, protocolo, assembleia_data, assembleia_numero,
                    modalidade, percentual, texto_protocolo, parcelas_em_atraso, lance_existente_autorizado,
                    pdf_id, drive_status, registrado_em)
VALUES (@cota_id, @execucao_cota_id, 'plataforma', @administradora, @protocolo, sqlc.narg(assembleia_data),
        sqlc.narg(assembleia_numero), @modalidade, sqlc.narg(percentual), sqlc.narg(texto_protocolo),
        @parcelas_em_atraso, @lance_existente_autorizado, sqlc.narg(pdf_id), @drive_status, @registrado_em)
ON CONFLICT (administradora, protocolo) DO NOTHING
RETURNING id;

-- Reimpressão pelo Histórico: cria o lance (origem historico) ou anexa o PDF a um lance
-- que já existe (ex.: registrado pela plataforma com o PDF pendente).
-- name: RegistrarComprovanteDoHistorico :one
INSERT INTO lances (cota_id, execucao_cota_id, origem, administradora, protocolo, assembleia_data, modalidade,
                    percentual, pdf_id, drive_status, registrado_em)
VALUES (@cota_id, @execucao_cota_id, 'historico', @administradora, @protocolo, sqlc.narg(assembleia_data),
        @modalidade, sqlc.narg(percentual), @pdf_id, @drive_status, sqlc.narg(registrado_em))
ON CONFLICT (administradora, protocolo) DO UPDATE
SET pdf_id        = EXCLUDED.pdf_id,
    drive_status  = CASE WHEN lances.drive_status IN ('enviado', 'enviando') THEN lances.drive_status ELSE EXCLUDED.drive_status END,
    atualizado_em = now()
RETURNING id, origem;

-- name: LancesDaCotaNaAssembleia :many
SELECT protocolo, origem FROM lances
WHERE cota_id = @cota_id AND assembleia_data = @assembleia_data
ORDER BY registrado_em;

-- name: ListarLancesDoCliente :many
SELECT l.id, l.cota_id, q.grupo, q.cota, q.versao, l.origem, l.protocolo, l.assembleia_data, l.assembleia_numero,
       l.modalidade, l.percentual, l.parcelas_em_atraso, l.lance_existente_autorizado, l.pdf_id,
       l.drive_status, l.drive_link, l.drive_erro, l.registrado_em, ec.execucao_id
FROM lances l
JOIN cotas q ON q.id = l.cota_id
LEFT JOIN execucao_cotas ec ON ec.id = l.execucao_cota_id
WHERE q.cliente_id = @cliente_id
ORDER BY l.registrado_em DESC NULLS LAST, l.id DESC;

-- Lances de uma execução: os que ela criou e, na reimpressão, o lance do protocolo
-- reimpresso (que pode ter sido criado antes, por outra execução).
-- name: ListarLancesDaExecucao :many
SELECT l.id, ec.id AS execucao_cota_id, l.protocolo, l.pdf_id, l.drive_status, l.drive_link, l.drive_erro, l.parcelas_em_atraso
FROM execucao_cotas ec
JOIN lances l ON l.cota_id = ec.cota_id
             AND (l.execucao_cota_id = ec.id OR (ec.status = 'reimpressa' AND l.protocolo = ec.protocolo))
WHERE ec.execucao_id = @execucao_id;

-- name: BuscarLance :one
SELECT l.id, l.cota_id, l.protocolo, l.pdf_id, l.drive_status, q.cliente_id
FROM lances l JOIN cotas q ON q.id = l.cota_id
WHERE l.id = @id;

-- ===== Fila de envio ao Google Drive =====

-- Envio interrompido (API caiu no meio): volta para a fila.
-- name: RecuperarEnviosTravados :execrows
UPDATE lances SET drive_status = 'pendente', atualizado_em = now()
WHERE drive_status = 'enviando' AND atualizado_em < now() - interval '10 minutes';

-- Pendentes e erros com espera crescente (1, 2, 4, 8 min), até 5 tentativas.
-- name: ReservarLancesParaDrive :many
UPDATE lances
SET drive_status = 'enviando', drive_tentativas = drive_tentativas + 1, atualizado_em = now()
WHERE id IN (
    SELECT l.id FROM lances l
    WHERE l.pdf_id IS NOT NULL
      AND (l.drive_status = 'pendente'
           OR (l.drive_status = 'erro' AND l.drive_tentativas < 5
               AND l.atualizado_em < now() - make_interval(mins => power(2, l.drive_tentativas - 1)::int)))
    ORDER BY l.id
    LIMIT @limite
    FOR UPDATE SKIP LOCKED
)
RETURNING id;

-- name: DadosParaDrive :one
SELECT l.id, l.protocolo, COALESCE(l.registrado_em, l.criado_em)::timestamptz AS data_lance,
       q.grupo, q.cota, q.versao, c.nome AS cliente_nome, a.conteudo AS pdf
FROM lances l
JOIN cotas q ON q.id = l.cota_id
JOIN clientes c ON c.id = q.cliente_id
JOIN arquivos a ON a.id = l.pdf_id
WHERE l.id = @id;

-- name: MarcarDriveEnviado :exec
UPDATE lances
SET drive_status = 'enviado', drive_arquivo_id = @drive_arquivo_id, drive_link = @drive_link, drive_erro = NULL, atualizado_em = now()
WHERE id = @id;

-- name: MarcarDriveErro :exec
UPDATE lances SET drive_status = 'erro', drive_erro = @drive_erro, atualizado_em = now() WHERE id = @id;

-- Pedido na tela: tentar de novo depois de erro, ou enviar um comprovante que ficou sem envio.
-- name: ReenviarAoDrive :execrows
UPDATE lances SET drive_status = 'pendente', drive_tentativas = 0, drive_erro = NULL, atualizado_em = now()
WHERE id = @id AND pdf_id IS NOT NULL AND drive_status IN ('erro', 'nao_enviar');

-- ===== Integrações =====

-- name: SalvarIntegracao :exec
INSERT INTO integracoes (nome, dados_cifrados) VALUES (@nome, @dados_cifrados)
ON CONFLICT (nome) DO UPDATE SET dados_cifrados = EXCLUDED.dados_cifrados, atualizado_em = now();

-- name: BuscarIntegracao :one
SELECT nome, dados_cifrados, atualizado_em FROM integracoes WHERE nome = @nome;
