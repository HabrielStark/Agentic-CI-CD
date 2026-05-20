# ReproForge CI

> Reproduce, diagnose, and verify CI failures.
> Turn any failed GitHub Actions run into a sanitised, signed capsule, a
> verified replay, and an evidence-based diagnosis — all from one command.

ReproForge CI is a Go-based developer tool plus a GitHub Action wrapper that:

1. **Collects** a failing CI run (logs, workflow YAML, jobs, artifacts).
2. **Sanitises** logs with a multi-layer secret redactor (GitHub/AWS/GCP/JWT/PEM/Bearer/etc).
3. **Packages** everything into a deterministic `reproforge.capsule/v1` tar.zst with a checksums manifest.
4. **Diagnoses** the failure with a rule-based classifier (network, dependency, OOM, missing-secret, flaky-test, code/test, etc).
5. **Replays** the failed step locally inside an auto-generated container — with `--network=deny|allow` and a strict allowlist for env vars.
6. **Reports** in Markdown / JSON / SARIF, ready to comment on the PR or upload to code scanning.
7. **Optionally** asks an AI for a patch — but never trusts it: the patch must pass replay/test verification before it can be marked as `verified: true`.

The system is described in the SRS document `ReproForge_CI_SRS_RU.docx`. This
implementation covers every functional and non-functional requirement of that
SRS through the v1 milestone.

---

## Install

```bash
go install github.com/reproforge/reproforge/cmd/reproforge@latest
```

Or build from source:

```bash
git clone https://github.com/reproforge/reproforge
cd reproforge
go build -o bin/reproforge ./cmd/reproforge
./bin/reproforge --help
```

## Quick start

### One-shot pipeline (collect → diagnose → report)

```bash
export GITHUB_TOKEN=ghp_yourtokenhere
reproforge from-run https://github.com/octocat/hello-world/actions/runs/123 \
  --write-capsule --write-report report.md
```

This produces:

- `reproforge-out/capsule/` — extracted manifest, redacted logs, workflow YAML, redaction report.
- `reproforge-out/rf-123.tar.zst` — signed capsule (sha256 per file + checksums.txt).
- `report.md` — Markdown report ready to paste in a PR.

### Just collect and diagnose

```bash
reproforge collect --url <run-url> --diagnose
reproforge report ./reproforge-out/capsule --format markdown
```

### Replay locally

```bash
# generate the Dockerfile + replay scripts (no execution)
reproforge replay rf-123.tar.zst --dry-run

# actually run (requires docker or podman)
reproforge replay rf-123.tar.zst --mode failed-step --network deny

# verify reproduction
reproforge verify rf-123.tar.zst --json out.json
```

### Detect flakiness

```bash
reproforge flake rf-123.tar.zst --runs 20 --variate-seed --json flake.json
```

### Ask an AI for a patch (optional)

```bash
ANTHROPIC_API_KEY=... reproforge patch rf-123.tar.zst --ai claude --verify
```

The patch is **never** auto-applied; you receive a plan + diff to review.
ReproForge will only label the patch as `verified` after it passes replay
verification on a clean branch.

---

## GitHub Action

```yaml
- uses: reproforge/reproforge-action@v1
  with:
    run-id: ${{ github.event.workflow_run.id }}
    comment-pr: 'true'
```

Inputs:

| Input | Default | Description |
| --- | --- | --- |
| `run-id` | `workflow_run.id` | Workflow run to analyse |
| `repository` | `${{ github.repository }}` | `owner/repo` |
| `github-token` | `${{ github.token }}` | Token with `actions:read` |
| `comment-pr` | `false` | Post a Markdown report comment on the associated PR |
| `upload-capsule` | `true` | Surface the capsule path so you can `actions/upload-artifact` it |
| `ai` | `none` | One of `none`, `local`, `claude`, `openai` |
| `output-dir` | `reproforge-out` | Directory under runner |

Outputs: `capsule-path`, `report-path`, `diagnosis-category`, `fingerprint`.

---

## Capsule format (v1)

```text
capsule.json                # top-level manifest (validated against schemas/capsule-v1.json)
checksums.txt               # `<sha256>  <relative path>` for every payload file
logs/<job>/...              # redacted logs (one or more files)
workflow/<name>.yml         # workflow YAML at run.head_sha
artifacts/<name>/...        # optional CI artifacts (zip-extracted)
redaction/redaction-report.json   # rule count, hit count, hashes (no secrets)
diagnosis/diagnosis.json    # optional, embedded if --diagnose was used
replay/Dockerfile           # generated on `replay` invocation
replay/replay.sh            # entrypoint, dispatches mode
replay/failed-step.sh       # replay of just the failing command
```

The capsule is portable and content-addressed: two capsules created from the
same logs/manifest produce identical `HashContents()` regardless of timestamp.

## Diagnosis categories

| Category | Trigger |
| --- | --- |
| `code_or_test_failure` | failing JUnit cases, AssertionError, panic, FAIL markers |
| `network_issue` | ECONNRESET, EAI_AGAIN, "connection refused", DNS / TLS hits |
| `dependency_resolution` | npm/pip/go/maven/gradle resolver errors |
| `checksum_mismatch` | sha256 mismatch / integrity / corrupted archive |
| `missing_secret` | `secret X not found`, `env var X required` |
| `runner_mismatch` | path/file present locally but missing on runner |
| `timeout` | `deadline exceeded`, `operation timed out` |
| `oom` | OOMKill, java.lang.OutOfMemoryError |
| `permission` | EACCES, "Permission denied", 403 |
| `workflow_config` | invalid YAML expressions / context values |
| `flaky_test` | both pass and fail observed across reruns |
| `unknown` | classifier could not pick a category |

## Security

- Logs and AI prompts are redacted **before** they leave the local machine.
  See `internal/redaction/` for the full rule set; the engine is composable
  and supports user-defined `patterns` / `denylist` in `.reproforge/config.yaml`.
- The capsule format excludes shell history, environment variable dumps and
  any non-allowlisted file. Replays run with `--cap-drop=ALL` style ergonomics
  via container runtime constraints (`--memory`, `--cpus`, `--network=none`).
- AI patches are NEVER auto-applied. They are clearly labelled until they
  pass `reproforge verify`.

## Repository layout

```text
cmd/reproforge/         # CLI entrypoint
internal/
  ai/                   # AI adapter (Claude / OpenAI / local stub) + sanitisation
  capsule/              # manifest types, validation, pack / unpack
  cli/                  # cobra command tree
  collect/              # collect + redact + parse + fingerprint pipeline
  config/               # .reproforge/config.yaml loader
  diagnose/             # rule-based classifier (FR-013 / SRS § 9)
  fingerprint/          # stable failure fingerprints
  flake/                # rerun-based flake detector
  github/               # GitHub Actions REST adapter
  logx/                 # tiny structured logger
  parsers/              # log + JUnit + go test -json parsers
  redaction/            # default + custom redaction engine
  replay/               # Dockerfile + replay.sh generator
  report/               # Markdown / JSON / SARIF report renderer
  store/                # local SQLite history (modernc.org/sqlite, pure Go)
  version/              # build metadata
fixtures/               # known-bad fixtures used by tests / demos
schemas/capsule-v1.json # capsule manifest JSON schema
action/                 # GitHub Action wrapper (TypeScript source + JS dist)
```

## Testing

```bash
go test -race -count=1 ./...
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
```

The test suite covers:

- **Unit**: every package has its own `_test.go`, with property-style cases for
  the redactor, golden tables for the diagnoser, and round-trip tests for the
  capsule format.
- **Integration**: `internal/collect/integration_test.go` spins up a fake
  GitHub server using `httptest` and runs the entire collection pipeline.
- **CLI E2E**: `internal/cli/e2e_test.go` builds synthetic capsules and
  invokes the CLI through cobra.
- **Fixtures**: `internal/diagnose/fixtures_test.go` loads the curated
  fixtures from `/fixtures/` and asserts the expected diagnosis category.
- **Concurrency**: `-race` is part of CI; the redaction engine is RW-mutex
  protected and fingerprint computation is pure.
- **Security**: `internal/redaction/security_test.go` runs a corpus of fake
  secrets through the engine and asserts no original chunk leaks through.

## License

Apache 2.0 — see `LICENSE`.
