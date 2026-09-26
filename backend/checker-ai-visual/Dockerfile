FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /checker-ai-visual ./checker-ai-visual

FROM node:22-bookworm-slim
ARG CODEX_CLI_VERSION=latest
ARG CLAUDE_CODE_VERSION=latest
RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && npm install --global "@openai/codex@${CODEX_CLI_VERSION}" "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" \
    && npm cache clean --force
WORKDIR /app
COPY --from=build /checker-ai-visual /app/checker-ai-visual
COPY checker-ai-visual/qc-output-schema.json checker-ai-visual/system-prompt.md /app/
COPY checker-ai-visual/entrypoint.sh /app/entrypoint.sh
RUN chmod 0555 /app/checker-ai-visual /app/entrypoint.sh
ENV HOME=/root \
    CLAUDE_CONFIG_DIR=/root/.claude
ENTRYPOINT ["/app/entrypoint.sh"]
