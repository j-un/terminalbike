check-format:
	@echo "--- Checking format... ---"
	test -z $(shell gofmt -l .) || (echo "ERROR: Go files are not formatted. Please run 'go fmt ./...'"; exit 1)

tidy:
	@echo "--- Running go mod tidy... ---"
	go mod tidy

tidy-check:
	@echo "--- Checking go.mod / go.sum is tidy... ---"
	go mod tidy -diff

test: check-format
	@echo "--- Running unit tests... ---"
	go test -v

build: test
	@echo "--- Building executable... ---"
	go build -o terminalbike .

.PHONY: build test check-format tidy tidy-check
