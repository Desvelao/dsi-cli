import pytest
from typer import rich_utils


@pytest.fixture(autouse=True)
def plain_cli_output(monkeypatch):
    """Keep rich/typer help output plain and wide (CI forces a colored 80-col terminal)."""
    monkeypatch.setattr(rich_utils, "FORCE_TERMINAL", False)
    monkeypatch.setattr(rich_utils, "MAX_WIDTH", 200)
    monkeypatch.delenv("FORCE_COLOR", raising=False)
    monkeypatch.setenv("NO_COLOR", "1")
    monkeypatch.setenv("TERM", "dumb")
    monkeypatch.setenv("COLUMNS", "200")
