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
       (SELECT count(*) FROM atendimento.perguntas p WHERE p.bot_id = b.id)::int AS perguntas,
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
-- name: ExcluirBot :execrows
DELETE FROM atendimento.bots WHERE id = @id;

-- name: ContarConversasDoBot :one
SELECT count(*)::int AS total,
       count(*) FILTER (WHERE status = 'aberta')::int AS abertas
FROM atendimento.conversas WHERE bot_id = @bot_id;

-- name: ListarPerguntasDoBot :many
SELECT * FROM atendimento.perguntas WHERE bot_id = @bot_id ORDER BY ordem;

-- As perguntas são gravadas inteiras de uma vez, numa transação: apaga e recria na ordem que a
-- tela mandou. É mais simples e mais previsível que casar pergunta por pergunta, e a rota só
-- deixa fazer isso quando não há conversa aberta naquele bot — trocar as perguntas debaixo de
-- uma conversa em andamento mudaria o que já foi perguntado.
-- name: ApagarPerguntasDoBot :exec
DELETE FROM atendimento.perguntas WHERE bot_id = @bot_id;

-- name: CriarPergunta :one
INSERT INTO atendimento.perguntas (bot_id, ordem, texto, campo, orientacao, obrigatoria)
VALUES (@bot_id, @ordem, @texto, @campo, sqlc.narg(orientacao), @obrigatoria)
RETURNING *;

-- ===== Etapa B: a conversa =====
-- O worker só transporta: quem abre conversa, chama o modelo, grava coleta e enfileira resposta
-- é a API. Nenhuma destas consultas sabe que existe WhatsApp.

-- Abre a conversa ou continua a que está aberta (o índice parcial garante uma por telefone em
-- cada bot). O nome do contato é atualizado quando vem, e não se apaga quando não vem.
-- name: AbrirOuContinuarConversa :one
INSERT INTO atendimento.conversas (bot_id, telefone, contato_nome)
VALUES (@bot_id, @telefone, sqlc.narg(contato_nome))
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

-- name: SalvarResposta :exec
INSERT INTO atendimento.coletas (conversa_id, pergunta_id, campo, valor)
VALUES (@conversa_id, sqlc.narg(pergunta_id), @campo, @valor)
ON CONFLICT (conversa_id, campo) DO UPDATE SET
    valor = EXCLUDED.valor, pergunta_id = EXCLUDED.pergunta_id, atualizado_em = now();

-- name: ColetasDaConversa :many
SELECT campo, valor FROM atendimento.coletas WHERE conversa_id = @conversa_id ORDER BY campo;

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

-- Etapa C (as telas). Consultas de LEITURA: o painel, a lista de conversas e a conversa aberta.
-- Os períodos ("hoje", "no mês") vêm calculados da API no fuso de Brasília, como no resto da
-- plataforma — o banco não decide onde o dia começa.

-- name: ResumoAtendimento :one
SELECT
    (SELECT count(*) FROM atendimento.bots b1)                                            AS bots,
    (SELECT count(*) FROM atendimento.bots b2 WHERE b2.ativo)                             AS bots_ativos,
    (SELECT count(*) FROM atendimento.conversas c1 WHERE c1.status = 'aberta')            AS conversas_abertas,
    (SELECT count(*) FROM atendimento.conversas c2 WHERE c2.iniciada_em >= @desde_hoje)    AS conversas_hoje,
    (SELECT count(*) FROM atendimento.conversas c3
       WHERE c3.encerrada_em >= @desde_mes AND c3.motivo = 'pediu_humano')                AS pediram_humano_mes,
    (SELECT count(*) FROM atendimento.conversas c4
       WHERE c4.encerrada_em >= @desde_mes AND c4.motivo = 'concluido')                   AS concluidas_mes,
    (SELECT count(*) FROM atendimento.mensagens m1 WHERE m1.status = 'pendente')          AS fila_pendentes,
    (SELECT count(*) FROM atendimento.mensagens m2 WHERE m2.status = 'falhou')            AS fila_falharam,
    (SELECT count(*) FROM atendimento.mensagens m3
       WHERE m3.status = 'cancelada' AND m3.criada_em >= @desde_mes)                      AS canceladas_mes,
    (SELECT min(m4.proxima_tentativa_em) FROM atendimento.mensagens m4
       WHERE m4.status = 'pendente')                                                      AS proxima_da_fila,
    (SELECT count(*) FROM atendimento.coletas k1 WHERE k1.criado_em >= @desde_mes)        AS dados_mes,
    (SELECT coalesce(sum(c5.tokens_entrada + c5.tokens_saida), 0) FROM atendimento.conversas c5
       WHERE c5.ultima_em >= @desde_mes)::bigint                                          AS tokens_mes;

-- name: ListarConversas :many
SELECT c.id, c.bot_id, b.nome AS bot_nome, b.slug AS bot_slug, c.telefone, c.contato_nome,
       c.status, c.motivo, c.tokens_entrada, c.tokens_saida,
       c.iniciada_em, c.ultima_em, c.encerrada_em,
       (SELECT count(*) FROM atendimento.mensagens m WHERE m.conversa_id = c.id) AS mensagens,
       (SELECT count(*) FROM atendimento.coletas k WHERE k.conversa_id = c.id)   AS dados,
       coalesce((SELECT m.texto FROM atendimento.mensagens m WHERE m.conversa_id = c.id
          ORDER BY m.id DESC LIMIT 1), '')::text                                 AS ultimo_texto,
       coalesce((SELECT m.direcao FROM atendimento.mensagens m WHERE m.conversa_id = c.id
          ORDER BY m.id DESC LIMIT 1), '')::text                                 AS ultima_direcao
FROM atendimento.conversas c
JOIN atendimento.bots b ON b.id = c.bot_id
WHERE (sqlc.narg('bot_id')::bigint IS NULL OR c.bot_id = sqlc.narg('bot_id')::bigint)
  AND (sqlc.narg('status')::text IS NULL OR c.status = sqlc.narg('status')::text)
ORDER BY c.ultima_em DESC
LIMIT @limite;

-- name: BuscarConversa :one
SELECT c.id, c.bot_id, b.nome AS bot_nome, b.slug AS bot_slug, c.telefone, c.contato_nome,
       c.status, c.motivo,
       c.tokens_entrada, c.tokens_saida, c.iniciada_em, c.ultima_em, c.encerrada_em,
       (SELECT count(*) FROM atendimento.perguntas x WHERE x.bot_id = c.bot_id)   AS perguntas_do_bot,
       (SELECT count(*) FROM atendimento.coletas k WHERE k.conversa_id = c.id)    AS respostas
FROM atendimento.conversas c
JOIN atendimento.bots b ON b.id = c.bot_id
WHERE c.id = @id;

-- Tudo o que a conversa teve, na ordem em que aconteceu — inclusive a mensagem `cancelada`, que
-- é o que o bot teria dito em ensaio. (Diferente de HistoricoDaConversa, que monta o que vai ao
-- modelo e por isso omite o que não saiu.)
-- name: MensagensDaConversa :many
SELECT id, direcao, texto, status, motivo, tentativas, ultimo_erro, criada_em, enviada_em
FROM atendimento.mensagens
WHERE conversa_id = @conversa_id
ORDER BY id;

-- Respostas da conversa, na ordem das perguntas do bot (o que a pessoa disse sem ser
-- perguntada vem no fim, porque não tem pergunta).
-- name: DadosDaConversa :many
SELECT k.campo, k.valor, k.criado_em, k.atualizado_em, p.texto AS pergunta_texto, p.ordem AS pergunta_ordem
FROM atendimento.coletas k
LEFT JOIN atendimento.perguntas p ON p.id = k.pergunta_id
WHERE k.conversa_id = @conversa_id
ORDER BY p.ordem NULLS LAST, k.criado_em;

-- Etapa D (o worker de WhatsApp). A fila de envio vive aqui, e não no worker: ele não tem papel
-- no Postgres e não sabe o que é ensaio, teto ou bot desligado — só transporta.

-- Reserva as próximas mensagens de um conjunto de bots. `FOR UPDATE SKIP LOCKED` + trava de 2
-- minutos no `proxima_tentativa_em`: se o worker morrer entre a reserva e o envio, a mensagem
-- volta à fila sozinha, e dois processos nunca pegam a mesma linha.
--
-- Só sai mensagem de bot **ligado e com envio autorizado**: se alguém desligar o bot enquanto a
-- resposta espera, ela simplesmente não é entregue — a decisão mais nova é a que vale.
-- name: ReservarMensagensDeAtendimento :many
WITH alvo AS (
    SELECT m.id
    FROM atendimento.mensagens m
    JOIN atendimento.conversas c ON c.id = m.conversa_id
    JOIN atendimento.bots b ON b.id = c.bot_id
    WHERE m.status = 'pendente'
      AND m.proxima_tentativa_em <= now()
      AND b.ativo
      AND b.slug = ANY(@slugs::text[])
    ORDER BY m.proxima_tentativa_em, m.id
    FOR UPDATE OF m SKIP LOCKED
    LIMIT @limite
)
UPDATE atendimento.mensagens m
SET tentativas           = m.tentativas + 1,
    proxima_tentativa_em = now() + interval '2 minutes'
WHERE m.id IN (SELECT id FROM alvo)
RETURNING m.id, m.texto, m.tentativas,
          (SELECT c.telefone FROM atendimento.conversas c WHERE c.id = m.conversa_id)   AS telefone,
          (SELECT b.slug FROM atendimento.bots b
             JOIN atendimento.conversas c ON c.bot_id = b.id
            WHERE c.id = m.conversa_id)                                                 AS bot_slug,
          (SELECT c.id FROM atendimento.conversas c WHERE c.id = m.conversa_id)          AS conversa_id;

-- name: MarcarMensagemDeAtendimentoEnviada :one
UPDATE atendimento.mensagens
SET status = 'enviada', enviada_em = now(), ultimo_erro = NULL
WHERE id = @id AND status = 'pendente'
RETURNING id, conversa_id;

-- Falha: duas consultas, porque quem decide entre tentar de novo e desistir é a API (o worker
-- diz se a falha é definitiva — número sem WhatsApp não melhora tentando de novo).
-- name: ReenfileirarMensagemDeAtendimento :one
UPDATE atendimento.mensagens
SET ultimo_erro = @erro, proxima_tentativa_em = now() + make_interval(secs => @espera_segundos)
WHERE id = @id AND status = 'pendente'
RETURNING id, tentativas, proxima_tentativa_em;

-- name: DesistirDaMensagemDeAtendimento :one
UPDATE atendimento.mensagens
SET status = 'falhou', motivo = @motivo, ultimo_erro = @erro
WHERE id = @id AND status = 'pendente'
RETURNING id, tentativas;

-- Fila por bot, para a tela de Conexões dizer o que está esperando em cada número.
-- name: FilaDeAtendimentoPorBot :many
SELECT b.id AS bot_id, b.slug, m.status, count(*) AS total
FROM atendimento.mensagens m
JOIN atendimento.conversas c ON c.id = m.conversa_id
JOIN atendimento.bots b ON b.id = c.bot_id
WHERE m.direcao = 'enviada'
GROUP BY b.id, b.slug, m.status;

-- Etapa E: o lead. Nasce quando a conversa encerra tendo colhido algo, é revisado por gente e
-- vira cliente no cadastro (ou é descartado com motivo).

-- Cria o lead da conversa, se ela ainda não tem um. O `ON CONFLICT DO NOTHING` é a garantia de
-- "um lead por conversa" sem precisar conferir antes.
-- name: CriarLeadDaConversa :one
INSERT INTO atendimento.leads (conversa_id, bot_id, nome, telefone, email, observacoes, dados)
VALUES (@conversa_id, @bot_id, sqlc.narg(nome), sqlc.narg(telefone), sqlc.narg(email),
        sqlc.narg(observacoes), @dados)
ON CONFLICT (conversa_id) DO NOTHING
RETURNING *;

-- name: ListarLeads :many
SELECT l.*, b.nome AS bot_nome, b.slug AS bot_slug,
       c.contato_nome, c.telefone AS conversa_telefone, c.status AS conversa_status,
       c.motivo AS conversa_motivo, c.ultima_em,
       cl.nome AS cliente_nome,
       u.nome AS revisado_por_nome,
       (SELECT count(*) FROM atendimento.mensagens m WHERE m.conversa_id = l.conversa_id) AS mensagens
FROM atendimento.leads l
JOIN atendimento.bots b ON b.id = l.bot_id
JOIN atendimento.conversas c ON c.id = l.conversa_id
LEFT JOIN clientes cl ON cl.id = l.cliente_id
LEFT JOIN usuarios u ON u.id = l.revisado_por
WHERE (sqlc.narg('status')::text IS NULL OR l.status = sqlc.narg('status')::text)
  AND (sqlc.narg('bot_id')::bigint IS NULL OR l.bot_id = sqlc.narg('bot_id')::bigint)
ORDER BY (l.status = 'novo') DESC, l.criado_em DESC
LIMIT @limite;

-- name: BuscarLead :one
SELECT l.*, b.nome AS bot_nome, b.slug AS bot_slug,
       c.contato_nome, c.telefone AS conversa_telefone, c.status AS conversa_status,
       c.motivo AS conversa_motivo, c.ultima_em,
       cl.nome AS cliente_nome,
       u.nome AS revisado_por_nome,
       (SELECT count(*) FROM atendimento.mensagens m WHERE m.conversa_id = l.conversa_id) AS mensagens
FROM atendimento.leads l
JOIN atendimento.bots b ON b.id = l.bot_id
JOIN atendimento.conversas c ON c.id = l.conversa_id
LEFT JOIN clientes cl ON cl.id = l.cliente_id
LEFT JOIN usuarios u ON u.id = l.revisado_por
WHERE l.id = @id;

-- name: BuscarLeadDaConversa :one
SELECT id, status, cliente_id FROM atendimento.leads WHERE conversa_id = @conversa_id;

-- Correção da revisão: só os campos que alguém digita. Status e decisão têm rotas próprias.
-- name: AtualizarLead :one
UPDATE atendimento.leads
SET nome = sqlc.narg(nome), telefone = sqlc.narg(telefone), email = sqlc.narg(email),
    observacoes = sqlc.narg(observacoes)
WHERE id = @id AND status = 'novo'
RETURNING *;

-- name: AproveitarLead :one
UPDATE atendimento.leads
SET status = 'aproveitado', cliente_id = @cliente_id, revisado_por = sqlc.narg(revisado_por),
    revisado_em = now(), motivo_descarte = NULL
WHERE id = @id AND status = 'novo'
RETURNING *;

-- name: DescartarLead :one
UPDATE atendimento.leads
SET status = 'descartado', motivo_descarte = @motivo_descarte,
    revisado_por = sqlc.narg(revisado_por), revisado_em = now()
WHERE id = @id AND status = 'novo'
RETURNING *;

-- Contagem para o painel e para a aba.
-- name: ContarLeads :one
SELECT
    (SELECT count(*) FROM atendimento.leads l1 WHERE l1.status = 'novo')          AS novos,
    (SELECT count(*) FROM atendimento.leads l2 WHERE l2.status = 'aproveitado')   AS aproveitados,
    (SELECT count(*) FROM atendimento.leads l3 WHERE l3.status = 'descartado')    AS descartados,
    (SELECT count(*) FROM atendimento.leads l4 WHERE l4.criado_em >= @desde_mes)  AS no_mes;

-- "De onde veio este cliente?": a tela do cliente mostra a conversa que o originou.
-- name: LeadsDoCliente :many
SELECT l.id, l.criado_em, l.conversa_id, b.nome AS bot_nome
FROM atendimento.leads l
JOIN atendimento.bots b ON b.id = l.bot_id
WHERE l.cliente_id = @cliente_id
ORDER BY l.criado_em DESC;

-- Etapa F: o que o vigia olha. Resposta que desistiu e resposta parada na fila — os dois sinais
-- de "o bot respondeu e a pessoa não recebeu", que é o pior jeito de descobrir um problema.
-- name: VigiaAtendimento :one
SELECT
    (SELECT count(*) FROM atendimento.mensagens m1
      WHERE m1.status = 'falhou' AND m1.criada_em > now() - interval '24 hours')     AS falharam,
    (SELECT count(*) FROM atendimento.mensagens m2
      WHERE m2.status = 'pendente' AND m2.criada_em < now() - interval '30 minutes') AS paradas;
