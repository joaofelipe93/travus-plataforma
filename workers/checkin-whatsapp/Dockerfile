# syntax=docker/dockerfile:1
# Notificador de check-in: webhook de reserva → fila (Postgres, schema checkin) → grupo do
# WhatsApp (Baileys).
# Um processo só: não escale (duas conexões na mesma sessão do Baileys derrubam uma à outra e
# cada processo tem o próprio laço da fila, o que duplicaria as mensagens no grupo).

# Dependências (com as de desenvolvimento: o make test roda nesta fase). O libsignal do baileys
# vem do GitHub (git). O git não vai para a imagem final.
FROM node:24.21.0-bookworm-slim AS deps
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

FROM deps AS build
COPY tsconfig.json ./
COPY src ./src
RUN npm run build && npm prune --omit=dev --no-audit --no-fund

FROM node:24.21.0-bookworm-slim
WORKDIR /app
# Versão (version.txt) e commit, passados pelo compose; aparecem no /health.
ARG VERSAO=dev
ARG COMMIT=desconhecido
ENV VERSAO=${VERSAO} \
    COMMIT=${COMMIT} \
    NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=3000 \
    LOG_FORMAT=json \
    AUTH_DIR=/dados/auth_info
COPY --from=build /app/package.json ./
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist

# /dados (volume): sessão do WhatsApp (credenciais do número pareado). Nunca na imagem.
RUN mkdir -p /dados && chown node:node /dados
USER node
VOLUME ["/dados"]
EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:3000/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"]

CMD ["node", "dist/index.js"]
