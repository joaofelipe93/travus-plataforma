-- +goose Up
-- Lance real automático: depois que o dry-run automático (migração 00013) termina, a API aprova
-- o lance real sozinha, sem ninguém na tela. Decisão do usuário em 15/10/2026 — a rotina do dia
-- 10 vai até o fim (lance registrado e comprovante no Google Drive).
--
-- Nasce DESLIGADO e continua debaixo da chave LANCE_REAL_HABILITADO do servidor: sem ela, nada
-- aqui acontece. As travas são o teto de cotas, a espera com aviso por e-mail e o fato de que
-- só admin liga (httpapi/configuracao.go). Nada disso mexe nas três camadas contra lance
-- duplicado: a trava confirmacao_iniciada, o trigger e o worker continuam como estão.
ALTER TABLE configuracao_canopus
    ADD COLUMN lance_real_automatico     boolean NOT NULL DEFAULT false,
    -- Quem ligou (admin). Nulo com o automático ligado só por escrita direta no banco: o
    -- agendamento não registra lance sem saber quem assina.
    ADD COLUMN lance_real_automatico_por bigint REFERENCES usuarios (id),
    -- Teto de cotas por execução automática: erro de cadastro não vira lance em massa.
    ADD COLUMN lance_real_max_cotas      integer NOT NULL DEFAULT 50
        CHECK (lance_real_max_cotas BETWEEN 1 AND 500),
    -- Espera entre o fim do dry-run e o lance real: janela para ler o aviso e cancelar.
    ADD COLUMN lance_real_espera_minutos integer NOT NULL DEFAULT 30
        CHECK (lance_real_espera_minutos BETWEEN 5 AND 1440);

ALTER TABLE execucoes
    -- Decisão do automático sobre ESTE dry-run, uma vez só (o laço roda a cada minuto).
    ADD COLUMN lance_real_auto_em        timestamptz,
    -- criada: a execução real entrou na fila. chave_desligada: LANCE_REAL_HABILITADO off.
    -- acima_do_limite: mais cotas liberadas que lance_real_max_cotas. sem_cota_liberada:
    -- nenhuma cota passou (todas com erro ou já com lance na assembleia). bloqueado: a revisão
    -- recusou (dry-run expirado, já aprovado, sem responsável). antes_de_ligar: dry-run que já
    -- havia terminado quando o automático foi ligado — não roda retroativo.
    ADD COLUMN lance_real_auto_resultado text
        CHECK (lance_real_auto_resultado IN ('criada', 'chave_desligada', 'acima_do_limite',
                                             'sem_cota_liberada', 'bloqueado', 'antes_de_ligar'));

-- Fila do automático: dry-run agendado que terminou e ainda não foi decidido.
CREATE INDEX execucoes_dry_run_agendado_sem_decisao_idx ON execucoes (finalizada_em)
    WHERE tipo = 'dry_run' AND origem = 'agendada' AND lance_real_auto_em IS NULL;

-- +goose Down
DROP INDEX execucoes_dry_run_agendado_sem_decisao_idx;
ALTER TABLE execucoes
    DROP COLUMN lance_real_auto_resultado,
    DROP COLUMN lance_real_auto_em;
ALTER TABLE configuracao_canopus
    DROP COLUMN lance_real_espera_minutos,
    DROP COLUMN lance_real_max_cotas,
    DROP COLUMN lance_real_automatico_por,
    DROP COLUMN lance_real_automatico;
