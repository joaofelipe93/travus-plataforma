# syntax=docker/dockerfile:1
FROM golang:1.27.0-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Versão (version.txt, mantido pelo release-please) e commit, passados pelo compose.
ARG VERSAO=dev
ARG COMMIT=desconhecido
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.versao=${VERSAO} -X main.commit=${COMMIT}" -o /out/api ./cmd/api

# Imagem final sem shell: só o binário (migrações embutidas).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api"]
CMD ["serve"]
