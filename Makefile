.PHONY: build build-linux docker clean test tidy

BINARY_NAME=wake-me-up
CMD_PATH=./cmd/wake-me-up

build:
	CGO_ENABLED=0 go build -a -ldflags '-extldflags "-s -w -static"' -o bin/$(BINARY_NAME) $(CMD_PATH)

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -ldflags '-extldflags "-s -w -static"' -o bin/$(BINARY_NAME) $(CMD_PATH)

test:
	go test -v ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/
	go clean

run: build
	./bin/$(BINARY_NAME) -config=./config/config.example.yaml
