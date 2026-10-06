import os

import typer
from .common import DEBUG_ENV
from .vcard import app as vcard_app
from .feeds import app as feeds_app
from .connections import app as connections_app
from .key import app as key_app

main_app = typer.Typer(
    help="DSI Tools: A collection of CLI tools for working with vCards, feeds, connections, and keys"
)

# Add the apps as a subcommands to the main app
main_app.add_typer(vcard_app, name="vcard")
main_app.add_typer(feeds_app, name="feeds")
main_app.add_typer(
    connections_app,
    name="connections",
)
main_app.add_typer(
    key_app,
    name="key",
)


# If no subcommand is provided, show the help message
@main_app.callback(invoke_without_command=True)
def callback(
    ctx: typer.Context,
    debug: bool = typer.Option(
        False, "--debug", help=f"Print tracebacks on errors (or set {DEBUG_ENV}=1)."
    ),
):
    if debug:
        os.environ[DEBUG_ENV] = "1"
    if ctx.invoked_subcommand is None:
        typer.echo(ctx.get_help())


if __name__ == "__main__":
    main_app()
