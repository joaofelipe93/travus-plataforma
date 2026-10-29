-- Etapa E da issue #100: o lead. A conversa que colheu algo vira uma linha que alguém revisa e,
-- se fizer sentido, promove a cliente no cadastro da plataforma.
--
-- Por que uma tabela, e não ler `atendimento.coletas` direto na tela: o lead tem vida própria.
-- Ele é **revisado** (alguém corrige o telefone que a pessoa digitou errado), **decidido**
-- (aproveitado ou descartado, com motivo) e precisa dizer **em que cliente ele virou** — nada
-- disso cabe na coleta, que é o registro cru do que o bot entendeu.
--
-- Decisão do usuário em 29/10/2026: os dados vão para **a própria plataforma** (o cadastro de
-- Clientes), não para um CRM de terceiros. E a promoção é de gente: o bot não cria cliente.

-- +goose Up
CREATE TABLE atendimento.leads (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Um lead por conversa: a segunda passada pela mesma conversa não cria outro.
    conversa_id bigint      NOT NULL UNIQUE REFERENCES atendimento.conversas (id) ON DELETE CASCADE,
    bot_id      bigint      NOT NULL REFERENCES atendimento.bots (id),

    status      text        NOT NULL DEFAULT 'novo'
                            CHECK (status IN ('novo', 'aproveitado', 'descartado')),

    -- O que a revisão mostra e deixa corrigir. Nascem do que o bot coletou (e o telefone, do
    -- próprio WhatsApp), mas são editáveis: quem revisa tem a conversa na tela ao lado.
    nome        text,
    telefone    text,
    email       text,
    observacoes text,

    -- Retrato das coletas no momento em que o lead nasceu. Fica para a revisão ver tudo o que a
    -- conversa rendeu, inclusive o que não tem campo próprio aqui (interesse, bairro, horário).
    -- Não substitui `coletas`: aquela é a origem, esta é a foto.
    dados       jsonb       NOT NULL DEFAULT '{}'::jsonb,

    -- Preenchidos na decisão.
    cliente_id      bigint      REFERENCES clientes (id) ON DELETE SET NULL,
    motivo_descarte text,
    revisado_por    bigint      REFERENCES usuarios (id) ON DELETE SET NULL,
    revisado_em     timestamptz,

    criado_em   timestamptz NOT NULL DEFAULT now(),

    -- Lead aproveitado aponta para um cliente; descartado tem motivo. O banco garante que a
    -- decisão não fica pela metade.
    CONSTRAINT lead_aproveitado_tem_cliente
        CHECK (status <> 'aproveitado' OR cliente_id IS NOT NULL),
    CONSTRAINT lead_decidido_tem_revisor
        CHECK (status = 'novo' OR (revisado_por IS NOT NULL AND revisado_em IS NOT NULL))
);

-- A tela abre em "novos primeiro, mais recentes no topo".
CREATE INDEX leads_status_idx ON atendimento.leads (status, criado_em DESC);
CREATE INDEX leads_bot_idx ON atendimento.leads (bot_id, criado_em DESC);
-- "Este cliente veio de qual conversa?"
CREATE INDEX leads_cliente_idx ON atendimento.leads (cliente_id) WHERE cliente_id IS NOT NULL;

-- O assistente da tela inicial (migração 00011) **não** enxerga esta tabela, pela mesma razão de
-- `mensagens` e `coletas`: é o conteúdo da conversa de uma pessoa, não configuração nem contagem.
-- Se um dia ele precisar contar leads, dê o GRANT numa migração nova e descreva em instrucoes.go.

-- +goose Down
DROP TABLE atendimento.leads;
