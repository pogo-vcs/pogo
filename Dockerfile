FROM node:22-alpine AS tailwind
ENV PNPM_HOME="/pnpm"
ENV PATH="$PNPM_HOME:$PATH"
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --production
COPY . /app
RUN pnpm run styles

FROM ghcr.io/tsukinoko-kun/go-common:1-alpine AS builder
WORKDIR /app
COPY . .
COPY --from=tailwind /app/server/public/styles.css /app/server/public/styles.css
RUN protoc --go_out=paths=source_relative:. --go-grpc_out=paths=source_relative:. protos/messages.proto && \
    sqlc generate && \
    templ generate && \
    go build -o /bin/pogo .

FROM alpine:latest AS runner
WORKDIR /
COPY --from=builder /bin/pogo /bin/pogo
ENTRYPOINT ["/bin/pogo", "serve"]
