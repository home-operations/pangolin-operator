# Changelog

## [0.2.1](https://github.com/home-operations/newt-sidecar/compare/0.2.0...0.2.1) (2026-03-25)


### Features

* Add crd for declaring privateresource ([cad5b4b](https://github.com/home-operations/newt-sidecar/commit/cad5b4b2193e8d90000fa6f1ffa8fedeb850ecc4))


### Bug Fixes

* **deps:** update kubernetes packages (v0.35.2 → v0.35.3) ([#14](https://github.com/home-operations/newt-sidecar/issues/14)) ([3040f0c](https://github.com/home-operations/newt-sidecar/commit/3040f0ce51de054549d5d4fb749748c5493a3316))

## [0.2.0](https://github.com/home-operations/newt-sidecar/compare/0.1.2...0.2.0) (2026-03-19)


### Features

* Allow reading configuration from environment variables ([0f0dbd7](https://github.com/home-operations/newt-sidecar/commit/0f0dbd730a2ad75a13c5507cc3f08820dd9dbc0d))

## [0.1.2](https://github.com/home-operations/newt-sidecar/compare/0.1.1...0.1.2) (2026-03-15)


### Bug Fixes

* replace atomic.Value with RWMutex for context storage in controller ([fba38e8](https://github.com/home-operations/newt-sidecar/commit/fba38e81462b82ff5a78b9f2bed902247a623f73))

## [0.1.1](https://github.com/home-operations/newt-sidecar/compare/0.1.0...0.1.1) (2026-03-15)


### Features

* improve parsing for boolean ([7da1ce0](https://github.com/home-operations/newt-sidecar/commit/7da1ce0137a25a92eb49ff27061afb7aafda4131))
* improve validation and add health ([24b0de0](https://github.com/home-operations/newt-sidecar/commit/24b0de0076edd582dc55bde6203ff7244368e6ab))


### Bug Fixes

* **github-action:** rename workflow to avoid conflict ([6f88d04](https://github.com/home-operations/newt-sidecar/commit/6f88d04a6d6ff5da61c295d5baa5e3bea23cb1d2))

## [0.1.0](https://github.com/home-operations/newt-sidecar/compare/0.0.2...0.1.0) (2026-03-14)


### ⚠ BREAKING CHANGES

* **github-action:** Update action actions/create-github-app-token (v2.2.2 → v3.0.0) ([#7](https://github.com/home-operations/newt-sidecar/issues/7))

### Features

* Add remaining blueprint feature ([6bd1f8c](https://github.com/home-operations/newt-sidecar/commit/6bd1f8c0b92f1e52584134e42ec0455c874f505c))
* add SSO auth support for HTTP resources ([#5](https://github.com/home-operations/newt-sidecar/issues/5)) ([baf1e64](https://github.com/home-operations/newt-sidecar/commit/baf1e6406155efe4e2908b5e1858a2d13b989ff1))


### Bug Fixes

* use dash-separated display name for all single-port Service resources ([#9](https://github.com/home-operations/newt-sidecar/issues/9)) ([036ae80](https://github.com/home-operations/newt-sidecar/commit/036ae80e674a8f05cfc208910710325653e0a670))


### Miscellaneous Chores

* Clean up code and duplicated function ([4adf237](https://github.com/home-operations/newt-sidecar/commit/4adf237f51fe047103f30c1540ee1f7bf137d905))


### Continuous Integration

* **github-action:** Update action actions/create-github-app-token (v2.2.2 → v3.0.0) ([#7](https://github.com/home-operations/newt-sidecar/issues/7)) ([edc864c](https://github.com/home-operations/newt-sidecar/commit/edc864cb8419037052414fea30f5c8a869d85860))

## [0.0.2](https://github.com/home-operations/newt-sidecar/compare/0.0.1...0.0.2) (2026-03-13)


### Features

* add Service discovery (TCP/UDP + HTTP mode, all-ports support) ([#3](https://github.com/home-operations/newt-sidecar/issues/3)) ([dc423e5](https://github.com/home-operations/newt-sidecar/commit/dc423e51b40a198d462ff8afb710827d3bfb27d9))


### Miscellaneous Chores

* cleanup and add more test ([f043936](https://github.com/home-operations/newt-sidecar/commit/f04393636eb46c9590a10b9c359bb4053ece4a61))

## [0.0.1](https://github.com/home-operations/newt-sidecar/compare/0.0.1...0.0.1) (2026-03-10)


### Features

* sidecar service to automatically create newt blueprint ([4e648fc](https://github.com/home-operations/newt-sidecar/commit/4e648fca63b7993b27dfbe870edc62f8913b035d))


### Miscellaneous Chores

* release 0.0.1 ([f92adfb](https://github.com/home-operations/newt-sidecar/commit/f92adfb426663e13027bd8847ad174ae7067315d))
