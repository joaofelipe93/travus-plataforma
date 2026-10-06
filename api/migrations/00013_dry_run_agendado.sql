-- +goose Up
-- Dry-run automático: a API cria a execução no dia e na hora da configuração do Canopus
-- (configuracao_canopus, migração 00012). Só dry-run — nada aqui cria execução real.
ALTER TABLE execucoes
    -- Quem pediu: a tela ("manual") ou o agendamento ("agendada"). criada_por continua
    -- apontando para uma pessoa: na agendada, quem ligou o agendamento por último.
    ADD COLUMN origem text NOT NULL DEFAULT 'manual' CHECK (origem IN ('manual', 'agendada')),
    -- Quando o resumo da execução agendada foi avisado por e-mail (uma vez só).
    ADD COLUMN aviso_email_em timestamptz;

-- Fila do aviso: poucas linhas, todas pendentes.
CREATE INDEX execucoes_agendadas_sem_aviso_idx ON execucoes (finalizada_em)
    WHERE origem = 'agendada' AND aviso_email_em IS NULL;

ALTER TABLE configuracao_canopus
    -- Ocorrência já atendida (a do dia X do mês): o laço roda a cada minuto e não repete.
    ADD COLUMN ultimo_disparo_dia       date,
    ADD COLUMN ultimo_disparo_em        timestamptz,
    -- criada: a execução entrou na fila. sem_cotas: nenhuma cota ativa no cadastro.
    -- sem_responsavel: ninguém assinava a configuração (só por escrita direta no banco).
    -- atrasada: a API estava fora do ar na hora e o atraso passou da tolerância.
    -- antes_de_ligar: ocorrência que já havia passado quando o agendamento foi ligado (ou
    -- quando o dia/hora mudou): não roda retroativo e não é alerta.
    ADD COLUMN ultimo_disparo_resultado text
        CHECK (ultimo_disparo_resultado IN ('criada', 'sem_cotas', 'sem_responsavel', 'atrasada', 'antes_de_ligar'));

-- +goose Down
DROP INDEX execucoes_agendadas_sem_aviso_idx;
ALTER TABLE configuracao_canopus
    DROP COLUMN ultimo_disparo_resultado,
    DROP COLUMN ultimo_disparo_em,
    DROP COLUMN ultimo_disparo_dia;
ALTER TABLE execucoes
    DROP COLUMN aviso_email_em,
    DROP COLUMN origem;
