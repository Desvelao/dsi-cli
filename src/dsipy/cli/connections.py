from functools import wraps
from typing import List
from pathlib import Path
import typer
from .common import Cli
from ..feeds.opml import generate_opml_from_vcards
from ..core.identity import VCardInputs

app = Cli(help="Commands related to connections processing", no_args_is_help=True)


@app.command(help="Generate an OPML file from vCard files (files, directories)")
def feed(
    inputs: List[str] = typer.Argument(
        ..., help="Directories to retrieve the vCards (.vcf or .vcard files)"
    ),
    output: Path = typer.Option(None, "--output", "-o", help="Output OPML file"),
):
    vcard_inputs = VCardInputs(inputs)
    for warning in vcard_inputs.warnings:
        typer.secho(warning, fg=typer.colors.RED)

    if len(vcard_inputs.vcard_files) == 0:
        typer.secho(
            f"❌ No vCard files found in the specified directory or files: {inputs}",
            fg=typer.colors.RED,
        )
        raise typer.Exit(code=1)

    opml_warnings: List[str] = []
    try:
        opml = generate_opml_from_vcards(vcard_inputs.vcard_files, opml_warnings)
    except ValueError as e:
        for warning in opml_warnings:
            typer.secho(warning, fg=typer.colors.RED)
        typer.secho(f"❌ {e}", fg=typer.colors.RED)
        raise typer.Exit(code=1)
    for warning in opml_warnings:
        typer.secho(warning, fg=typer.colors.RED)

    # Write the OPML to the output file
    if output:
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(opml, encoding="utf-8")
        typer.secho(f"✅ OPML file generated: {output}")
    else:
        print(opml)


if __name__ == "__main__":
    app()
