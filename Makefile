.PHONY: build run test clean deps

BINARY_NAME=mcp-server
CMD_DIR=cmd/mcp

build:
	go build -o $(BINARY_NAME) ./$(CMD_DIR)

run:
	go run ./$(CMD_DIR)

deps:
	go mod download
	go mod tidy

test:
	go test -v ./...

clean:
	rm -f $(BINARY_NAME)
	rm -f audit.log

install:
	go install ./$(CMD_DIR)

# Run with stdio transport (default)
run-stdio:
	KRANE_API_URL=http://localhost:8080 \
	KRANE_API_KEY=krane_your_key_here \
	go run ./$(CMD_DIR)

# Run with HTTP transport
run-http:
	KRANE_MCP_TRANSPORT=http \
	KRANE_MCP_PORT=3100 \
	KRANE_API_URL=http://localhost:8080 \
	KRANE_API_KEY=krane_your_key_here \
	go run ./$(CMD_DIR)
