# syntax=docker/dockerfile:1
# A tag da imagem tem que ser a mesma versão do pacote "playwright" do package-lock.json;
# senão os navegadores embutidos na imagem não batem com a biblioteca.
FROM mcr.microsoft.com/playwright:v1.63.0-noble

WORKDIR /app
ENV NODE_ENV=production

COPY package.json package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund

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
