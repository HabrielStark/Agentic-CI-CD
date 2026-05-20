# Fixture: deterministic Python failure

A Python project with a single failing pytest case. Used as a known-deterministic
failure for ReproForge regression tests and demos.

```
pytest -q tests/test_arith.py
# E   AssertionError: assert add(2, 2) == 5
# 1 failed in 0.05s
```

When ReproForge replays this capsule it must:
- reproduce the failure on every run (no flakiness),
- classify it as `code_or_test_failure`,
- list `tests/test_arith.py::test_add_is_correct` as the failing test.
