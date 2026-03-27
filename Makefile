BINARY := home-automation-schedule-analytics

.PHONY: all build test test-analytics test-analytics-golden test-ui-parity test-analytics-reference run generate clean fmt check-tools

all: build

build: generate
	go build -o $(BINARY) .

test: generate
	go test ./...

test-analytics: generate
	go test ./internal/analytics ./internal/handler

test-analytics-golden: generate
	go test ./internal/handler -run TestAnalyticsGoldenFixtures ./internal/handler

test-ui-parity: generate
	go test ./internal/handler -run 'TestControlPage(RawModeEmbedsSameBucketsAsAPI|CanRenderRawAnalytics|ShowsReportParameterControls)' ./internal/handler

test-analytics-reference:
	python3 scripts/check_analytics.py --self-test

run: generate
	go run .

seed-demo:
	go run ./cmd/seed-demo

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
