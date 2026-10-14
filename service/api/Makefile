.PHONY: lint test

lint:
	@golangci-lint run --fast

test:
	@go test -race -count=1 ./...
