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
    pnpm run styles

completions:
    mkdir -p docs/completions
    go run . completion bash > docs/completions/pogo.bash
    go run . completion zsh > docs/completions/pogo.zsh
    go run . completion fish > docs/completions/pogo.fish

man:
    go run . gen-man

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
    go build -tags=fakekeyring ./...
    go test -tags=fakekeyring ./... -count=1 -v

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
