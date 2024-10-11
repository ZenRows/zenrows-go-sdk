.NOTPARALLEL:

.PHONY: next-patch-version next-minor-version next-major-version tag-next-patch tag-next-minor tag-next-major commit-patch-version commit-minor-version commit-major-version lint test

PACKAGE := "service/api"
NEXT_PATCH_VERSION := $(shell go run cmd/nextversion/main.go -patch)
NEXT_MINOR_VERSION := $(shell go run cmd/nextversion/main.go -minor)
NEXT_MAJOR_VERSION := $(shell go run cmd/nextversion/main.go -major)

.PHONY:
next-patch-version:
	sed -i .bak s/"^const Version.*"/"const Version = \"${NEXT_PATCH_VERSION}\""/g version/version.go

.PHONY:
commit-patch-version:
	git add version/version.go
	git commit -m "chore: set ${PACKAGE} version to ${NEXT_PATCH_VERSION}"

.PHONY:
tag-next-patch: next-patch-version commit-patch-version
	git tag -a ${PACKAGE}/v${NEXT_PATCH_VERSION} -m "New ${PACKAGE} release v${NEXT_PATCH_VERSION}"

.PHONY:
next-minor-version:
	sed -i .bak s/"^const Version.*"/"const Version = \"${NEXT_MINOR_VERSION}\""/g version/version.go

.PHONY:
commit-minor-version:
	git add version/version.go
	git commit -m "chore: set ${PACKAGE} version to ${NEXT_MINOR_VERSION}"

.PHONY:
tag-next-minor: next-minor-version commit-minor-version
	git tag -a ${PACKAGE}/v${NEXT_MINOR_VERSION} -m "New ${PACKAGE} release v${NEXT_MINOR_VERSION}"

.PHONY:
next-major-version:
	sed -i .bak s/"^const Version.*"/"const Version = \"${NEXT_MAJOR_VERSION}\""/g version/version.go

.PHONY:
commit-major-version:
	git add version/version.go
	git commit -m "chore: set ${PACKAGE} version to ${NEXT_MAJOR_VERSION}"

.PHONY:
tag-next-major: next-major-version commit-major-version
	git tag -a ${PACKAGE}/v${NEXT_MAJOR_VERSION} -m "New ${PACKAGE} release v${NEXT_MAJOR_VERSION}"

.PHONY:
lint:
	@golangci-lint run --fast

.PHONY:
test:
	@go test -race -count=1 ./...
