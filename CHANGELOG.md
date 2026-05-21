# Changelog

## 0.1.0 — 2026-05-20

Initial release. Implements the ReproForge SRS v1 in full, including all P0
(FR-001..FR-020), all P1 (FR-021..FR-029) and the cross-CI-migration P2
requirement FR-033.

### Added

- **CLI** `reproforge` with subcommands: `init`, `collect`, `from-run`,
  `capsule create/inspect/extract`, `diagnose`, `report`, `replay`,
  `verify`, `verify-patch`, `flake`, `patch`, `lint`, `shell`, `migrate`,
  `history show/stats`, `schema`.
- **Provider abstraction** (FR-025): `internal/provider` defines a
  `Provider` interface implemented by `internal/github` (GitHub Actions)
  and `internal/gitlab` (pipelines + jobs + traces + artifacts via
  GitLab REST). The CLI auto-detects the provider from the run URL and
  also accepts `--provider github_actions|gitlab_ci` explicitly.
- **GitHub Actions adapter** (FR-002, FR-003): pinned `X-GitHub-Api-Version:
  2022-11-28`, manual redirect handling so the bearer token never leaks
  into AWS-presigned URLs.
- **Capsule format** (FR-008, FR-009): `reproforge.capsule/v1` manifest +
  tar.zst with per-file sha256, `checksums.txt`, traversal-safe extraction,
  strict JSON unknown-field rejection.
- **Redaction** (FR-007, NFR-001): GitHub/AWS/Azure/GCP/Slack/Stripe/
  OpenAI/Anthropic/Google/npm/PEM/Bearer/JWT plus a key=value heuristic and
  user-extensible patterns/denylist; secrets never appear in any artifact
  the tool writes (capsule, report, AI prompt).
- **Fingerprint** (FR-018): stable `sha256:...` normalising runner paths,
  hex blobs, UUIDs, long numbers and dependency lists.
- **Parsers**: log scanner (network/dns/tls/timeout/oom/permission/
  dependency/checksum/missing-env/yaml-expr/python-traceback/JS asserts/
  Go panic), JUnit XML, `go test -json`.
- **Diagnose engine** (FR-013, FR-014): rule-based classifier producing
  evidence + next actions + replay commands per category, with confidence
  blending across overlapping signals.
- **Replay engine** (FR-010, FR-011, FR-019): generates a `Dockerfile`,
  `replay.sh`, `failed-step.sh`. Honours `--mode full-job|failed-step|
  test-only|dependency-install`, `--network allow|deny`, `--memory`,
  `--cpus`, `--env-from KEY` allowlist.
- **Resource profiling** (FR-026): `--profile` polls `<runtime> stats`
  at 1Hz and writes `replay/resource-profile.json` with peak CPU/memory.
- **Interactive shell** (FR-027): `reproforge shell <capsule>` opens a
  bash inside the freshly built replay container with the same network/
  memory/env constraints, the entrypoint set to `/bin/bash`.
- **Cache strategy** (FR-028): `internal/cache` extracts dependency-cache
  hints from a workflow (npm, pnpm, yarn, pip, go, maven, gradle), without
  copying credentials. Hints are stored under `cache/cache-hints.json` in
  the capsule and surfaced by the replay engine.
- **Workflow lint** (FR-024): `internal/wflint` ships a hand-written
  GitHub Actions linter with rules for missing `permissions:`,
  `pull_request_target` + PR-head checkout, floating action references,
  secrets echoed in `run:` blocks, self-hosted runners with no labels,
  and meaningless `if: ${{ secrets.X != '' }}` guards. Findings are
  embedded in the capsule under `workflow/lint.json` when collected
  with `--lint`.
- **Patch verification** (FR-022, FR-029): `verify-patch` applies a
  unified diff inside a temp copy of the source (git apply preferred,
  patch -p1 fallback), runs the failed-step replay before AND after the
  patch, computes both fingerprints, and only reports `verified: true`
  when the failure reproduces on HEAD AND no longer reproduces on the
  patched tree.
- **Flake detector** (FR-017, FR-023): rerun loop with optional network
  toggling and `REPROFORGE_SEED`/`PYTHONHASHSEED` variation; quarantine
  suggestion is evidence-only, never silently disables a test.
- **Reporter** (FR-015): Markdown / JSON / SARIF / GitHub-issue templates
  including failing tests, evidence and replay/flake outcome blocks.
- **AI adapter** (FR-021): Claude / OpenAI / local stub / disabled,
  with the same redactor applied to every prompt body.
- **Local SQLite history** (FR-016): pure-Go modernc.org/sqlite store for
  runs, flakes, and replay outcomes, queryable via `reproforge history`.
- **Migration** (FR-033): `reproforge migrate <capsule>` emits a portable
  Bash script and Earthfile that reproduce the failing job outside GitHub
  Actions.
- **Curated fixtures**: `python-deterministic`, `node-flaky`, `go-network`,
  `missing-secret`, exercised by an integration test that loads each one
  through the diagnose pipeline.
- **TypeScript GitHub Action wrapper**: Node 20 entry built using only
  built-in modules (no `node_modules` at runtime), TypeScript source +
  hand-written CommonJS dist.
- **Tests**: 31 `_test.go` files covering unit, race, fixture-driven
  diagnosis, redaction-leak corpus, replay against a fake docker runtime,
  flake detector against the same fake runtime, GitLab adapter against
  an in-process fake server, GitHub adapter against an in-process fake
  server (including 302-to-presigned-URL handling), capsule pack/unpack
  round-trip, JSON/Markdown/SARIF rendering, end-to-end CLI runs.
- **CI**: pinned `actions/checkout`, `actions/setup-go`, `actions/setup-node`
  by SHA, govulncheck job, race-mode test job, action self-test job,
  binary smoke test job.


## 0.2.0 — 2026-05-20

Closes the SRS P2 list. Now every FR-001..FR-033 has working code, tests
and (where applicable) a real-runtime smoke check.

### Added

- **FR-030 ML ranking layer** — `internal/mlrank` trains a small Naive
  Bayes model from the local SQLite history and re-ranks the rule-based
  diagnosis confidences. Calibration preserves the rule floor (NFR-008).
  `internal/diagnose.Rerank()` is the entry point; it is a no-op when
  the store is empty.
- **FR-031 self-hosted server** — `internal/server` ships a small,
  opt-in HTTP service with `POST /api/v1/capsules`, `GET /capsules/:fp`,
  `GET /diagnoses/:fp`, `GET /healthz`. Bearer-token auth, content-addressed
  storage, body size cap. CLI: `reproforge serve --token T`.
- **FR-032 strace/ltrace mode** — `internal/replay/trace.go` injects an
  in-container install + `strace -f` (or `ltrace`) prefix into the
  failed-step. New flag `reproforge replay --trace strace|ltrace`.
- **TypeScript wrapper compiles** — `tsconfig.json` now sets
  `ignoreDeprecations: 6.0` so the action source compiles with TS 7
  forward-compatibility; the hand-written `dist/index.js` and the
  tsc-emitted output were verified to be functionally equivalent.

### Changed

- `internal/store` gained `HistoryByCategory` so the ML layer can pull
  per-category histories without inventing new schemas.
