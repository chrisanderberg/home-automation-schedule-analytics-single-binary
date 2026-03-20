BINARY := home-automation-schedule-analytics

.PHONY: all build test run generate clean fmt check-tools

all: build

build: generate
	go build -o $(BINARY) .

test: generate
	go test ./...

run: generate
	go run .

check-tools:
	@command -v templ >/dev/null 2>&1 || { echo "templ is required but was not found in PATH. Install it before running make generate or make fmt."; exit 1; }

generate: check-tools
	templ generate

clean:
	rm -f $(BINARY)
	find . -name '*_templ.go' -delete

fmt: check-tools
	gofmt -w .
	templ fmt .
