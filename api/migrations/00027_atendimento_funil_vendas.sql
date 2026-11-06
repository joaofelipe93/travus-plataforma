-- Issue #128, item 3: o lead do atendimento sobe para o **Funil de Vendas** (o CRM que a Smart
-- usa, app.funildevendas.com.br).
--
-- Decisões do usuário em 06/11/2026:
--
--   - o envio é **automático quando a conversa encerra** (foi dito a ele que isso tira a triagem
--     da etapa E e que o modelo erra — "sorvete" já virou nome de empresa num teste; ele
--     confirmou). O que limita o estrago: **cada lead sobe uma vez só**, identificado pelo
--     `codigo_interno` da API deles, e há interruptor para desligar;
--   - os códigos da conta (vendedor, funil, etapa, canal, segmento) ficam **na tela**, não no
--     código: trocar um deles não pode exigir deploy, e o repositório é público.
--
-- A chave de integração **não** fica aqui: é segredo, vai no `deploy/.env` (`FUNIL_CHAVE`). A API
-- deles a recebe na query string, então ela não pode aparecer em log nenhum nosso.
--
-- Nada sobe retroativo: o lead é marcado para envio **no momento em que nasce**, com a integração
-- já ligada. Ligar o interruptor não despeja no CRM o que ficou para trás.
--
-- +goose Up
CREATE TABLE atendimento.integracao_funil (
    -- Uma linha só, como a configuração do Canopus.
    id              boolean     PRIMARY KEY DEFAULT true CHECK (id),
    habilitado      boolean     NOT NULL DEFAULT false,
    -- Códigos da conta no Funil de Vendas. Sem todos eles a API recusa a oportunidade, então o
    -- envio só acontece quando estão preenchidos.
    codigo_vendedor integer,
    codigo_funil    integer,
    codigo_etapa    integer,
    codigo_canal    integer,
    codigo_segmento integer,
    atualizado_por  bigint      REFERENCES usuarios (id),
    atualizado_em   timestamptz NOT NULL DEFAULT now()
);

INSERT INTO atendimento.integracao_funil (id) VALUES (true);

ALTER TABLE atendimento.leads
    -- 'nao_enviado' é o lead que nasceu com a integração desligada: ele não entra na fila depois.
    ADD COLUMN funil_status     text NOT NULL DEFAULT 'nao_enviado'
                                CHECK (funil_status IN ('nao_enviado', 'pendente', 'enviando', 'enviado', 'erro')),
    ADD COLUMN funil_tentativas integer NOT NULL DEFAULT 0,
    ADD COLUMN funil_proxima_em timestamptz,
    ADD COLUMN funil_enviado_em timestamptz,
    -- O que o Funil respondeu: a mensagem de sucesso, ou o erro que fez a tentativa falhar.
    ADD COLUMN funil_resposta   text,
    ADD COLUMN funil_erro       text;

CREATE INDEX leads_funil_fila_idx ON atendimento.leads (funil_proxima_em)
    WHERE funil_status IN ('pendente', 'erro');

-- +goose Down
DROP INDEX IF EXISTS atendimento.leads_funil_fila_idx;

ALTER TABLE atendimento.leads
    DROP COLUMN funil_status,
    DROP COLUMN funil_tentativas,
    DROP COLUMN funil_proxima_em,
    DROP COLUMN funil_enviado_em,
    DROP COLUMN funil_resposta,
    DROP COLUMN funil_erro;

DROP TABLE atendimento.integracao_funil;
