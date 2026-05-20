# Changelog

## 0.1.0 — 2026-05-20

Initial release. Implements the ReproForge SRS v1 milestone in full:

### Added

- Go CLI `reproforge` with subcommands: `init`, `collect`, `from-run`,
  `capsule create/inspect/extract`, `diagnose`, `report`, `replay`,
  `verify`, `flake`, `patch`, `schema`.
- GitHub Actions provider (`internal/github`): runs, jobs, run-logs zip,
  workflow YAML, artifacts. Pins API version `2022-11-28`. Manual redirect
  handling so the bearer token never leaks into AWS-presigned URLs.
- Capsule manifest schema (`reproforge.capsule/v1`) with strict validation
  and JSON-schema (`schemas/capsule-v1.json`).
- Pack/Unpack for tar.zst capsules with `checksums.txt`, manifest hash
  cross-check, and path-traversal-safe extraction.
- Multi-rule secret redactor with default coverage for GitHub/AWS/Azure/
  GCP/Slack/Stripe/Anthropic/OpenAI/Google/npm/PEM/Bearer/JWT plus a
  user-extensible pattern + denylist set.
- Stable failure fingerprint that normalises runner paths, hex blobs, UUIDs,
  long numbers and trims dependency lists.
- Log/JUnit/go-test-json parsers used by the diagnoser.
- Rule-based diagnoser covering the SRS § 9 categories with a confidence
  blend across overlapping signals.
- Replay engine that emits a Dockerfile + `replay.sh` + `failed-step.sh`
  and, when docker/podman is available, runs them with `--network`,
  `--memory` and `--cpus` constraints.
- Flake detector (rerun loop) with optional network and seed variation.
- Reporter for Markdown / JSON / SARIF / GitHub-issue templates.
- AI adapter (Claude, OpenAI, local stub, disabled) with a sanitisation
  layer that runs the redactor before any prompt leaves the process.
- Local SQLite history (`internal/store`) using pure-Go modernc.org/sqlite.
- Curated fixtures: `python-deterministic`, `node-flaky`, `go-network`,
  `missing-secret`.
- TypeScript GitHub Action wrapper using only Node 20 built-ins (no
  `node_modules` at runtime).
- CI workflow with race/vet/govulncheck/binary-smoke jobs and pinned
  third-party `uses:` SHAs.
