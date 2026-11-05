-- ci: destrutiva-aprovada: a restrição "um lead por conversa" sai e volta como índice parcial.
-- Nenhum dado se perde (os leads continuam todos lá); o que muda é quantos leads uma conversa
-- pode ter.
--
-- Ela fazia sentido quando conversa encerrada nunca reabria. Da migração 00024 em diante a pessoa
-- que volta a escrever continua na MESMA conversa, e aí a regra antiga criaria um beco: o lead da
-- primeira passagem, se já tivesse sido aproveitado ou descartado, bloquearia para sempre o lead
-- da segunda — ou pior, seria sobrescrito, apagando o trabalho de quem o revisou.
--
-- A regra nova: **no máximo um lead POR DECIDIR por conversa**. Lead já decidido é histórico; a
-- pessoa que volta meses depois é outra oportunidade e rende um lead novo, para alguém revisar.
--
-- +goose Up
ALTER TABLE atendimento.leads DROP CONSTRAINT leads_conversa_id_key;

CREATE UNIQUE INDEX leads_um_novo_por_conversa_idx
    ON atendimento.leads (conversa_id) WHERE status = 'novo';

-- +goose Down
DROP INDEX IF EXISTS atendimento.leads_um_novo_por_conversa_idx;

ALTER TABLE atendimento.leads ADD CONSTRAINT leads_conversa_id_key UNIQUE (conversa_id);
