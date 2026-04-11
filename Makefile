check-format:
	@echo "--- Checking format... ---"
	test -z $(shell gofmt -l .) || (echo "ERROR: Go files are not formatted. Please run 'go fmt ./...'"; exit 1)

test: check-format
	@echo "--- Running unit tests... ---"
	go test -v

build: test
	@echo "--- Building executable... ---"
	go build

.PHONY: build test check-format
