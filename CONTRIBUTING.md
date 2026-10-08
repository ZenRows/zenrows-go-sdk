# Contributing to ZenRows Go SDK

First off, thanks for taking the time to contribute!

The following is a set of guidelines for contributing to the ZenRows Go SDK. These are mostly guidelines, not rules.
Use your best judgment, and feel free to propose changes to this document in a pull request.

## How Can I Contribute?

### Reporting Bugs

If you find a bug, please open an issue on GitHub. Provide as much detail as possible:

- A clear and descriptive title.
- A description of the steps to reproduce the issue.
- Any error messages or logs.
- Your environment (Go version, OS, etc.)

### Suggesting Enhancements

Feel free to suggest new features or enhancements. Open an issue with the following details:

- Use a clear and descriptive title.
- Provide a detailed explanation of the feature.
- Explain why this feature would be useful.

### Pull Requests

1. Fork the repository.
2. Clone your fork.
3. Create a new branch for your feature or bug fix:
   ```bash
   git checkout -b feature/your-feature-name
   ```
4. Make your changes and test them.
5. Commit your changes with a meaningful commit message.
6. Push your changes to your fork.
7. Open a pull request on the main repository.

### Code Style

- Follow Go conventions (e.g., `gofmt`).
- Write tests for new functionality.
- Make sure existing tests pass before submitting a PR.

### Running Tests

Run the test suite using:

```bash
go test ./...
```

Each service under `service/` is its own Go module: run the command from that service's directory.

### Running the Crawl end-to-end test

`service/crawl` has an end-to-end test that runs a real crawl (of
`https://www.scrapingcourse.com/ecommerce/`, a few pages) against a live API. It is built only with
the `integration` tag, and skips unless both variables below are set, so `go test ./...` never runs it.

- `ZENROWS_API_KEY`: a key with Crawl access. Each run spends a few requests on its account.
- `ZENROWS_CRAWL_BASE_URL`: the API base, `https://api.zenrows.com/v1` for production. Point it at
  a local or staging deployment to test against that instead.

```bash
cd service/crawl
export ZENROWS_API_KEY=...            # read it from your secret store; keep it out of shell history
export ZENROWS_CRAWL_BASE_URL=https://api.zenrows.com/v1
make test-e2e                         # go test -tags integration -count=1 -run E2E -v -timeout 20m ./...
```

When the account has too many crawls running, the test waits for `Retry-After` and retries the
create for up to 5 minutes before it fails.

### Code of Conduct

This project adheres to the Contributor Covenant [code of conduct](./CODE_OF_CONDUCT.md). By participating, you are 
expected to uphold this code. Please report unacceptable behavior to [us](https://www.zenrows.com/contact).

## Thank You!

Thank you for considering contributing to the ZenRows Go SDK! Feel free to reach out if you have any questions.
