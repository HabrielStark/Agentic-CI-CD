# ReproForge — Usage

## CLI commands

```text
reproforge init                              create .reproforge/config.yaml
reproforge schema                            print supported capsule schema versions
reproforge from-run <url>                    one-shot: collect + diagnose + report
reproforge collect --url <url>               just collect into ./reproforge-out
reproforge capsule create --url <url>        write a tar.zst capsule
reproforge capsule inspect <capsule>         summarise a tar.zst
reproforge capsule extract <capsule> -d dst  extract a tar.zst
reproforge diagnose <capsule>                classify a capsule
reproforge report <capsule> --format ...     md/json/sarif/issue report
reproforge replay <capsule> [--dry-run]      generate + run replay container
reproforge verify <capsule>                  replay and check exit code matches original
reproforge flake <capsule> --runs N          rerun N times to detect flakiness
reproforge patch <capsule> --ai claude       request a sanitised AI patch plan
```

`<capsule>` accepts either a tar.zst path or an extracted directory.

## Configuration

`.reproforge/config.yaml` (created by `reproforge init`):

```yaml
provider: github_actions
runtime: auto              # docker | podman | auto
outputDir: ./reproforge-out

redaction:
  patterns:                 # extra regexes
    - '(?i)CompanyInternalToken-[A-Z0-9]{20,}'
  denylist:                 # exact substrings to redact on sight
    - "internal.example.com"

replay:
  image: ""                 # auto from runner OS by default
  memory: "4g"
  cpus: 2.0
  network: configurable     # allow | deny | configurable
  timeoutSec: 1800
  envAllowlist:
    - GITHUB_RUN_ID
    - CI

reporting:
  markdown: true
  json: true
  sarif: false

ai:
  provider: none            # none | local | claude | openai
  model: claude-3-5-sonnet-latest
  verify: true

github:
  apiBase: https://api.github.com
```

## Environment variables

| Var | Purpose |
| --- | --- |
| `GITHUB_TOKEN` / `GH_TOKEN` | Token with `actions:read` |
| `ANTHROPIC_API_KEY` | Required when `--ai claude` |
| `OPENAI_API_KEY` | Required when `--ai openai` |
| `REPROFORGE_LOG` | `debug` / `info` / `warn` / `error` |
| `REPROFORGE_LOG_JSON=1` | Emit logs as JSON |

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | User-facing error (bad input, validation failure) |
| 2 | Internal failure (network, container runtime) |
| 64..78 | Reserved for replay subprocess (matches sysexits.h) |
