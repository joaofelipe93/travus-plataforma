-- Issue #128, item "tokens gastos, otimizar": medir o cache do prompt.
--
-- O agente já sabia quantos tokens vinham do cache (a API informa), mas a conversa só guardava
-- entrada e saída — então não havia como responder "o cache está funcionando?". Sem essa resposta,
-- a otimização seria promessa: o jeito de o cache morrer é silencioso (basta um byte mudar no
-- prefixo), e aparece só como conta maior no fim do mês.
--
-- Leitura de cache custa cerca de um décimo da entrada normal, e a gravação 1,25x — por isso as
-- duas colunas são separadas, e não somadas em `tokens_entrada`.
--
-- +goose Up
ALTER TABLE atendimento.conversas
    ADD COLUMN tokens_cache_lidos    bigint NOT NULL DEFAULT 0,
    ADD COLUMN tokens_cache_gravados bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE atendimento.conversas
    DROP COLUMN tokens_cache_lidos,
    DROP COLUMN tokens_cache_gravados;
