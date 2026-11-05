-- Issue #128, etapa 1: o bot para de atropelar quem já está sendo atendido.
--
-- Três coisas que a conversa não sabia dizer:
--
--   1. **que uma pessoa assumiu.** Um consultor respondia pelo WhatsApp do bot e o bot continuava
--      falando por cima dele — a plataforma nem via essas mensagens, porque o worker descarta o
--      que sai do próprio número (`fromMe`). Agora ele as entrega, e a conversa fica marcada como
--      assumida: enquanto estiver, o bot não responde nela.
--   2. **que a pessoa voltou.** Conversa encerrada não reabria: quem escrevia de novo começava do
--      zero, sem histórico e sem os dados já colhidos, e o bot reperguntava tudo. Aconteceu em
--      produção em 05/11/2026 com alguém que havia pedido atendimento humano quatro minutos
--      antes. A conversa passa a ser reaberta (decisão do usuário em 05/11/2026).
--   3. **de onde contar o teto de tokens.** Com a conversa reaberta, o teto por conversa viraria
--      teto de vida inteira: `tokens_marco` guarda o gasto no momento da reabertura, e o teto
--      vale para o trecho atual.
--
-- +goose Up
ALTER TABLE atendimento.conversas
    -- Quem assumiu: um usuário (botão na tela) ou NULL quando a plataforma descobriu sozinha, por
    -- ter visto uma mensagem escrita pelo celular do bot.
    ADD COLUMN assumida_em  timestamptz,
    ADD COLUMN assumida_por bigint REFERENCES usuarios (id),
    ADD COLUMN reaberta_em  timestamptz,
    ADD COLUMN tokens_marco integer NOT NULL DEFAULT 0,
    -- O identificador LID desta conversa no WhatsApp, guardado quando a pessoa escreve.
    --
    -- Serve para reconhecer a conversa quando a mensagem SAI pelo número do bot: aí o telefone de
    -- quem está do outro lado não vem no evento (o jid é `<lid>@lid` e o `sender_pn` é o nosso
    -- próprio número). Sem isto, mensagem de consultor em conversa endereçada por LID não teria
    -- como ser atribuída, e a intervenção humana passaria batida justamente nas contas novas.
    ADD COLUMN wa_lid       text;

ALTER TABLE atendimento.mensagens
    -- Mensagem que saiu pelo número do bot mas foi escrita por uma pessoa, não pelo modelo.
    ADD COLUMN de_pessoa boolean NOT NULL DEFAULT false,
    -- O id da mensagem no WhatsApp. É como se distingue o que a plataforma mandou (e volta como
    -- `fromMe` no evento) do que um consultor escreveu pelo celular.
    ADD COLUMN wa_id     text;

CREATE INDEX mensagens_wa_id_idx ON atendimento.mensagens (wa_id) WHERE wa_id IS NOT NULL;

CREATE INDEX conversas_wa_lid_idx ON atendimento.conversas (bot_id, wa_lid)
    WHERE wa_lid IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS atendimento.mensagens_wa_id_idx;
DROP INDEX IF EXISTS atendimento.conversas_wa_lid_idx;

ALTER TABLE atendimento.mensagens
    DROP COLUMN de_pessoa,
    DROP COLUMN wa_id;

ALTER TABLE atendimento.conversas
    DROP COLUMN assumida_em,
    DROP COLUMN assumida_por,
    DROP COLUMN reaberta_em,
    DROP COLUMN tokens_marco,
    DROP COLUMN wa_lid;
