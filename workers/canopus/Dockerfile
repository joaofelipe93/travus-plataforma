# A tag da imagem tem que ser a mesma versão do pacote "playwright" do package-lock.json;
# senão os navegadores embutidos na imagem não batem com a biblioteca.
FROM mcr.microsoft.com/playwright:v1.63.0-noble

WORKDIR /app
ENV NODE_ENV=production
# Versão (version.txt) e commit, passados pelo compose; aparecem no log de início.
ARG VERSAO=dev
ARG COMMIT=desconhecido
ENV VERSAO=${VERSAO} COMMIT=${COMMIT}

COPY package.json package-lock.json ./
# Retentativas de rede: o registry do npm derrubou o deploy e a CI em 15/10/2026 com
# ECONNRESET no meio do download (o padrão do npm é 2 tentativas). Não muda o que é
# instalado — o package-lock.json continua mandando.
RUN npm ci --omit=dev --no-audit --no-fund \
    --fetch-retries=5 --fetch-retry-mintimeout=2000 --fetch-retry-maxtimeout=60000 --fetch-timeout=600000

COPY src ./src
COPY docker-entrypoint.sh ./

# Saídas (downloads, screenshots, logs) ficam em /app/data, montado pelo compose.
RUN mkdir -p /app/data/downloads /app/data/screenshots /app/data/logs \
 && chmod +x /app/docker-entrypoint.sh \
 && chown -R pwuser:pwuser /app
USER pwuser

# O Chromium precisa de /dev/shm maior que o padrão do Docker (64 MB):
# o compose usa shm_size; com docker run, passe --shm-size=1g ou --ipc=host.
ENTRYPOINT ["/app/docker-entrypoint.sh"]
# Worker da plataforma (só dry-run nesta etapa). O script legado continua disponível:
# node src/index.js --dry-run --limit 1 (make worker-dry-run).
CMD ["node", "src/worker.js"]
