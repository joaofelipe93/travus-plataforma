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
