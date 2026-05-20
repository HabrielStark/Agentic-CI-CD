# ReproForge — Security model

## Threat model

ReproForge handles raw CI logs, which routinely contain:

- API tokens (GitHub, OpenAI, npm, AWS, GCP, ...)
- bearer headers, PEM private keys, JWTs
- internal hostnames and credentials
- artifact contents (binaries, archives, test data)

Our threat model assumes:

- the **CLI runs locally** (or inside a private CI runner) with full read
  access to the run; this is necessary to download logs.
- the **capsule** may be shared (committed to issues, PRs, attached to bug
  reports). Therefore the capsule MUST NOT contain unredacted secrets.
- **AI providers** are external. Therefore prompts MUST NOT contain
  unredacted secrets.

## Controls

1. **Default redactor** at `internal/redaction`.
   - Rules cover GitHub/AWS/Azure/GCP/Slack/Stripe/OpenAI/Anthropic/Google/npm/PEM/Bearer/JWT and a `key=value` heuristic for high-entropy values after `secret`/`password`/`token`/`api_key`.
   - Adds an explicit denylist for the value of every well-known secret env
     name (`GITHUB_TOKEN`, `AWS_*`, `*_KEY`, `OPENAI_API_KEY`, ...).
   - Each hit is reported with sha256+length only — no plaintext escapes.
   - Custom rules can be added via `.reproforge/config.yaml > redaction.patterns / denylist`.

2. **Capsule integrity**:
   - `checksums.txt` is generated at pack time with sha256 per file.
   - `Unpack` rejects path traversal, missing files, mismatched hashes, and
     manifest disagreement. A capsule is either valid or refused.

3. **Replay isolation**:
   - Container is built with no host paths mounted. Source comes from
     a copy under `/work/source`.
   - `--network=deny` translates to `--network=none` for docker/podman.
   - `--memory` and `--cpus` are surfaced for resource-bounded runs.
   - Env passthrough is opt-in via `--env-from <KEY>`; values come from the
     caller's environment, never from the capsule.

4. **AI safety**:
   - The same redactor runs over command, evidence, snippets and diagnosis
     before any AI call.
   - The system prompt forbids removing or weakening tests, adding secrets
     or modifying unrelated files.
   - The CLI does NOT apply patches automatically. The patch is only
     "verified" if `reproforge verify` reproduces the original failure on
     `HEAD` and confirms the diff fixes it on a candidate branch.

5. **Supply-chain hygiene**:
   - All third-party Go modules are explicit in `go.mod`/`go.sum` (see also
     `go mod verify` in CI).
   - The GitHub Action ships as a Node 20 entry that uses **only** the
     standard library — no `node_modules` are required at runtime.
   - All workflow `uses:` lines are pinned to commit SHAs.

## Reporting a security issue

Open an issue prefixed with `security:` or email security@reproforge.dev. Do
NOT include real secrets in the report; ReproForge will redact them on its
own once you attach the capsule.
