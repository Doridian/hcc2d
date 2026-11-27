GOROOT := $(shell go env GOROOT)

.PHONY: all build web serve test clean

all: build web

build:
	go build -o colorqr ./cmd/colorqr

# The demo web app: the codec compiled to WebAssembly plus Go's JS glue.
web: web/colorqr.wasm web/wasm_exec.js

web/colorqr.wasm: $(shell find pkg cmd/colorqr-wasm -name '*.go') go.mod
	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o $@ ./cmd/colorqr-wasm

web/wasm_exec.js: $(GOROOT)/lib/wasm/wasm_exec.js
	cp $< $@

serve: web
	go run ./cmd/colorqr-web -dir web

test:
	go test ./...

clean:
	rm -f colorqr web/colorqr.wasm web/wasm_exec.js
