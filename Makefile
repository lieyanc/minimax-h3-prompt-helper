BINARY  := h3helper
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
RUN_ARGS ?=

.PHONY: all build web deps run dev test fmt clean

all: build

## deps: install frontend dependencies
deps:
	cd web && npm install

## web: build the React frontend into webui/dist for go:embed
web:
	cd web && npm run build
	@touch webui/dist/.gitkeep

## build: build the single binary with the frontend embedded
build: web
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) .
	@echo "built bin/$(BINARY) ($(VERSION))"

## run: rebuild the frontend and backend, then run on the LAN
run: build
	./bin/$(BINARY) $(RUN_ARGS)

## dev: run the Go server and the Vite dev server side by side
##      (the Vite dev server proxies /api to 127.0.0.1:8199)
dev:
	go run . & cd web && npm run dev

## test: vet and test the Go code
test:
	go vet ./...
	go test ./...

fmt:
	go fmt ./...
	cd web && npm run format

clean:
	rm -rf bin webui/dist
	mkdir -p webui/dist
	touch webui/dist/.gitkeep
