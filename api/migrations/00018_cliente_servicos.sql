-- +goose Up
-- O cadastro de clientes é um só para a plataforma inteira (decisão do usuário em 17/10/2026):
-- o mesmo cliente serve a todos os serviços e o que muda de um para o outro é a ATIVAÇÃO.
--
-- Até aqui isso estava implícito e cada serviço inventava a própria regra: o dry-run pegava
-- toda cota ativa (logo, todo cliente), e a rodada de boletos pegava todo cliente com
-- CPF/CNPJ preenchido — quer dizer, preencher um documento ligava um serviço sem querer.
-- Agora quem decide é esta tabela, e o documento volta a ser só um dado do cliente.
CREATE TABLE cliente_servicos (
    cliente_id    bigint      NOT NULL REFERENCES clientes (id) ON DELETE CASCADE,
    -- Serviços que agem sobre um cliente. 'lances' é o credenciamento de lances no Newcon e
    -- 'boletos' é o envio e a cobrança do Boleto Fácil. Reservas não entra: hóspede não é
    -- cliente do cadastro. Serviço novo = migração nova, junto do código que o use.
    servico       text        NOT NULL CHECK (servico IN ('lances', 'boletos')),
    ativo         boolean     NOT NULL DEFAULT true,
    -- Quem ligou ou desligou por último: a tela mostra de onde veio a decisão.
    alterado_por  bigint      REFERENCES usuarios (id),
    criado_em     timestamptz NOT NULL DEFAULT now(),
    atualizado_em timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (cliente_id, servico)
);

-- Quem está ativo num serviço é a pergunta que as filas fazem (execução e rodada de boletos).
CREATE INDEX cliente_servicos_servico_idx ON cliente_servicos (servico) WHERE ativo;

-- Nada muda de comportamento na virada: todo cliente que já existe entra ativo nos dois
-- serviços, que é exatamente o que a plataforma faz hoje. Desligar é decisão de quem usa.
INSERT INTO cliente_servicos (cliente_id, servico)
SELECT c.id, s.servico
FROM clientes c
CROSS JOIN (VALUES ('lances'), ('boletos')) AS s (servico);

-- O assistente responde "quem está ativo no serviço X": tabela nova precisa do GRANT
-- (migração 00011). Não há coluna sensível aqui.
GRANT SELECT ON cliente_servicos TO assistente;

-- +goose Down
DROP TABLE cliente_servicos;
