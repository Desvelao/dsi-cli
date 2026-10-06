from functools import wraps
import logging
import difflib
import os
import traceback

import typer
from rich.syntax import Syntax

try:  # newer typer vendors click; its errors are not the installed click's
    from typer._click.exceptions import ClickException, UsageError
except ImportError:  # typer<=0.2x depends on click
    from click import ClickException, UsageError

DEBUG_ENV = "DSIPY_DEBUG"


def debug_enabled() -> bool:
    """True when DSIPY_DEBUG is set to anything other than empty/0/false."""
    return os.environ.get(DEBUG_ENV, "").strip().lower() not in ("", "0", "false")


def cmd_error_handler(message: str):
    def decorator(func):
        @wraps(func)
        def wrapper(*args, **kwargs):
            try:
                return func(*args, **kwargs)
            except Exception as e:
                if isinstance(e, (typer.Exit, typer.Abort, ClickException)):
                    raise
                typer.secho(f"❌ {message}: {str(e)}", fg=typer.colors.RED)
                if debug_enabled():
                    typer.echo(traceback.format_exc(), err=True)
                raise typer.Exit(code=1)

        return wrapper

    return decorator


class Cli(typer.Typer):
    """Custom Typer application that automatically wraps all commands with a generic error handler."""

    def __init__(self, *args, logger: logging.Logger = None, **kwargs):
        super().__init__(*args, **kwargs)
        self.logger = logger or logging.getLogger(__name__)

    def command(self, *args, **kwargs):
        """Override the command decorator to wrap all commands with a generic error handler."""
        original_decorator = super().command(*args, **kwargs)

        def decorator(func):
            wrapped_func = cmd_error_handler(f"Command '{func.__name__}' failed")(func)
            return original_decorator(wrapped_func)

        return decorator


def echo_keypair_generated(priv, pub, pub_b64: str) -> None:
    """Print the outcome of a keypair generation."""
    typer.secho(
        f"✅ Keypair generated and saved to '{priv}' and '{pub}'", fg=typer.colors.GREEN
    )
    typer.secho(
        f"📋 Public key (Base64-encoded DER for vCard): {pub_b64}", fg=typer.colors.BLUE
    )


def show_diff(
    console, old_text: str, new_text: str, fromfile: str, tofile: str
) -> None:
    """Print a colored unified diff (or a "No differences." note) to `console`."""
    diff = "\n".join(
        difflib.unified_diff(
            old_text.splitlines(),
            new_text.splitlines(),
            fromfile=fromfile,
            tofile=tofile,
            lineterm="",
        )
    )
    if diff.strip():
        console.print(Syntax(diff, "diff", theme="ansi_dark", line_numbers=False))
    else:
        console.print("[green]  No differences.[/green]")
