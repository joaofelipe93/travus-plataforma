-- +goose Up
-- O texto das mensagens de boleto e de cobrança passa a ser configurável na tela (decisão do
-- usuário em 20/10/2026). Duas coisas que ele pediu e que os padrões abaixo já trazem:
--
--  * é **uma mensagem só para todos**, sem ninguém editar nada cliente a cliente: o que varia
--    são os campos entre chaves, que a plataforma preenche na hora do envio ({grupo}, {cota},
--    {parcela}, {vencimento}, {valor} e, se alguém quiser, {nome});
--  * o número **não tem atendente**, então o texto avisa que a mensagem é automática e que
--    ninguém lê as respostas. O texto anterior terminava com "qualquer dúvida, é só chamar
--    aqui", que convidava o cliente a responder para o vazio.
--
-- Os padrões são os mesmos de api/internal/mensagem (PadraoEnvio e PadraoCobranca), fixados
-- por teste lá: é o texto que chega a um cliente, não se muda sem querer.
ALTER TABLE boletos.configuracao
    ADD COLUMN texto_envio text NOT NULL DEFAULT
        E'Olá! Chegou o boleto do seu investimento para construção do seu patrimônio.\n\n'
        'Grupo {grupo} · Cota {cota} · Parcela {parcela}\n'
        'Vencimento: {vencimento}\n'
        'Valor: {valor}\n\n'
        'O boleto está em anexo.\n\n'
        'Esta é uma mensagem automática: este número não recebe respostas.',
    ADD COLUMN texto_cobranca text NOT NULL DEFAULT
        E'Olá! Vimos que seu investimento para construção do seu patrimônio ainda está disponível para pagamento.\n\n'
        'Ainda dá tempo de participar da assembleia do mês.\n\n'
        'Caso já tenha pago, desconsidere esta mensagem.\n\n'
        'Esta é uma mensagem automática: este número não recebe respostas.';

-- O limite vale para o que for digitado na tela; a API recusa antes, com mensagem melhor.
ALTER TABLE boletos.configuracao
    ADD CONSTRAINT texto_envio_tamanho CHECK (length(texto_envio) BETWEEN 1 AND 1000),
    ADD CONSTRAINT texto_cobranca_tamanho CHECK (length(texto_cobranca) BETWEEN 1 AND 1000);

-- +goose Down
ALTER TABLE boletos.configuracao
    DROP CONSTRAINT texto_envio_tamanho,
    DROP CONSTRAINT texto_cobranca_tamanho,
    DROP COLUMN texto_envio,
    DROP COLUMN texto_cobranca;
