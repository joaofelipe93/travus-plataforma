-- ci: destrutiva-aprovada: as etapas saem e entram PERGUNTAS NA ORDEM (decisão do usuário em
-- 29/10/2026: ele não gostou de montar o atendimento como etapas com objetivo e campos). Nenhum
-- bot existe, então nada se perde; deixar as duas formas no banco seria o pior dos mundos.
--
-- O que muda, conceitualmente: antes se descrevia o **fluxo** (etapa 1 tem um objetivo e coleta
-- estes campos; com os obrigatórios, avança). Agora se escreve a **lista de perguntas** que o
-- atendimento precisa fazer, na ordem. O bot faz uma por vez, com as palavras dele, e guarda a
-- resposta no campo daquela pergunta.
--
-- Consequência boa: **a conversa não guarda mais em que etapa está**. A próxima pergunta é a
-- primeira sem resposta — calculada do que já foi respondido, não um estado que pode divergir.
-- Por isso `conversas.etapa_id` também sai, e a ferramenta `avancar_etapa` do agente deixa de
-- existir.

-- +goose Up
CREATE TABLE atendimento.perguntas (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    bot_id        bigint      NOT NULL REFERENCES atendimento.bots (id) ON DELETE CASCADE,
    ordem         integer     NOT NULL CHECK (ordem > 0),
    -- A pergunta como quem configurou escreveu. O bot NÃO a repete literalmente: ele reescreve
    -- conforme a conversa (a pessoa pode já ter respondido metade sem ser perguntada).
    texto         text        NOT NULL,
    -- Onde a resposta é guardada (vira chave em `coletas` e campo do lead). Sai do texto da
    -- pergunta e é editável: `nome_da_empresa`, `quantas_vidas`, `melhor_horario`.
    campo         text        NOT NULL CHECK (campo ~ '^[a-z][a-z0-9_]{1,30}$'),
    -- Opcional: o que o bot precisa saber para fazer ESTA pergunta bem — o que fazer com certas
    -- respostas, o que não prometer, quando encerrar.
    orientacao    text,
    -- Pergunta opcional não segura o atendimento: se a pessoa não responder, o bot segue.
    obrigatoria   boolean     NOT NULL DEFAULT true,
    criado_em     timestamptz NOT NULL DEFAULT now(),
    atualizado_em timestamptz NOT NULL DEFAULT now(),
    -- Uma ordem por bot e um campo por bot: duas perguntas gravando no mesmo campo fariam a
    -- segunda apagar a primeira.
    UNIQUE (bot_id, ordem) DEFERRABLE INITIALLY IMMEDIATE,
    UNIQUE (bot_id, campo) DEFERRABLE INITIALLY IMMEDIATE
);
CREATE INDEX perguntas_bot_idx ON atendimento.perguntas (bot_id, ordem);

-- A coleta passa a apontar para a pergunta que a originou (nula quando o bot anotou algo que a
-- pessoa disse sem ter sido perguntada).
ALTER TABLE atendimento.coletas
    DROP COLUMN etapa_id,
    ADD COLUMN pergunta_id bigint REFERENCES atendimento.perguntas (id) ON DELETE SET NULL;

-- A conversa não guarda mais onde está: a próxima pergunta é a primeira sem resposta.
ALTER TABLE atendimento.conversas DROP COLUMN etapa_id;

DROP TABLE atendimento.etapas;

GRANT SELECT ON atendimento.perguntas TO assistente;

-- +goose Down
CREATE TABLE atendimento.etapas (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    bot_id        bigint      NOT NULL REFERENCES atendimento.bots (id) ON DELETE CASCADE,
    ordem         integer     NOT NULL CHECK (ordem > 0),
    nome          text        NOT NULL,
    objetivo      text        NOT NULL,
    instrucoes    text,
    campos        jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(campos) = 'array'),
    criado_em     timestamptz NOT NULL DEFAULT now(),
    atualizado_em timestamptz NOT NULL DEFAULT now(),
    UNIQUE (bot_id, ordem) DEFERRABLE INITIALLY IMMEDIATE
);
CREATE INDEX etapas_bot_idx ON atendimento.etapas (bot_id, ordem);
GRANT SELECT ON atendimento.etapas TO assistente;

ALTER TABLE atendimento.conversas
    ADD COLUMN etapa_id bigint REFERENCES atendimento.etapas (id) ON DELETE SET NULL;
ALTER TABLE atendimento.coletas
    DROP COLUMN pergunta_id,
    ADD COLUMN etapa_id bigint REFERENCES atendimento.etapas (id) ON DELETE SET NULL;

DROP TABLE atendimento.perguntas;
