.PHONY: all test lint wasm cli site fuzz clean

all: wasm site

lint:
	test -z "$$(gofmt -l .)" || (gofmt -l . && false)
	go vet ./engine ./cmd/rainward
	GOOS=js GOARCH=wasm go vet ./cmd/wasm

test:
	go test -count=1 ./...

race:
	go test -race -count=1 -run 'Drop|Flood|Bowl|Bad|Rules' ./engine

fuzz:
	go test -run=NONE -fuzz=FuzzParseLot -fuzztime=30s ./engine

wasm:
	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o site/assets/rainward.wasm ./cmd/wasm

cli:
	go build -trimpath -o build/rainward ./cmd/rainward

site:
	npm run build

clean:
	rm -rf build site/assets/rainward.wasm
