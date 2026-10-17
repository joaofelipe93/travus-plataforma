# A tag da imagem tem que ser a mesma versão do pacote "playwright" do package-lock.json;
# senão os navegadores embutidos na imagem não batem com a biblioteca. É a MESMA tag do worker
# Canopus de propósito: as camadas são compartilhadas e o disco da VM quase não cresce
# (tools/ci/checar-versoes.sh confere os dois).
FROM mcr.microsoft.com/playwright:v1.63.0-noble

WORKDIR /app
ENV NODE_ENV=production
# Versão (version.txt) e commit, passados pelo compose; aparecem no log de início.
ARG VERSAO=dev
ARG COMMIT=desconhecido
ENV VERSAO=${VERSAO} COMMIT=${COMMIT}

COPY package.json package-lock.json ./
# Retentativas de rede: o registry do npm já derrubou o deploy e a CI com ECONNRESET no meio do
# download (o padrão do npm é 2 tentativas). Não muda o que é instalado.
RUN npm ci --omit=dev --no-audit --no-fund \
    --fetch-retries=5 --fetch-retry-mintimeout=2000 --fetch-retry-maxtimeout=60000 --fetch-timeout=600000

COPY src ./src

RUN chown -R pwuser:pwuser /app
USER pwuser

# O Chromium precisa de /dev/shm maior que o padrão do Docker (64 MB): o compose usa shm_size.
# Nada aqui entra no Newcon nem manda mensagem: este worker só LÊ o Boleto Fácil.
CMD ["node", "src/worker.js"]
