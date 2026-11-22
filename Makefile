BIN      := ibm-agentid
PKG      := ./cmd/ibm-agentid
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -X main.version=$(VERSION)
PLATFORMS := linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64 windows-arm64

.PHONY: all
all: build

# Build for this machine to bin/ibm-agentid.
.PHONY: build
build:
	go build -mod=readonly -ldflags "$(LDFLAGS)" -o bin/$(BIN) $(PKG)

.PHONY: build-all
build-all: $(addprefix build-,$(PLATFORMS))

# build-<os>-<arch> writes bin/ibm-agentid-<os>-<arch>, with .exe on windows.
.PHONY: build-%
build-%:
	GOOS=$(word 1,$(subst -, ,$*)) GOARCH=$(word 2,$(subst -, ,$*)) \
		go build -mod=readonly -ldflags "$(LDFLAGS)" \
		-o bin/$(BIN)-$*$(if $(findstring windows,$*),.exe) $(PKG)

# Regenerate the OpenAPI clients under internal/resource/ (see api/generate.go).
.PHONY: generate
generate:
	go generate ./api/...

.PHONY: fmt
fmt:
	gofmt -l -w .

.PHONY: vet
vet:
	go vet ./...

.PHONY: test
test:
	go test -race -count=1 ./...

# The checks to pass before calling work done.
.PHONY: check
check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "gofmt: files need formatting"; exit 1; }
	$(MAKE) vet test

.PHONY: clean
clean:
	rm -rf ./bin
