.PHONY: build test test-core test-integrations bench demo
build:
	go build -o bin/sessionflow ./cmd/sessionflow

test-core:
	go test -race ./...
	go vet ./...

test-integrations:
	python3 scripts/test_integrations.py

test: test-core test-integrations
bench:
	go test -run '^$$' -bench BenchmarkMiddleware -benchmem -benchtime=1s
demo: build
	mkdir -p artifacts
	./bin/sessionflow demo -out artifacts/demo.json
	./bin/sessionflow report -input artifacts/demo.json -out artifacts/demo.html
	@echo 'Open artifacts/demo.html, or run: ./bin/sessionflow serve -input artifacts/demo.json'
