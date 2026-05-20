# Fixture: flaky Node.js test (timing-based)

A small Vitest test that intermittently fails because it relies on a 50ms timer
in a busy event loop. Used to validate ReproForge's flake detector.

When ReproForge runs `flake --runs 20` against this fixture's capsule it must:
- detect both pass and fail outcomes,
- mark the result as flaky,
- avoid silently quarantining the test (suggestion only, with evidence).
