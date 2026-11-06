# Changelog

Releases before service/api v1.2.1 are listed on [GitHub Releases](https://github.com/ZenRows/zenrows-go-sdk/releases).

## [1.4.0](https://github.com/ZenRows/zenrows-go-sdk/compare/service/api/v1.3.0...service/api/v1.4.0) (2026-11-06)


### Behaviour Changes

* **api:** with a retry count set, 502/503/504 are retried; POST, PUT, io.Reader bodies, target-site statuses and errors after sending are not; waits use a 1s jittered backoff base, Retry-After up to 30s and a 90s budget; invalid retry options return InvalidOptionError ([1a54b91](https://github.com/ZenRows/zenrows-go-sdk/commit/1a54b9198e27e04c5376ece9a5a859e32c1f4870))


### Features

* **api:** opt-in retries following the shared retry policy; still off by default ([1a54b91](https://github.com/ZenRows/zenrows-go-sdk/commit/1a54b9198e27e04c5376ece9a5a859e32c1f4870))

## [1.3.0](https://github.com/ZenRows/zenrows-go-sdk/compare/service/api/v1.2.1...service/api/v1.3.0) (2026-10-20)


### Features

* **extract:** remove the autoparse fallback and add domain preparation ([#33](https://github.com/ZenRows/zenrows-go-sdk/issues/33)) ([cc93c5a](https://github.com/ZenRows/zenrows-go-sdk/commit/cc93c5ab1b2de8b8cd508c910d601396f1be64f5))

## [1.2.1](https://github.com/ZenRows/zenrows-go-sdk/compare/service/api/v1.2.0...service/api/v1.2.1) (2026-10-14)


### Bug Fixes

* **extract:** return REQS007 as is and drop the "domain-gated" copy ([#27](https://github.com/ZenRows/zenrows-go-sdk/issues/27)) ([66dbd15](https://github.com/ZenRows/zenrows-go-sdk/commit/66dbd153db04fad31131eb0412e7b4c3c10b159c))
