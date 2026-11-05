-- ci: destrutiva-aprovada: o interruptor de envio por bot sai. "Ligado" passa a ser o único botão
-- do bot (decisão do usuário em 29/10/2026: "não quero tantas travas, só quero um botão para
-- ligar e desligar"). Nenhum bot existe em produção nem em desenvolvimento, então a coluna sai
-- sem perder dado de ninguém — e deixá-la morta no banco só confundiria quem ler depois.
--
-- Antes disso, um bot só respondia com QUATRO condições: `ativo`, `envio_habilitado` do bot,
-- `ATENDIMENTO_ENVIO_HABILITADO` na API e `ENVIO_HABILITADO` no worker — duas delas exigindo
-- deploy para mudar. Para um serviço que fala pelo número da própria casa, era peso sem retorno:
-- quem liga o bot é quem responde por ele.
--
-- O que sobra para conferir um bot antes de divulgar o número: **mandar mensagem para ele você
-- mesmo**. É o teste que nunca mente, porque é o caminho de verdade.

-- +goose Up
ALTER TABLE atendimento.bots
    DROP COLUMN envio_habilitado,
    DROP COLUMN envio_habilitado_por,
    DROP COLUMN envio_habilitado_em;

-- +goose Down
ALTER TABLE atendimento.bots
    ADD COLUMN envio_habilitado     boolean NOT NULL DEFAULT false,
    ADD COLUMN envio_habilitado_por bigint REFERENCES usuarios (id),
    ADD COLUMN envio_habilitado_em  timestamptz;
