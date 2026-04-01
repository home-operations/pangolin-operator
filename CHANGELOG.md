# Changelog

## [0.1.0](https://github.com/home-operations/pangolin-operator/compare/0.0.5...0.1.0) (2026-04-01)


### ⚠ BREAKING CHANGES

* **github-action:** Update action azure/setup-helm (v4.3.1 → v5.0.0) ([#16](https://github.com/home-operations/pangolin-operator/issues/16))

### Features

* **container:** update image golangci/golangci-lint (v2.9.0 → v2.11.4) ([#5](https://github.com/home-operations/pangolin-operator/issues/5)) ([e927311](https://github.com/home-operations/pangolin-operator/commit/e927311d71109c01269d21f42b76c56d09ecaf17))


### Bug Fixes

* don't return early after adding finalizer ([eeb00fb](https://github.com/home-operations/pangolin-operator/commit/eeb00fb70529d94c8eacd20c6f73c4df226c15fa))
* unpin helm version and ping helm unittest ([dd85725](https://github.com/home-operations/pangolin-operator/commit/dd857255f591a98e98605a209b90df0433995210))


### Miscellaneous Chores

* **ci:** tidy up actions ([7bb71e3](https://github.com/home-operations/pangolin-operator/commit/7bb71e3446575ca2d0513bff71bb7084c2bbfe9c))
* cleanup duplicates ([16223fe](https://github.com/home-operations/pangolin-operator/commit/16223fe77c17dd0b936878f8109f871d8cae4f66))
* improve backward compatibility with update site ([1bf3410](https://github.com/home-operations/pangolin-operator/commit/1bf341033064b69d6a69601c8472b6ed559d940b))
* more cleanup ([a844afc](https://github.com/home-operations/pangolin-operator/commit/a844afc4fe07859e99d5ef7b93d7a4acca91529a))
* more cleanup ([4a55e7e](https://github.com/home-operations/pangolin-operator/commit/4a55e7ebdcc4c8da65b8f4ab2b6c8ac36ac90df8))
* share more code between controllers ([0eddd34](https://github.com/home-operations/pangolin-operator/commit/0eddd344a4ebb2a0cff4b1e41511b2bfb6180a0e))
* simplify and unify autodiscover logic ([80e758e](https://github.com/home-operations/pangolin-operator/commit/80e758e0c52fa71696b0bd0f17e835d41120fbfb))


### Continuous Integration

* **github-action:** Update action azure/setup-helm (v4.3.1 → v5.0.0) ([#16](https://github.com/home-operations/pangolin-operator/issues/16)) ([219a539](https://github.com/home-operations/pangolin-operator/commit/219a539504675fd925d7e5a0899f0815333766a3))

## [0.0.5](https://github.com/home-operations/pangolin-operator/compare/0.0.4...0.0.5) (2026-03-31)


### Features

* add generation change predicate ([cfcf55e](https://github.com/home-operations/pangolin-operator/commit/cfcf55e2a1c488a02ae49c4cc7289a3d739756fb))
* **crd:** make all resources cluster-scoped ([9b17c4c](https://github.com/home-operations/pangolin-operator/commit/9b17c4c7ccf332b981fb5aa6369617af63e3f10f))


### Bug Fixes

* **ci:** fix ci failing ([a0f6974](https://github.com/home-operations/pangolin-operator/commit/a0f6974ab7a4d3e1bb80d2b99ad6a56d746a173f))
* **ci:** setup helm ([5bb7b18](https://github.com/home-operations/pangolin-operator/commit/5bb7b182963275c17d0bde9912ebabe5ec641afa))
* ensure enabled to be used ([182f736](https://github.com/home-operations/pangolin-operator/commit/182f7368564f5c7a8776032729387f0fdef35726))
* fix reconciliation logic ([9ffe373](https://github.com/home-operations/pangolin-operator/commit/9ffe373db251d2692464bc8fb73b4ec67c429a86))
* handle better stale resources ([92a6b22](https://github.com/home-operations/pangolin-operator/commit/92a6b2274e98bdd161e60a625c1bd9e3e46dbf2f))
* handle correctly drift ([99f9bef](https://github.com/home-operations/pangolin-operator/commit/99f9befbc7eee21c1d7e8f12b25c562cde9b1c16))
* handle multipages ([fccc0dc](https://github.com/home-operations/pangolin-operator/commit/fccc0dcfa4bc2b82507cb71284eeb260e1be070a))
* keep the resources Namespaced ([448ad15](https://github.com/home-operations/pangolin-operator/commit/448ad15b066ee32d767907c67bb0201ea9b06b70))
* **reconcile:** adopt-or-create pattern to prevent duplicate resources ([c8d9574](https://github.com/home-operations/pangolin-operator/commit/c8d9574dde6dcca6dc28a86e120ecb902a8e1965))


### Miscellaneous Chores

* cleanup code ([b30cdd0](https://github.com/home-operations/pangolin-operator/commit/b30cdd05b11ed796adb8c31b6163e0f85b3173d5))

## [0.0.4](https://github.com/home-operations/pangolin-operator/compare/0.0.3...0.0.4) (2026-03-31)


### Bug Fixes

* **client:** prefer to use orgID scoped resources ([2e9485e](https://github.com/home-operations/pangolin-operator/commit/2e9485e5e2f1cbc86f9e8d83669882d2701d8621))

## [0.0.3](https://github.com/home-operations/pangolin-operator/compare/0.0.2...0.0.3) (2026-03-31)


### ⚠ BREAKING CHANGES

* **container:** Update image alpine/helm (3.17.3 → 4.1.3) ([#11](https://github.com/home-operations/pangolin-operator/issues/11))
* **github-action:** Update action actions/create-github-app-token (v2.2.2 → v3.0.0) ([#6](https://github.com/home-operations/pangolin-operator/issues/6))
* **github-action:** Update action azure/setup-helm (v4.3.1 → v5.0.0) ([#7](https://github.com/home-operations/pangolin-operator/issues/7))

### Features

* add tcproutes discovery ([4ae71ad](https://github.com/home-operations/pangolin-operator/commit/4ae71adf016e5cdcf8a5a3b144d16db8e69563b3))
* **container:** Update image alpine/helm (3.17.3 → 4.1.3) ([#11](https://github.com/home-operations/pangolin-operator/issues/11)) ([ebd5e47](https://github.com/home-operations/pangolin-operator/commit/ebd5e47309f77454ae11fc07c7c8b3ea90cacf19))
* expose metrics ([6c0e170](https://github.com/home-operations/pangolin-operator/commit/6c0e17029a5b055fb1035b48510d05a24bba52aa))
* improve reliability of reconciliation ([5ed43b0](https://github.com/home-operations/pangolin-operator/commit/5ed43b0bd201e9eae9e3f36563b2c4baa381b9d5))


### Bug Fixes

* **container:** update image woodpeckerci/plugin-docker-buildx (6.0.3 → 6.0.4) ([#3](https://github.com/home-operations/pangolin-operator/issues/3)) ([d8f8afa](https://github.com/home-operations/pangolin-operator/commit/d8f8afa27ff62e2d444e6c958884529f9993296b))
* **deps:** update kubernetes monorepo (v0.35.1 → v0.35.3) ([#4](https://github.com/home-operations/pangolin-operator/issues/4)) ([ede3c8c](https://github.com/home-operations/pangolin-operator/commit/ede3c8c50177bf3eb3e1edd010949ea58d681fbf))
* fix linter error ([8133204](https://github.com/home-operations/pangolin-operator/commit/8133204adf8d8218ae24ecfad69e203dda2c32ba))
* only periodic requeue when autodiscover is enabled ([b15c430](https://github.com/home-operations/pangolin-operator/commit/b15c430d4f71f370cb58de255928a757f646c4b2))
* pod annotations incorrectly defaulting to labels map ([9ab9add](https://github.com/home-operations/pangolin-operator/commit/9ab9add4b760639e1be918abdbbbac60e360de5f))
* reconcile labels on existing autodiscovered PublicResources ([45ca2c3](https://github.com/home-operations/pangolin-operator/commit/45ca2c3a9113e635af499b6f95c670c425024424))
* replace fragile string replacement in NOTES.txt with explicit labels ([f90f6bc](https://github.com/home-operations/pangolin-operator/commit/f90f6bc4230f63ae10f31b71ac6fdeb103bb2896))
* skip creating credentials secret when values are empty ([47283bb](https://github.com/home-operations/pangolin-operator/commit/47283bb8e51144c2a56883391d783ae72f372ba2))


### Documentation

* fix incorrect helm values, mtu default, and private resource mode in readme ([5476d0d](https://github.com/home-operations/pangolin-operator/commit/5476d0d37c0af27de6eb7c5f0faa0f89beeecb0c))


### Miscellaneous Chores

* add .helmignore ([9694990](https://github.com/home-operations/pangolin-operator/commit/9694990974644b82e9af0a25cb2dba8a2da7827b))
* add consistent yaml document separator to servicemonitor template ([4cdf08f](https://github.com/home-operations/pangolin-operator/commit/4cdf08faf190fe917a079d2bb68a4b3e1fa76655))
* add default resource requests and limits ([94ee495](https://github.com/home-operations/pangolin-operator/commit/94ee49518c8e98f11091ab9ca7833bb71831ccd3))
* add helm chart NOTES.txt with post-install guidance ([270a9da](https://github.com/home-operations/pangolin-operator/commit/270a9dae25847fed392ec2837f8101113913b5a7))
* add kubeVersion constraint to chart ([4f12aaa](https://github.com/home-operations/pangolin-operator/commit/4f12aaa880b97346f4e87bd204fff6c65a04e7bd))
* adjust default resource requests and limits ([77c2763](https://github.com/home-operations/pangolin-operator/commit/77c276305f9656af8e6a6b321b89140f34956a4a))
* cleanup duplicate ([69ae200](https://github.com/home-operations/pangolin-operator/commit/69ae200bfbb0152c6b1b1605becd8e098ff26ee9))
* enable gosec linter and fix findings ([4c13a7a](https://github.com/home-operations/pangolin-operator/commit/4c13a7aaefde4259beac9e12def318f06101327f))
* make priorityClassName configurable in values ([83779c5](https://github.com/home-operations/pangolin-operator/commit/83779c592eb09a542fe24777bd7a711bc4faa5c7))
* pin distroless base image by digest ([0cd693f](https://github.com/home-operations/pangolin-operator/commit/0cd693f8c6f088e7ed4d59911e9543c8ec631731))
* release 0.0.3 ([95ff137](https://github.com/home-operations/pangolin-operator/commit/95ff137a61513aaff44d02bc4acb75e0fa920970))
* remove unused DeleteOwnedPublicResources ([3c3f400](https://github.com/home-operations/pangolin-operator/commit/3c3f4001dbaf39e428538089dff2617afeb9c10e))


### Code Refactoring

* align privateresource setCondition signature with other controllers ([d744348](https://github.com/home-operations/pangolin-operator/commit/d744348fb66dbaef2a1889c156b4864239ee8533))
* consolidate test helpers into internal/testutil ([43ef320](https://github.com/home-operations/pangolin-operator/commit/43ef3206804b765efe25e1fcbe8374ca23dd1b7c))
* extract MTU default into constant ([b2f9c8a](https://github.com/home-operations/pangolin-operator/commit/b2f9c8a0e18b92bb0ea4f206cd88dd32518a179e))


### Continuous Integration

* **github-action:** Update action actions/create-github-app-token (v2.2.2 → v3.0.0) ([#6](https://github.com/home-operations/pangolin-operator/issues/6)) ([530d30e](https://github.com/home-operations/pangolin-operator/commit/530d30e78bb917e901a7d8bb7e81a97fa9ec9e4b))
* **github-action:** Update action azure/setup-helm (v4.3.1 → v5.0.0) ([#7](https://github.com/home-operations/pangolin-operator/issues/7)) ([5648ac3](https://github.com/home-operations/pangolin-operator/commit/5648ac3501f7c6db8fc9ae79e04bea03d5b23a25))

## [0.0.2](https://github.com/home-operations/pangolin-operator/compare/0.0.1...0.0.2) (2026-03-30)


### Features

* add more deployment options for newt ([ca2ac1d](https://github.com/home-operations/pangolin-operator/commit/ca2ac1d6620f4e01a5087056fd572271fd475e79))
* drop siteNamespace ref ([b30e4c8](https://github.com/home-operations/pangolin-operator/commit/b30e4c8abd2214be251e9da878b885b40d08d0e6))


### Bug Fixes

* cache only secrets managed-by pangolin-operator ([99bddf1](https://github.com/home-operations/pangolin-operator/commit/99bddf1ee8ae1db97bd7679be93f126f4e345b0d))


### Documentation

* add permissions for api key ([8a285b3](https://github.com/home-operations/pangolin-operator/commit/8a285b3646efda5acf860e5821b8ae4056d1caca))


### Miscellaneous Chores

* add missing tests and update readme ([904b37e](https://github.com/home-operations/pangolin-operator/commit/904b37e2e5b2120c8585525652c30c90ad705712))
* improve charts and login ([f93e84e](https://github.com/home-operations/pangolin-operator/commit/f93e84eadbedf761d9b3feb14fd1c8afb9f725a2))

## 0.0.1 (2026-03-30)


### Features

* transform newt-sidecar into an operator ([9f591fd](https://github.com/home-operations/pangolin-operator/commit/9f591fdd23357fb6afdb6ae0a5a2c1f09de670d3))


### Miscellaneous Chores

* **versioning:** reset release please version ([1588d0d](https://github.com/home-operations/pangolin-operator/commit/1588d0d234abdf129d44af8a781156c510640bad))
