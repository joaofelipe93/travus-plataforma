# Boleto Fácil da Canopus

O que se sabe do **Boleto Fácil**, o portal em que o consorciado da Canopus pega a 2ª via do
boleto: <https://www.consorciocanopus.com.br/boletofacil/>. É daqui que sai o serviço de boletos
(issue #48): todo mês a plataforma busca os boletos em aberto dos clientes, manda por WhatsApp e,
dias depois, cobra quem não pagou.

**Leia este arquivo inteiro antes de mexer no serviço de boletos.** É o equivalente do
[`canopus-newcon.md`](canopus-newcon.md) para este site, e nada aqui foi adivinhado: tudo veio da
leitura do front (`/extensions/boleto-facil-rolledback/public/dist/dist.js` e `templates.js`) e de
chamadas feitas à mão em 16/10/2026: `find-cota` com um CPF fictício (lista vazia) e
**`generate-bill` + impressão num grupo/cota de verdade, até o PDF na mão**. A resposta do
`find-cota` com cotas nunca foi vista: os nomes dos campos vêm do template do próprio site. O que
**não** foi verificado está marcado como tal — não transforme suposição em código silenciosamente.

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
→ 200 {"cotas":[{"CD_Grupo":"006650","CD_Cota":"0924","ID_Cota":…}, …]}
```

Os três nomes de campo saem do template `cotas` do site (`dist/templates.js`), que lê exatamente
`CD_Grupo`, `CD_Cota` e `ID_Cota`; **uma resposta com cotas ainda não foi vista de perto**, então
pode haver mais campos (e o tipo de `ID_Cota`, texto ou número, está em aberto).

- O front exige `^(\d{11}|\d{14})$` antes de chamar — **nada de pontuação**.
- **Um cliente costuma ter vários grupos/cotas**: a lista vem toda de uma vez, e é ela que diz
  quais cotas aquele documento tem na Canopus. O `CD_Grupo` vem com os zeros à esquerda, no mesmo
  formato do cadastro da plataforma (`cotas.grupo`). Já a cota só foi vista com 4 dígitos, e
  *não se sabe* se uma cota de 3 dígitos vem com o zero (`"0924"`) ou sem (`"924"`): **compare os
  dois lados normalizados** (zeros à esquerda até 6/4), nunca por igualdade de texto.
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
→ 200 {"bills":[{"id":<int>, "parcelNumber":"018", "date":"AAAA-MM-DD", "price":<float>,
                 "emitBillet":<bool>, "type":"overdue", "group":"006650", "cota":"0924",
                 "transactionId":<int>}, …]}
```

Resposta conferida numa cota real em 16/10/2026. Cuidados, em ordem de quem já me enganou:

- **`Content-Type` é `text/html`, o corpo é JSON.** Não decida pelo cabeçalho; interprete o corpo
  (o front usa `$.getJSON`, que ignora o tipo declarado).
- **`transactionId` é novo a cada chamada.** Duas chamadas seguidas na mesma cota devolveram
  transações diferentes, e é esse número que entra na URL de impressão. Ou seja: `generate-bill`
  **cria uma transação** no site, e o link do PDF não é estável. Consequência de projeto: busque o
  PDF **logo depois** do `generate-bill`, na mesma sessão (mesmo cookie jar), e **não guarde a URL
  de impressão** como se fosse um link durável — guarde os dados da parcela.
- **`parcelNumber` não é número.** Vem como texto com zeros à esquerda (`"018"`) e existe uma linha
  `"DIF"` (diferença/reajuste). Guarde como texto.
- **O front só mostra parcela com `price > 0`** — a linha `DIF` costuma vir com `price: 0`. Faça o
  mesmo filtro, ou o cliente recebe um boleto de R$ 0,00.
- **`type` foi `overdue`, não `duplicate`**, numa parcela que vence *no futuro*. Então `type` não
  significa "está atrasado": o ramo `duplicate` (→ `print-bill/{id}`) é a exceção, e o caminho
  normal é o de atraso. *Não verificado* quando `duplicate` aparece — trate os dois ramos.
- `emitBillet` não existe no template do site e veio `false` numa cota que **imprimiu PDF**:
  *significado desconhecido*. Não use como condição sem descobrir o que é.
- Em erro, o front diz "Não foram identificados valores válidos para geração de boletos. Entrar em
  contato com a administração do Consórcio Canopus (65) 2123-6000." Isso é o caso normal de cota
  sem nada em aberto — não é falha do serviço.
- O nome "generate" assusta menos do que parecia (nada é cobrado nem baixado), mas ele **registra
  uma transação**: **não chame em teste automático**. Nenhum teste da plataforma fala com o site de
  verdade; os testes usam dublê de HTTP. Exploração à mão, só com pedido do usuário.

### 3. O PDF

```
type == "duplicate" → GET /print-bill/{id}
senão               → GET /print-overdue-bill/{group}/{cota}/{id}/{transactionId}
```

- **Funciona e devolve o PDF**: `200`, `Content-Type: application/pdf`, ~56 KB na cota testada
  (16/10/2026), com os ids saídos do `generate-bill` da mesma sessão.
- **Id velho dá `429 Too Many Requests`.** A mesma rota, chamada antes com o `transactionId` de um
  exemplo antigo, respondeu 429; com os ids recém-gerados, 200. Não está provado se é limite de
  taxa ou recusa de imprimir uma transação que não é a da sessão (o cookie guarda
  `last_charge_id`/`last_transaction_id`) — **as duas leituras levam à mesma regra**: imprima na
  sequência do `generate-bill`, com o mesmo cookie jar, e trate 429 como "recue e tente mais
  tarde", nunca como "tente de novo agora". A VM de produção sai por um IP só, então o serviço
  ainda precisa de espaçamento entre clientes e teto de tentativas por rodada.

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

Por isso a comparação é **por parcela (`parcelNumber` + `date`), não por "a cota tem boleto"**: a
cota testada devolveu a parcela do mês seguinte, ou seja, a lista mostra o que está em aberto
agora, e uma parcela paga some dando lugar à próxima. Quem olhar só "ainda vem boleto?" vai cobrar
cliente em dia.

## De onde a chamada sai (em aberto)

**O usuário relatou que a Canopus não aceita chamada do IP de uma VM** (16/10/2026). Isso decide a
arquitetura do serviço, e é diferente do que parece:

- as três rotas respondem a **`curl` puro**, sem navegador, com User-Agent de `curl` — foi assim
  que elas foram verificadas. Então **não é o navegador que faz o site aceitar**: o Playwright do
  worker do Newcon existe por causa dos postbacks do ASP.NET, não para furar bloqueio;
- o site está atrás de **Cloudflare**, que barra faixas de datacenter antes de a requisição chegar
  na aplicação. Se for esse o caso, **subir um Chromium na VM não resolve**: ele sai pelo mesmo IP.

Para separar as duas hipóteses existe **`tools/teste-ip-vm/teste-boleto.sh`** (só leitura: página
pública, JavaScript e `find-cota` com CPF fictício; **não chama `generate-bill`**). Rode na VM e
compare com a máquina do escritório:

| Resultado na VM | O que é | Caminho |
|---|---|---|
| passa igual | não há bloqueio | o serviço roda na VM, por HTTP, como planejado |
| falha no `curl` e passa com cabeçalho de navegador | detecção de bot por cabeçalho | mandar cabeçalhos de navegador (ou Playwright), **sem** trocar de IP |
| falha nas duas | bloqueio por IP de saída | a requisição precisa sair por outro IP; navegador na VM é inútil |

No terceiro caso as opções reais são: **sair pela rede do escritório** (túnel só para
`*.consorciocanopus.com.br`, serve também para o Newcon e não entrega dado de cliente a terceiro),
**proxy residencial pago** (resolve o IP, mas CPF e boleto passam por um intermediário) ou **rodar
o worker fora da VM**, do jeito que a plataforma já foi desenhada: o worker é cliente da API
interna (puxa tarefa com token), então ele pode rodar na rede do escritório e só conversar com a
plataforma. **Nenhuma dessas foi escolhida** — é decisão do usuário, e vale a mesma resposta para o
worker do Newcon.

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
