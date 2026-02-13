BINARY_NAME=prtea
BUILD_DIR=bin
CMD_DIR=cmd/prtea

.PHONY: build run lint clean

build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)

run:
	go run ./$(CMD_DIR)

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR)
	go clean
