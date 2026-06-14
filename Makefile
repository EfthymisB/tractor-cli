.PHONY: build run test vet fmt lint tidy clean snapshot

build:
	go build -o tractor ./cmd/tractor

run:
	go run ./cmd/tractor

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w $(shell git ls-files '*.go')

# fmt + vet + test
lint: vet
	gofmt -l $(shell git ls-files '*.go')

tidy:
	go mod tidy

# local release snapshot (requires goreleaser)
snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -f tractor tractor.exe
	rm -rf dist
