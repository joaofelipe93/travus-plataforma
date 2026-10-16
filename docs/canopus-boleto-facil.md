# Boleto Fácil da Canopus

O que se sabe do **Boleto Fácil**, o portal em que o consorciado da Canopus pega a 2ª via do
boleto: <https://www.consorciocanopus.com.br/boletofacil/>. É daqui que sai o serviço de boletos
(issue #48): todo mês a plataforma busca os boletos em aberto dos clientes, manda por WhatsApp e,
dias depois, cobra quem não pagou.

**Leia este arquivo inteiro antes de mexer no serviço de boletos.** É o equivalente do
[`canopus-newcon.md`](canopus-newcon.md) para este site, e nada aqui foi adivinhado: tudo veio da
leitura do front (`/extensions/boleto-facil-rolledback/public/dist/dist.js` e `templates.js`) e de
chamadas de leitura feitas à mão em 16/10/2026. O que **não** foi verificado está marcado como
tal — não transforme suposição em código silenciosamente.

## O site, por baixo

Não é o Newcon: é uma extensão PHP (Slim) do site institucional, atrás do Cloudflare, **sem login
e sem CSRF**, com `Access-Control-Allow-Origin: *`. Ou seja, **não precisa de navegador nem de
Playwright** — o serviço fala HTTP direto, o que é uma diferença grande em relação ao worker do
Newcon.

Base de todas as rotas (o front a lê de um input `data-role="boleto-facil-url"`, então ela pode
mudar sem aviso; é o primeiro lugar a conferir se tudo começar a dar 404):

```
https://www.consorciocanopus.com.br/extensions/boleto-facil-rolledback/public/index.php
```

O front guarda estado num cookie `slim_session` (depois de uma impressão ele carrega
`last_charge_id`, `last_transaction_id`, `last_group_cod`, `last_cota_cod`). **Use um cookie jar
por fluxo e siga a ordem das três chamadas**: achar as cotas → gerar o boleto → imprimir.

## As três chamadas

### 1. Cotas do documento

```
GET /find-cota/{documento}        documento = CPF (11) ou CNPJ (14), só dígitos
→ 200 {"cotas":[{"CD_Grupo":"006650","CD_Cota":"3419","ID_Cota":"468379"}, …]}
```

- O front exige `^(\d{11}|\d{14})$` antes de chamar — **nada de pontuação**.
- **Um cliente costuma ter vários grupos/cotas**: a lista vem toda de uma vez, e é ela que diz
  quais cotas aquele documento tem na Canopus. O `CD_Grupo` vem com os zeros à esquerda, no mesmo
  formato do cadastro da plataforma (`cotas.grupo`), e o `CD_Cota` sem eles (`"3419"`, enquanto a
  plataforma guarda `"3419"` com 4 dígitos — compare normalizando os dois lados).
- **Documento desconhecido não dá erro**: responde `200 {"cotas":[]}` (verificado com um CPF
  fictício válido). Por isso o CPF é validado na plataforma (`internal/cadastro.CpfCnpj`): sem
  isso um dígito errado no cadastro viraria "cliente sem cotas" para sempre, em silêncio.
- **Captcha**: o front conta as tentativas e, a partir da terceira consulta sem resultado, troca o
  formulário por um captcha (`GET /captcha`, `POST /captcha`). A contagem é do JavaScript, mas o
  endpoint existe no servidor — *não verificado* se o servidor também bloqueia por IP. Trate
  vários documentos sem cota como sinal de problema no cadastro, não como caso normal.

### 2. Boletos em aberto da cota

```
GET /generate-bill/{CD_Grupo}/{CD_Cota}/{ID_Cota}
→ 200 {"bills":[{"parcelNumber":…, "date":"AAAA-MM-DD", "price":…,
                 "type":"duplicate"|…, "id":…, "group":…, "cota":…, "transactionId":…}, …]}
```

- O front **só mostra parcela com `price > 0`** e, em erro, diz "Não foram identificados valores
  válidos para geração de boletos. Entrar em contato com a administração do Consórcio Canopus
  (65) 2123-6000." Erro aqui é o caso normal de cota sem nada em aberto — não é falha do serviço.
- `price` é número (reais, com centavos) e `date` é ISO; o front formata (`brMoney`, `brDate`).
- **`type` decide a rota da impressão** (passo 3). O único valor conhecido é `duplicate`; o resto
  cai no ramo de boleto em atraso. *Não verificado* que valores aparecem de fato.
- O nome "generate" assusta, mas é o que o próprio consorciado dispara ao clicar no grupo/cota no
  site. Ainda assim: **não chame isto em teste automático nem em exploração**. Nenhum teste da
  plataforma fala com o site de verdade; os testes usam dublê de HTTP.

### 3. O PDF

```
type == "duplicate" → GET /print-bill/{id}
senão               → GET /print-overdue-bill/{group}/{cota}/{id}/{transactionId}
```

- **Tem limite de taxa.** Uma única chamada à rota de atraso, com ids de um exemplo real,
  respondeu `429 Too Many Requests` (16/10/2026). Não se sabe se o limite é por IP, por sessão ou
  por boleto: o serviço precisa de espaçamento entre clientes, retentativa com espera e um teto de
  tentativas por rodada. A VM de produção sai por um IP só.
- *Não verificado*: o `Content-Type` e o corpo da resposta bem-sucedida (presume-se
  `application/pdf`). Confirme antes de confiar — se vier HTML, o fluxo tem mais um passo.

## Como o serviço usa isso

```
por cliente com cpf_cnpj no cadastro:
  find-cota → casa CD_Grupo/CD_Cota com as cotas cadastradas (só as nossas entram)
  por cota:  generate-bill → parcelas com price > 0 → PDF → mensagem no WhatsApp do cliente
  guarda cada parcela (parcelNumber, date, price, transactionId) para a cobrança seguinte
dias depois:
  generate-bill de novo → parcela que não volta mais como aberta = paga → quem continua aberto
  recebe o follow-up
```

**O site não diz "pago".** A única leitura de pagamento que ele permite é a ausência: a parcela
deixa de aparecer entre as em aberto. Qualquer outra interpretação (status, baixa, data de
pagamento) não existe nesta API — não invente campo.

## Regras

1. **Só leitura, e devagar.** O serviço nunca faz POST neste site. Entre clientes, espere; no
   `429`, recue e tente mais tarde; nunca varra o cadastro inteiro em rajada.
2. **Nenhum teste automático toca o site.** Vale a mesma regra do Newcon: dublê sempre. Exploração
   à mão, só de leitura e com pedido do usuário.
3. **CPF errado é problema de cadastro.** Documento inválido é recusado na API da plataforma e
   documento sem cotas é alerta, não silêncio — três consultas vazias e o site pede captcha.
4. **Dado de cliente não entra no git**: documento, PDF de boleto e payload de resposta ficam
   fora do repositório (o repositório é público). Nos testes, só documentos fictícios com dígito
   verificador certo.
