import typer
from typer.testing import CliRunner

from src.dsipy.cli.common import Cli, UsageError

runner = CliRunner()


def make_app():
    app = Cli()

    @app.command()
    def boom():
        raise ValueError("kaput")

    @app.command()
    def abort():
        raise typer.Abort()

    @app.command()
    def usage():
        raise UsageError("bad usage")

    @app.command()
    def badparam():
        raise typer.BadParameter("bad value")

    @app.command()
    def ok():
        pass

    return app


def test_generic_error_no_traceback(monkeypatch):
    monkeypatch.delenv("DSIPY_DEBUG", raising=False)
    r = runner.invoke(make_app(), ["boom"])
    assert r.exit_code == 1
    assert "Command 'boom' failed: kaput" in r.output
    assert "Traceback" not in r.output


def test_generic_error_debug_traceback(monkeypatch):
    monkeypatch.setenv("DSIPY_DEBUG", "1")
    r = runner.invoke(make_app(), ["boom"])
    assert r.exit_code == 1
    assert "failed: kaput" in r.output
    assert "Traceback" in r.output


def test_debug_false_values_disabled(monkeypatch):
    monkeypatch.setenv("DSIPY_DEBUG", "0")
    r = runner.invoke(make_app(), ["boom"])
    assert "Traceback" not in r.output


def test_abort_propagates(monkeypatch):
    monkeypatch.delenv("DSIPY_DEBUG", raising=False)
    r = runner.invoke(make_app(), ["abort"])
    assert r.exit_code == 1
    assert "Aborted" in r.output
    assert "failed" not in r.output


def test_usage_error_propagates():
    r = runner.invoke(make_app(), ["usage"])
    assert r.exit_code == 2
    assert "bad usage" in r.output
    assert "failed" not in r.output


def test_bad_parameter_propagates():
    r = runner.invoke(make_app(), ["badparam"])
    assert r.exit_code == 2
    assert "bad value" in r.output
    assert "failed" not in r.output


def test_main_app_debug_flag(monkeypatch):
    from src.dsipy.cli.app import main_app

    monkeypatch.delenv("DSIPY_DEBUG", raising=False)
    r = runner.invoke(main_app, ["--debug", "--help"])
    assert r.exit_code == 0
    assert "--debug" in r.output
