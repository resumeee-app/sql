.PHONY: fmt fmt-check test lint verify

fmt:
	gofmt -w .

fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not formatted with gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

test: fmt-check
	go test -race -v ./...

lint: fmt-check
	golangci-lint run ./...

verify: fmt-check test lint
