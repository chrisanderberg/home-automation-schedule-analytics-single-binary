BINARY := home-automation-schedule-analytics

.PHONY: build test run generate clean fmt

build: generate
	go build -o $(BINARY) .

test: generate
	go test ./...

run: generate
	go run .

generate:
	templ generate

clean:
	rm -f $(BINARY)
	find . -name '*_templ.go' -delete

fmt:
	gofmt -w .
	templ fmt .
