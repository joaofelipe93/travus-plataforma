-- Serviço de Atendimento (schema atendimento, migração 00020): bots de WhatsApp com fluxo por
-- etapas. O worker não fala com o banco — tudo passa por aqui, pelas rotas da API, como no
-- serviço de boletos. Ver a issue #100.

-- name: CriarBot :one
INSERT INTO atendimento.bots (nome, slug, modelo, instrucoes, saudacao, max_mensagens_hora,
                              max_tokens_conversa, pausa_resposta_segundos, criado_por, atualizado_por)
VALUES (@nome, @slug, @modelo, @instrucoes, sqlc.narg(saudacao), @max_mensagens_hora,
        @max_tokens_conversa, @pausa_resposta_segundos, sqlc.narg(por), sqlc.narg(por))
RETURNING *;

-- Lista para a tela: o bot mais quantas etapas ele tem e quantas conversas estão abertas. As
-- duas contagens evitam a tela pedir bot por bot.
-- name: ListarBots :many
SELECT b.*,
       (SELECT count(*) FROM atendimento.etapas e WHERE e.bot_id = b.id)::int AS etapas,
       (SELECT count(*) FROM atendimento.conversas c
        WHERE c.bot_id = b.id AND c.status = 'aberta')::int AS conversas_abertas
FROM atendimento.bots b
ORDER BY b.nome;

-- name: BuscarBot :one
SELECT * FROM atendimento.bots WHERE id = @id;

-- O worker pede o bot pelo slug: é o nome da pasta da sessão dele no volume.
-- name: BuscarBotPorSlug :one
SELECT * FROM atendimento.bots WHERE slug = @slug;

-- Atualiza o que admin e operador podem mexer. `slug` fica de fora de propósito (trocar
-- perderia o pareamento do WhatsApp) e `envio_habilitado` também, que é só de admin.
-- name: AtualizarBot :one
UPDATE atendimento.bots SET
    nome                    = @nome,
    modelo                  = @modelo,
    instrucoes              = @instrucoes,
    saudacao                = sqlc.narg(saudacao),
    ativo                   = @ativo,
    max_mensagens_hora      = @max_mensagens_hora,
    max_tokens_conversa     = @max_tokens_conversa,
    pausa_resposta_segundos = @pausa_resposta_segundos,
    atualizado_por          = sqlc.narg(por),
    atualizado_em           = now()
WHERE id = @id
RETURNING *;

-- Liga ou desliga o envio de verdade. Só admin chega aqui (a rota barra operador). Quem ligou
-- fica gravado; desligar não apaga o registro de quem havia ligado, que é o histórico útil.
-- name: DefinirEnvioDoBot :one
UPDATE atendimento.bots SET
    envio_habilitado     = @envio_habilitado,
    envio_habilitado_por = CASE WHEN @envio_habilitado::boolean THEN sqlc.narg(por) ELSE envio_habilitado_por END,
    envio_habilitado_em  = CASE WHEN @envio_habilitado::boolean THEN now() ELSE envio_habilitado_em END,
    atualizado_por       = sqlc.narg(por),
    atualizado_em        = now()
WHERE id = @id
RETURNING *;

-- Bot com conversa é histórico e não se exclui (mesma regra de cota com lance): a rota confere
-- antes e responde 409 pedindo para desativar.
-- name: ExcluirBot :execrows
DELETE FROM atendimento.bots WHERE id = @id;

-- name: ContarConversasDoBot :one
SELECT count(*)::int AS total,
       count(*) FILTER (WHERE status = 'aberta')::int AS abertas
FROM atendimento.conversas WHERE bot_id = @bot_id;

-- name: ListarEtapasDoBot :many
SELECT * FROM atendimento.etapas WHERE bot_id = @bot_id ORDER BY ordem;

-- O fluxo é gravado inteiro de uma vez, numa transação: apaga e recria na ordem que a tela
-- mandou. É mais simples e mais previsível que casar etapa por etapa, e a rota só deixa fazer
-- isso quando não há conversa aberta naquele bot — mudar o fluxo debaixo de uma conversa em
-- andamento deixaria a pessoa numa etapa que não existe mais.
-- name: ApagarEtapasDoBot :exec
DELETE FROM atendimento.etapas WHERE bot_id = @bot_id;

-- name: CriarEtapa :one
INSERT INTO atendimento.etapas (bot_id, ordem, nome, objetivo, instrucoes, campos)
VALUES (@bot_id, @ordem, @nome, @objetivo, sqlc.narg(instrucoes), @campos)
RETURNING *;

-- ===== Etapa B: a conversa =====
-- O worker só transporta: quem abre conversa, chama o modelo, grava coleta e enfileira resposta
-- é a API. Nenhuma destas consultas sabe que existe WhatsApp.

-- Abre a conversa ou continua a que está aberta (o índice parcial garante uma por telefone em
-- cada bot). O nome do contato é atualizado quando vem, e não se apaga quando não vem.
-- name: AbrirOuContinuarConversa :one
INSERT INTO atendimento.conversas (bot_id, telefone, contato_nome, etapa_id)
VALUES (@bot_id, @telefone, sqlc.narg(contato_nome), sqlc.narg(etapa_id))
ON CONFLICT (bot_id, telefone) WHERE status = 'aberta' DO UPDATE SET
    contato_nome = coalesce(EXCLUDED.contato_nome, atendimento.conversas.contato_nome),
    ultima_em    = now()
RETURNING *;

-- name: RegistrarMensagemRecebida :one
INSERT INTO atendimento.mensagens (conversa_id, direcao, texto, status)
VALUES (@conversa_id, 'recebida', @texto, 'recebida')
RETURNING *;

-- A resposta entra na fila como 'pendente'; em ensaio (ou barrada por teto) entra 'cancelada'
-- com o motivo, e é assim que a tela mostra o que teria saído.
-- name: EnfileirarResposta :one
INSERT INTO atendimento.mensagens (conversa_id, direcao, texto, status, motivo)
VALUES (@conversa_id, 'enviada', @texto, @status, sqlc.narg(motivo))
RETURNING *;

-- name: HistoricoDaConversa :many
SELECT direcao, texto FROM atendimento.mensagens
WHERE conversa_id = @conversa_id AND status <> 'cancelada'
ORDER BY id DESC
LIMIT @limite;

-- name: SalvarColeta :exec
INSERT INTO atendimento.coletas (conversa_id, etapa_id, campo, valor)
VALUES (@conversa_id, sqlc.narg(etapa_id), @campo, @valor)
ON CONFLICT (conversa_id, campo) DO UPDATE SET
    valor = EXCLUDED.valor, etapa_id = EXCLUDED.etapa_id, atualizado_em = now();

-- name: ColetasDaConversa :many
SELECT campo, valor FROM atendimento.coletas WHERE conversa_id = @conversa_id ORDER BY campo;

-- name: DefinirEtapaDaConversa :one
UPDATE atendimento.conversas SET etapa_id = sqlc.narg(etapa_id), ultima_em = now()
WHERE id = @id RETURNING *;

-- name: EncerrarConversa :one
UPDATE atendimento.conversas SET
    status = @status, motivo = sqlc.narg(motivo), ultima_em = now(), encerrada_em = now()
WHERE id = @id RETURNING *;

-- O teto de tokens é por conversa: somado a cada resposta, é o que a próxima rodada compara.
-- name: SomarTokensDaConversa :one
UPDATE atendimento.conversas SET
    tokens_entrada = tokens_entrada + @tokens_entrada,
    tokens_saida   = tokens_saida + @tokens_saida,
    ultima_em      = now()
WHERE id = @id RETURNING tokens_entrada, tokens_saida;

-- Teto de mensagens por hora, por bot: conta o que saiu e o que está na fila. Protege contra
-- laço de resposta (o outro lado também é automático) e contra bloqueio do número.
-- name: ContarRespostasDaHora :one
SELECT count(*)::int FROM atendimento.mensagens m
JOIN atendimento.conversas c ON c.id = m.conversa_id
WHERE c.bot_id = @bot_id AND m.direcao = 'enviada'
  AND m.status IN ('pendente', 'enviada')
  AND m.criada_em > now() - interval '1 hour';

-- name: PrimeiraEtapaDoBot :one
SELECT * FROM atendimento.etapas WHERE bot_id = @bot_id ORDER BY ordem LIMIT 1;

-- name: EtapaSeguinte :one
SELECT * FROM atendimento.etapas
WHERE bot_id = @bot_id AND ordem > @ordem ORDER BY ordem LIMIT 1;

-- name: BuscarEtapa :one
SELECT * FROM atendimento.etapas WHERE id = @id;
