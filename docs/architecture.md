# ReproForge CI — Architecture

ReproForge has six logical layers; each is a Go package with a documented
public interface and an isolated test suite.

```text
+-----------------------------------------------------------------+
|                          CLI (cobra)                            |
|       cmd/reproforge  |  internal/cli (init/collect/...)        |
+-----------------------+-----------------------------------------+
                        |
+-----------------------v-----------------------------------------+
|       Collection orchestrator (internal/collect)                |
|    fetch run -> redact -> parse logs -> compute fingerprint     |
|                  -> build manifest -> write capsule             |
+--------+---------+---------+---------+--------------------------+
         |         |         |         |
         v         v         v         v
+-----------+ +---------+ +---------+ +-----------+ +-----------+
| github    | | redact  | | parsers | | fingerprint | | capsule  |
| API client| | engine  | | logs+xml| | sha256       | | tar.zst   |
+-----------+ +---------+ +---------+ +-----------+ +-----------+
                                            |
                                            v
                                      +------------+
                                      | diagnose   |
                                      |  rules     |
                                      +-----+------+
                                            |
                                            v
                                      +------------+
                                      | report     |
                                      | md/json/   |
                                      | sarif      |
                                      +------------+
```

## Data flow

1. `cli.from-run <url>` invokes `collect.FromRun(ref, opts)`.
2. `github.Client` fetches the run, jobs, workflow YAML and run-logs zip.
3. Each log file passes through `redaction.Engine` (default rules + user
   patterns + denylist) and the redacted body is staged under `<out>/capsule/logs/<job>/...`.
4. `parsers.ScanLog` extracts hits per category, `parsers.JUnitXML`
   processes any artifacts, and the result feeds the fingerprint.
5. `fingerprint.Compute` builds a stable `sha256:...` from a normalised tuple
   (step, command, error class, top frame, failing tests, deps).
6. The manifest is validated against `internal/capsule.Validate()` and the
   on-disk JSON schema (`schemas/capsule-v1.json`).
7. `capsule.PackFile` writes the deterministic tar.zst with `checksums.txt`.
8. Optional `diagnose.Classify` produces a structured diagnosis; `report.*`
   renders it for humans and machines.

## Replay engine

The replay engine is independent of the collector. Given a capsule and an
extracted source tree, it generates:

- `Dockerfile` — minimal Ubuntu base (configurable) with `python3`, `git`,
  `jq`, `curl`, `build-essential`, etc.
- `replay.sh` — dispatcher honouring `REPROFORGE_MODE`.
- `failed-step.sh` — exact command that failed, ready to re-execute.

Container is started with the requested network policy (`--network none`
for `deny`), memory/CPU limits, and only the env vars in the allowlist.

## Flake detector

`flake.Runner.Run(...)` runs the replay engine N times in series, optionally
toggling the network policy and `REPROFORGE_SEED`/`PYTHONHASHSEED` between
runs. A target is reported as flaky if both pass and fail outcomes are
observed.

## AI adapter

`ai.NewAdapter(provider)` returns a thin shim for Claude, OpenAI or a local
stub. The same `SanitisedPrompt(...)` builder is used for every backend and
applies the redaction engine to every body the request will carry. The
adapter never decides "verified" on its own — verification is the job of
the replay engine after a human reviews and applies the diff.
