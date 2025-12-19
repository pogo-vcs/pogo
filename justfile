fmt:
    go fmt ./...
    templ fmt .

proto:
    protoc --go_out=paths=source_relative:. --go-grpc_out=paths=source_relative:. protos/messages.proto

db:
    sqlc generate

templ:
    templ generate

styles:
    pnpm install
    pnpm run styles

completions:
    mkdir -p docs/completions
    go run -tags="nogui" . completion bash > docs/completions/pogo.bash
    go run -tags="nogui" . completion zsh > docs/completions/pogo.zsh
    go run -tags="nogui" . completion fish > docs/completions/pogo.fish

man:
    go run -tags="nogui" ./scripts man ./docs/man

docs:
    @just completions
    @just man

prebuild:
    @just proto
    @just db
    @just templ
    @just styles

build:
    @just prebuild
    just docs
    go build .

test:
    @just prebuild
    just docs
    go build -tags="fakekeyring,nogui" ./...
    go test -tags="fakekeyring,nogui" ./...

install:
    @just prebuild
    just docs
    go install .

serve:
    @just prebuild
    just docs
    PORT=4321 DATABASE_URL=postgres://pogo:pogo@localhost:5432/pogo ROOT_TOKEN=HP9X+pubni2ufsXTeDreWsxcY+MyxFHBgM+py1hWOks= PUBLIC_ADDRESS=http://localhost:4321 air

deamon:
    @just prebuild
    just docs
    go run ./bin/pogo deamon run
