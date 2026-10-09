.PHONY: lint test test-e2e

lint:
	@golangci-lint run

test:
	@go test -race -count=1 ./...

# Runs a real crawl against a live API. Needs ZENROWS_API_KEY (a key with Crawl access),
# ZENROWS_CRAWL_BASE_URL (e.g. https://api.zenrows.com/v1) and ZENROWS_E2E_CRAWL_URL (the start
# URL); ZENROWS_E2E_CRAWL_INCLUDE is optional. Skips otherwise. See CONTRIBUTING.md.
test-e2e:
	@go test -tags integration -count=1 -run E2E -v -timeout 20m ./...
