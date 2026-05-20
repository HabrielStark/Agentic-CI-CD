# Fixture: missing CI secret

A workflow that requires `GITHUB_TOKEN` to call an internal API but the token
is not exposed to the step. Used to validate the missing-secret diagnosis path.

Expected diagnosis: `missing_secret`.
