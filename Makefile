APP_NAME=app
MAIN=./cmd/server/main.go
PROTO_DIR=proto
GEN_DIR=notes

.PHONY: build run clean fmt test proto

# Build Go binary
build:
	go build -o $(APP_NAME).exe $(MAIN)

# Run app
run:
	go run $(MAIN)

# Run tests
test:
	go test ./...

# Format Go code
fmt:
	go fmt ./...

# Clean build artifacts
clean:
	-del $(APP_NAME).exe 2>nul || true
	-if exist $(GEN_DIR) rmdir /S /Q $(GEN_DIR)

# Compile protobuf files
proto:
	if not exist $(GEN_DIR) mkdir $(GEN_DIR)
	protoc --proto_path=$(PROTO_DIR) \
		--go_out=$(GEN_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/notes.proto