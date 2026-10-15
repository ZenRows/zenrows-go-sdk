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

`service/crawl` has an end-to-end test that runs a small real crawl (depth 1, at most 3 items and
5 pages) against a live API. It is built only with the `integration` tag, and skips unless the
first three variables below are set, so `go test ./...` never runs it.

- `ZENROWS_API_KEY`: a key with Crawl access. Each run spends a few requests on its account.
- `ZENROWS_CRAWL_BASE_URL`: the API base, `https://api.zenrows.com/v1` for production. Point it at
  a local or staging deployment to test against that instead.
- `ZENROWS_E2E_CRAWL_URL`: the start URL to crawl, a page whose links lead to a few others.
- `ZENROWS_E2E_CRAWL_INCLUDE` (optional): an include pattern; the test then checks that every
  result URL contains it.

```bash
cd service/crawl
export ZENROWS_API_KEY=...            # read it from your secret store; keep it out of shell history
export ZENROWS_CRAWL_BASE_URL=https://api.zenrows.com/v1
export ZENROWS_E2E_CRAWL_URL=https://example.com/products/
export ZENROWS_E2E_CRAWL_INCLUDE=/product/
make test-e2e                         # go test -tags integration -count=1 -run E2E -v -timeout 20m ./...
```

When the account has reached its limit of active jobs (3 by default, shared with its Batch jobs),
the test waits for `Retry-After` and retries the create for up to 5 minutes before it fails.

### Code of Conduct

This project adheres to the Contributor Covenant [code of conduct](./CODE_OF_CONDUCT.md). By participating, you are 
expected to uphold this code. Please report unacceptable behavior to [us](https://www.zenrows.com/contact).

## Releases

Each module under `service/` (`api`, `batch`, `crawl`) has its own version and tags, for
example `service/api/v1.2.3`.

1. Merge PRs with Conventional Commit titles, for example `fix(crawl): ...` or
   `feat(batch): ...`. `fix`, `feat` and `perf` need a scope. A `docs`, `chore`, `test`,
   `ci`, `refactor`, `build` or `style` PR releases nothing.
2. After each merge to `main`, release-please opens or updates one PR per changed module,
   `chore: release service/<module> X.Y.Z`. It sets the version in
   `.release-please-manifest.json`, and for `service/api` also in
   `service/api/version/version.go`. Do not change these by hand. To release, merge that
   PR. The workflow runs the module's checks, creates the tag `service/<module>/vX.Y.Z`
   and the GitHub release, and makes the Go proxy fetch the new version. The `release`
   environment has no reviewers.

To verify an existing tag on the Go proxy again, run the Release workflow by hand from
`main` with that tag.

### If the checks fail after the release PR is merged

The release PR runs CI like any other PR. After you merge it, the release
run checks the code again before it tags and publishes. If that check fails, `main`
already has the new version, but there is no tag, and the merged release PR keeps the
`autorelease: pending` label, which blocks the next release PR. To recover:

1. Fix the problem in a normal PR and merge it.
2. Create the release on that fix commit, with the module's version from
   `.release-please-manifest.json`:
   `gh release create service/<module>/vX.Y.Z --target <fix commit> --title "service/<module> vX.Y.Z" --generate-notes --notes-start-tag <previous tag of the module>`
3. On the merged release PR, replace the label `autorelease: pending` with
   `autorelease: tagged`.
4. Run the Release workflow by hand from `main` with the tag `service/<module>/vX.Y.Z`.

## Thank You!

Thank you for considering contributing to the ZenRows Go SDK! Feel free to reach out if you have any questions.
