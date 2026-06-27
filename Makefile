.PHONY: all build run demo serve test bench vet fmt tidy cover clean

BIN := bin/rule-engine

all: vet test build

build:
	@mkdir -p bin
	go build -o $(BIN) .

run:
	go run .

demo:
	go run . -demo

serve:
	go run . -serve :8080

# convert rules to other DSLs: make export DSL=cel
export:
	go run . -export $(or $(DSL),cel) -rules data/rules.json

test:
	go test ./...

bench:
	go test -bench=. -benchmem ./benchmark/

vet:
	go vet ./...

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

cover:
	go test -coverprofile=coverage.txt ./...
	go tool cover -func=coverage.txt | tail -1

clean:
	rm -rf bin coverage.txt
