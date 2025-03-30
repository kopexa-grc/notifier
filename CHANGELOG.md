# Changelog

## [0.3.0](https://github.com/kopexa-grc/notifier/compare/v0.2.0...v0.3.0) (2025-03-30)


### Features

* Add GitHub Actions workflow for dependency vulnerability scanning using Nancy and OSV Scanner ([4c51fd3](https://github.com/kopexa-grc/notifier/commit/4c51fd3ddbdbdb11515baba834e4baa4c831fb91))

## [0.2.0](https://github.com/kopexa-grc/notifier/compare/v0.1.0...v0.2.0) (2025-03-30)


### Features

* Add benchmark tests for notifier performance, including single, batch, concurrent notifications, rate limiting, and resilience mechanisms ([5860acb](https://github.com/kopexa-grc/notifier/commit/5860acba6d068cb0f4b31d8eed6e7a82a3290920))
* Add CLA Assistant GitHub Action workflow for managing Contributor License Agreement signatures ([c8ebd36](https://github.com/kopexa-grc/notifier/commit/c8ebd36a9ee788cd05714760c8bcffa5795d4c66))
* Add configuration files for CI/CD, code quality, and coverage reporting ([b8ec560](https://github.com/kopexa-grc/notifier/commit/b8ec56015b591af6ffb7cc33b037d15bd23c2bdc))
* Add notifier service with options pattern and multiple providers ([b240fbb](https://github.com/kopexa-grc/notifier/commit/b240fbbf93cf9d501b9807f9563b1a207d95eafb))
* Enhance GoBreaker and RetryManager with safety checks and secure random jitter ([4102cec](https://github.com/kopexa-grc/notifier/commit/4102cec5ab44c67fc04006dfd79aeda88ab06147))
* Init repo ([4f3cfff](https://github.com/kopexa-grc/notifier/commit/4f3cfff7f4d6428c423916c518c83248f6b49175))
* Integrate OpenTelemetry for tracing and metrics in ResilientNotifier, enhancing resilience mechanisms and improving logging ([1b6d2a0](https://github.com/kopexa-grc/notifier/commit/1b6d2a06329e37816968888e325de369f6b1eb66))


### Bug Fixes

* Improve StopBatchProcessing and EnableBatching methods with additional safety checks and channel initialization ([618d13b](https://github.com/kopexa-grc/notifier/commit/618d13b87b87cafe24af19add3a733119c7c1588))
