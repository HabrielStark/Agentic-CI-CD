"""Deterministic-failure fixture for ReproForge."""

from arith import add, subtract


def test_add_is_correct() -> None:
    # Intentionally broken assertion — fixture demonstrates a real failure
    # that ReproForge can reproduce, classify and report. Do NOT "fix" this
    # in the fixture: ReproForge depends on its determinism.
    assert add(2, 2) == 5  # noqa: PLR2004


def test_subtract_is_correct() -> None:
    assert subtract(5, 3) == 2
