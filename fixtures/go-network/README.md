# Fixture: Go network failure

A `go test` job that fetches a remote URL and fails when the registry/network
is unreachable. Used to validate the network/dependency rule branch.

ReproForge expectations:
- classify as `network_issue` (or `dependency_resolution` if go module proxy is involved),
- propose `--network=deny` and `--network=allow` replays to confirm root cause.
