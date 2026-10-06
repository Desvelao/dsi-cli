from datetime import datetime, timezone
import json
import re
import os
from pathlib import Path
from rich.progress import Progress
import sys
import typer
from typing import List
from .common import Cli, echo_keypair_generated, show_diff as _show_diff
from ..vcard.qr import generate_qr
from ..crypto.keys import (
    action_generate_keypair,
    load_private_key_pem,
    load_public_key_b64_der,
)
from ..core.identity import VCard, VCardInputs
from ..core.canonical import normalize_vcard
from ..core.model import vcard_main_attributes
from ..core.validator import ValidationResult, validate_profile
from ..endorsements.verify import verify_endorsements

app = Cli(help="Commands related to vCard processing", no_args_is_help=True)


@app.command()
def create(
    output: Path = typer.Option(
        Path("dsi-card.vcf"), "--output", "-o", help="Output file to save the vCard"
    ),
    interactive: bool = typer.Option(
        False, "--interactive", "-i", help="Interactive prompts", is_flag=True
    ),
    fn: str = typer.Option(
        vcard_main_attributes["fn"]["default"],
        help=vcard_main_attributes["fn"]["description"],
    ),
    n: str = typer.Option(
        vcard_main_attributes["n"]["default"],
        help=vcard_main_attributes["n"]["description"],
    ),
    nickname: str = typer.Option(
        vcard_main_attributes["nickname"]["default"],
        help=vcard_main_attributes["nickname"]["description"],
    ),
    lang: str = typer.Option(
        vcard_main_attributes["lang"]["default"],
        help=vcard_main_attributes["lang"]["description"],
    ),
    gender: str = typer.Option(
        vcard_main_attributes["gender"]["default"],
        help=vcard_main_attributes["gender"]["description"],
    ),
    email: str = typer.Option(
        vcard_main_attributes["email"]["default"],
        help=vcard_main_attributes["email"]["description"],
    ),
    categories: str = typer.Option(
        vcard_main_attributes["categories"]["default"],
        help=vcard_main_attributes["categories"]["description"],
    ),
    note: str = typer.Option(
        vcard_main_attributes["note"]["default"],
        help=vcard_main_attributes["note"]["description"],
    ),
    url: str = typer.Option(
        vcard_main_attributes["url"]["default"],
        help=vcard_main_attributes["url"]["description"],
    ),
    source: str = typer.Option(
        vcard_main_attributes["source"]["default"],
        help=vcard_main_attributes["source"]["description"],
    ),
    bday: str = typer.Option(
        vcard_main_attributes["bday"]["default"],
        help=vcard_main_attributes["bday"]["description"],
    ),
    anniversary: str = typer.Option(
        vcard_main_attributes["anniversary"]["default"],
        help=vcard_main_attributes["anniversary"]["description"],
    ),
    kind: str = typer.Option(
        vcard_main_attributes["kind"]["default"],
        help=vcard_main_attributes["kind"]["description"],
    ),
    adr: str = typer.Option(
        vcard_main_attributes["adr"]["default"],
        help=vcard_main_attributes["adr"]["description"],
    ),
    tel: str = typer.Option(
        vcard_main_attributes["tel"]["default"],
        help=vcard_main_attributes["tel"]["description"],
    ),
    impp: str = typer.Option(
        vcard_main_attributes["impp"]["default"],
        help=vcard_main_attributes["impp"]["description"],
    ),
    photo: str = typer.Option(
        vcard_main_attributes["photo"]["default"],
        help=vcard_main_attributes["photo"]["description"],
    ),
    generate_key: bool = typer.Option(
        False,
        "--generate-key",
        help="Generate a new Ed25519 keypair and save to PEM files (vcard_private.pem and vcard_public.pem)",
        is_flag=True,
    ),
    force: bool = typer.Option(
        False,
        "--force",
        help="With --generate-key, overwrite existing key files",
    ),
    resume: bool = typer.Option(
        False,
        "--resume",
        help="With --interactive, reuse the answers saved in vcard_create.tmp by a "
        "previous run that was cancelled or interrupted as prompt defaults. The file "
        "is kept on cancel/abort so it can be resumed, ignored without --resume, "
        "and deleted after the vCard is saved.",
    ),
):
    """
    Generate a vCard by asking the user for information.
    """

    values = {
        "fn": fn,
        "n": n,
        "nickname": nickname,
        "lang": lang,
        "gender": gender,
        "email": email,
        "categories": categories,
        "note": note,
        "url": url,
        "source": source,
        "bday": bday,
        "anniversary": anniversary,
        "kind": kind,
        "adr": adr,
        "tel": tel,
        "impp": impp,
        "photo": photo,
    }
    custom_attributes = {}
    temp_file = "vcard_create.tmp"
    # Prompt the user for vCard fields if interactive mode is enabled
    if interactive:
        typer.secho("Let's create a new vCard!", fg=typer.colors.CYAN)
        # Only reuse answers from a previous interrupted run when asked to
        prompt_with_temp = _make_temp_prompter(temp_file, resume)

        for key in _PROMPT_ORDER:
            values[key] = prompt_with_temp(
                key, vcard_main_attributes[key]["description"], values[key]
            )

        # Add key
        generate_key = typer.confirm("Do you want to create new keys?", default=False)

        _prompt_feeds(prompt_with_temp, custom_attributes)
        _prompt_social(prompt_with_temp, custom_attributes)
        _prompt_custom_attributes(prompt_with_temp, custom_attributes)

    keys = None
    if generate_key:
        try:
            priv, pub, key = action_generate_keypair(
                Path("vcard_private.pem"), Path("vcard_public.pem"), force=force
            )
        except FileExistsError as e:
            typer.secho(
                f"❌ {e}; refusing to overwrite a key file (use --force).",
                fg=typer.colors.RED,
            )
            raise typer.Exit(code=1)
        echo_keypair_generated("vcard_private.pem", "vcard_public.pem", key)
        keys = [{"alg": "ed25519", "key_b64": key, "pref": 1, "encoding": "b"}]

    # Generate the vCard content
    vcard_content = VCard.build_content(
        **values, custom_attributes=custom_attributes, keys=keys
    )

    # Print a summary of the vCard
    typer.secho("\nSummary of the vCard:", fg=typer.colors.CYAN)
    typer.echo(vcard_content)
    typer.echo("\n")

    # Ask for confirmation before saving
    if interactive:
        confirm_save = typer.confirm(
            "Do you want to save this vCard to the file?", default=True
        )
        if not confirm_save:
            typer.secho(
                "❌ Operation canceled. The vCard was not saved.", fg=typer.colors.RED
            )
            raise typer.Exit(code=1)

    # Write the vCard to the output file
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(vcard_content, encoding="utf-8", newline="")
    typer.secho(f"✅ vCard generated and saved to {output}", fg=typer.colors.GREEN)
    if not values["source"]:
        typer.secho(
            "⚠️  No SOURCE: the card will not pass `vcard validate` until you set one "
            "(--source https://your.host/dsi.vcf).",
            fg=typer.colors.YELLOW,
        )
    if interactive:
        # Saved successfully: the in-progress answers are no longer needed
        for leftover in (temp_file, f"{temp_file}.part"):
            try:
                os.remove(leftover)
            except OSError:
                pass


# Order in which the main attributes are asked for in interactive mode
_PROMPT_ORDER = (
    "fn",
    "n",
    "nickname",
    "lang",
    "gender",
    "email",
    "categories",
    "bday",
    "anniversary",
    "kind",
    "adr",
    "tel",
    "impp",
    "photo",
    "note",
    "url",
    "source",
)


def _make_temp_prompter(temp_file: str, resume: bool):
    """Return a prompt function that saves every answer to the temp file."""
    temp_data = _load_resume_file(temp_file) if resume else {}

    def save_entry_temp(key, value):
        """Save a single entry to the temp file (JSON, written atomically)."""
        temp_data[key] = value
        tmp = f"{temp_file}.part"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(temp_data, f)
        os.replace(tmp, temp_file)

    def prompt_with_temp(key, description, default):
        """Prompt the user and save the value to the temp file."""
        value = typer.prompt(description, default=temp_data.get(key, default))
        save_entry_temp(key, value)
        return value

    return prompt_with_temp


def _prompt_feeds(prompt_with_temp, custom_attributes: dict) -> None:
    """Interactively collect the X-FEED and X-FEED;LANGUAGE attributes."""
    add_feed = typer.confirm(
        "Do you want to add a X-FEED attribute for an RSS feed?", default=False
    )
    if add_feed:
        feed_url = prompt_with_temp("x_feed", "Enter the RSS feed URL", "")
        custom_attributes["X-FEED"] = feed_url

    typer.secho(
        "ℹ️ If you have feeds in different languages, add X-FEED;LANGUAGE:language-region to the vCard."
    )
    custom_feeds = []
    add_feed = typer.confirm(
        "Do you want to add custom X-FEED;LANGUAGE:language-region entries?",
        default=False,
    )

    while add_feed:
        language = prompt_with_temp(
            f"x_feed_language_{len(custom_feeds)}",
            "Enter the language-region (e.g., 'en-US', 'es-ES')",
            "",
        ).strip()
        feed_url = prompt_with_temp(
            f"x_feed_url_{len(custom_feeds)}", "Enter the feed URL", ""
        ).strip()
        custom_feeds.append((language, feed_url))
        add_feed = typer.confirm(
            "Do you want to add another X-FEED;LANGUAGE entry?", default=False
        )

    # Add the X-FEED;LANGUAGE entries to the vCard content
    for language, feed_url in custom_feeds:
        custom_attributes[f"X-FEED;LANGUAGE={language}"] = feed_url


def _prompt_social(prompt_with_temp, custom_attributes: dict) -> None:
    """Interactively collect custom attributes for social media links."""
    add_custom_social = typer.confirm(
        "Do you want to add custom attributes for social media links?",
        default=False,
    )
    while add_custom_social:
        attribute_name = VCard.build_custom_attribute_social_platform(
            typer.prompt(
                "Enter the name of the social platform (it will be prefixed with 'X-SOCIAL;PLATFORM=')"
            )
        )
        attribute_value = prompt_with_temp(
            f"x_{attribute_name}", f"Enter the value for {attribute_name}", ""
        )
        custom_attributes[f"{attribute_name}"] = attribute_value
        add_custom_social = typer.confirm(
            "Do you want to add another social media link?", default=False
        )


def _prompt_custom_attributes(prompt_with_temp, custom_attributes: dict) -> None:
    """Interactively collect custom attributes for other information."""
    add_custom = typer.confirm(
        "Do you want to add custom attributes for other information?",
        default=False,
    )
    while add_custom:
        attribute_name = VCard.build_custom_attribute(
            typer.prompt(
                "Enter the name of the custom attribute (it will be prefixed with 'X-')"
            )
        )
        attribute_value = prompt_with_temp(
            f"x_{attribute_name}", f"Enter the value for X-{attribute_name}", ""
        )
        custom_attributes[f"X-{attribute_name}"] = attribute_value
        add_custom = typer.confirm(
            "Do you want to add another custom attribute?", default=False
        )


def _load_resume_file(path: str) -> dict:
    """Load saved prompt answers; tolerate missing, legacy or malformed files."""
    try:
        with open(path, "r", encoding="utf-8") as f:
            text = f.read()
    except (OSError, UnicodeDecodeError):
        return {}
    try:
        data = json.loads(text)
        if isinstance(data, dict):
            return {str(k): v for k, v in data.items() if isinstance(v, str)}
    except ValueError:
        pass
    # Legacy key=value format: skip lines that do not parse
    result = {}
    for line in text.splitlines():
        key, sep, value = line.partition("=")
        if sep and key.strip():
            result[key.strip()] = value
    return result


@app.command()
def fetch(
    inputs: List[str] = typer.Argument(
        ...,
        help="One or more .vcf files, directories containing .vcf files, or URLs to vCard files.",
    ),
    output_dir: Path = typer.Option(
        None,
        "--output-dir",
        "-o",
        help="Directory to write updated vCards. Defaults to overwriting in place.",
    ),
    dry_run: bool = typer.Option(
        False,
        "--dry-run",
        "-n",
        help="Show what would be updated without writing any files.",
    ),
    backup: bool = typer.Option(
        False,
        "--backup",
        "-b",
        help="Create a .bak copy before overwriting.",
        is_flag=True,
    ),
    show_diff: bool = typer.Option(
        False,
        "--diff",
        "-d",
        help="Show colored unified diff between old and new content.",
    ),
    allow_http: bool = typer.Option(
        False,
        "--allow-http",
        help="Allow plain HTTP URLs (HTTPS is required by default).",
    ),
    verify_source: bool = typer.Option(
        True,
        "--verify-source/--no-verify-source",
        help="Require the fetched vCard SOURCE to match the requested URL.",
    ),
):
    """
    Fetch or update vCards by fetching the remote vCard referenced in their SOURCE property.

    Supports multiple files, directories, URLs, dry-run mode, backups, colored diffs,
    a progress bar, and a final summary report.
    """

    vcard_inputs = VCardInputs(inputs)

    for warning in vcard_inputs.warnings:

        typer.secho(warning, fg=typer.colors.RED)
    total_inputs = len(vcard_inputs.vcard_files) + len(vcard_inputs.vcard_urls)
    if total_inputs == 0:
        typer.secho("No valid .vcf files or URLs provided.", fg=typer.colors.RED)
        raise typer.Exit(code=1)

    if output_dir:
        output_dir.mkdir(parents=True, exist_ok=True)

    # Summary counters
    downloaded = 0
    updated = 0
    unchanged = 0
    skipped = 0
    failed = 0

    with Progress() as progress:
        task = progress.add_task("Processing vCards...", total=total_inputs)

        for url in vcard_inputs.vcard_urls:
            progress.console.print(f"[bold]Processing URL:[/bold] {url}")
            try:
                vcard = VCard(
                    url=url, allow_http=allow_http, verify_source=verify_source
                )
                filename = vcard.path
                destination = output_dir / filename if output_dir else Path(filename)
                new_text = vcard.to_string()
                # An existing destination is refreshed like a file input
                old_text = None
                if destination.is_file():
                    old_text = destination.read_bytes().decode("utf-8")

                if old_text is not None and show_diff:
                    _show_diff(
                        progress.console,
                        old_text,
                        new_text,
                        str(destination),
                        "(fetched)",
                    )

                if old_text is not None and old_text == new_text:
                    progress.console.print(
                        f"  Unchanged: {destination} (already up to date)"
                    )
                    unchanged += 1
                elif dry_run:
                    verb = "update" if old_text is not None else "download"
                    progress.console.print(
                        f"[cyan]  Dry-run: would {verb} {destination}; no changes written.[/cyan]"
                    )
                    if old_text is not None:
                        updated += 1
                    else:
                        downloaded += 1
                else:
                    if old_text is not None and backup:
                        backup_path = destination.with_suffix(
                            destination.suffix + ".bak"
                        )
                        backup_path.write_text(old_text, encoding="utf-8", newline="")
                        progress.console.print(
                            f"[blue]  Backup created:[/blue] {backup_path}"
                        )
                    vcard.to_file(destination)
                    if old_text is not None:
                        progress.console.print(
                            f"[green]  Updated:[/green] {destination}"
                        )
                        updated += 1
                    else:
                        progress.console.print(
                            f"[green]  Downloaded and saved to:[/green] {url} -> {destination.name}"
                        )
                        downloaded += 1
            except Exception as e:
                progress.console.print(f"[red]  Failed to fetch URL {url}: {e}[/red]")
                failed += 1
            progress.update(task, advance=1)

        for file in vcard_inputs.vcard_files:
            progress.console.print(f"[bold]Processing:[/bold] {file}")

            try:
                vcard = VCard(path=file)
            except Exception as e:
                progress.console.print(f"[red]  Failed to read {file}: {e}[/red]")
                failed += 1
                progress.update(task, advance=1)
                continue
            old_text = vcard.profile.raw

            if not vcard.profile.source:
                progress.console.print(
                    "[yellow]  No SOURCE property found. Skipping.[/yellow]"
                )
                skipped += 1
                progress.update(task, advance=1)
                continue

            url = vcard.profile.source
            progress.console.print(f"  Fetching: {url}")

            try:
                new_text = VCard(
                    url=url, allow_http=allow_http, verify_source=verify_source
                ).profile.raw
            except Exception as e:
                progress.console.print(f"[red]  Failed to fetch SOURCE: {e}[/red]")
                failed += 1
                progress.update(task, advance=1)
                continue

            # Show colored diff if requested
            if show_diff:
                _show_diff(progress.console, old_text, new_text, str(file), "(fetched)")

            # Determine output path
            if output_dir:
                out_path = output_dir / file.name
            else:
                out_path = file

            current_text = old_text
            if output_dir:
                current_text = (
                    out_path.read_bytes().decode("utf-8")
                    if out_path.is_file()
                    else None
                )

            if current_text is not None and current_text == new_text:
                progress.console.print(f"  Unchanged: {out_path} (already up to date)")
                unchanged += 1
                progress.update(task, advance=1)
                continue

            # Dry-run mode: do not write anything
            if dry_run:
                progress.console.print(
                    f"[cyan]  Dry-run: would update {out_path}; no changes written.[/cyan]"
                )
                updated += 1
                progress.update(task, advance=1)
                continue

            # Backup if overwriting in place
            if backup and out_path.exists() and not output_dir:
                backup_path = out_path.with_suffix(out_path.suffix + ".bak")
                backup_path.write_text(old_text, encoding="utf-8", newline="")
                progress.console.print(f"[blue]  Backup created:[/blue] {backup_path}")

            # Write updated vCard
            out_path.write_text(new_text, encoding="utf-8", newline="")
            progress.console.print(f"[green]  Updated:[/green] {out_path}")
            updated += 1

            progress.update(task, advance=1)

    typer.secho(f"Summary:")
    typer.secho(
        f"  {'Would download' if dry_run else 'Downloaded'}: {downloaded}",
        fg=typer.colors.GREEN,
    )
    typer.secho(
        f"  {'Would update' if dry_run else 'Updated'}: {updated}",
        fg=typer.colors.GREEN,
    )
    typer.secho(f"  Unchanged: {unchanged}", fg=typer.colors.YELLOW)
    typer.secho(f"  Skipped: {skipped}", fg=typer.colors.YELLOW)
    typer.secho(f"  Failed: {failed}", fg=typer.colors.RED)
    if failed:
        typer.secho(f"❌ {failed} item(s) failed.", fg=typer.colors.RED)
        raise typer.Exit(code=1)
    typer.secho("Done.")


@app.command()
def parse(
    input: str = typer.Argument(
        None, help="Path or URL of the vCard to parse, or '-' to read from stdin"
    ),
    allow_http: bool = typer.Option(
        False, "--allow-http", help="Allow plain HTTP URLs"
    ),
):
    """
    Parse a vCard (file, URL or stdin) and print its properties as JSON.
    """
    if input is None and not os.isatty(0):  # Check if stdin is not a terminal
        input = "-"

    if input is None:
        typer.secho("❌ No input data provided for parsing.", fg=typer.colors.RED)
        raise typer.Exit(code=1)

    try:
        if input == "-":
            text = sys.stdin.read().strip()
            card = VCard(text=text) if text else None
        else:
            card = _load_card(input, allow_http)
        json_string = card.to_json() if card else None
    except Exception as e:
        typer.secho(f"Failed to parse vCard: {e}", fg=typer.colors.RED)
        raise typer.Exit(code=1)

    if json_string is None:
        typer.secho("❌ No input data provided for parsing.", fg=typer.colors.RED)
        raise typer.Exit(code=1)
    print(json_string)


@app.command(help="Sign canonical endorsement strings for one or more vCard files")
def endorse(
    inputs: List[str] = typer.Argument(
        ...,
        exists=True,
        readable=True,
        help="Path to a vCard file. Pass multiple to process multiple files.",
    ),
    v_card_destination: Path = typer.Option(
        None,
        "--vcard",
        "-v",
        exists=True,
        readable=True,
        help="Path to the .vcf where the endorsement will be added",
    ),
    priv: Path = typer.Option(
        ...,
        "--priv",
        exists=True,
        readable=True,
        help="Path to the private key PEM used to sign endorsements.",
    ),
    confidence: str = typer.Option(
        "medium",
        "--confidence",
        "-c",
        help="Confidence level for the endorsement (low, medium, high). This is just metadata and does not affect the signature.",
    ),
    write: bool = typer.Option(
        False,
        "--write",
        help="Whether to write the endorsement to the vCard file. If false, the endorsement will be generated and printed but not saved.",
        is_flag=True,
    ),
):
    """
    Parse each vCard, extract its preferred key, build canonical endorsement string,
    and sign it.
    """

    valid_confidence_levels = {"low", "medium", "high"}
    if confidence not in valid_confidence_levels:
        typer.secho(
            f"❌ Invalid confidence level '{confidence}'. Must be one of: {', '.join(valid_confidence_levels)}",
            fg=typer.colors.RED,
        )
        raise typer.Exit(code=1)

    try:
        private_key = load_private_key_pem(priv.read_bytes())
    except Exception as e:
        typer.secho(
            f"Failed to load private key from '{priv}': {e}", fg=typer.colors.RED
        )
        raise typer.Exit(code=1)

    if write and v_card_destination is None:
        typer.secho(
            "❌ --write requires --vcard (the vCard where endorsements are added).",
            fg=typer.colors.RED,
        )
        raise typer.Exit(code=1)

    v_card_inputs = VCardInputs(inputs)

    for warning in v_card_inputs.warnings:

        typer.secho(warning, fg=typer.colors.RED)
    if not v_card_inputs.vcard_files:
        typer.secho("❌ No valid vCard files found.", fg=typer.colors.RED)
        raise typer.Exit(code=1)
    destination = VCard(path=v_card_destination) if write else None
    failed = 0

    for vcard_path in v_card_inputs.vcard_files:
        try:
            v_card_input = VCard(path=vcard_path)

            preferred_key = v_card_input.get_preferred_key()

            if not preferred_key:
                raise ValueError("No valid KEY entries found in the vCard")

            # Reject malformed or non-Ed25519 keys before endorsing them
            load_public_key_b64_der(preferred_key.key_b64)

            signature_endorsement = VCard.sign_endorsement(
                private_key, preferred_key.key_b64
            )

            endorsement_value = VCard.build_custom_attribute_endorsement(
                preferred_key.key_b64,
                signature_endorsement,
                confidence=confidence,
                date=datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ"),
            )
            if write:
                if destination.has_endorsement_for_key(preferred_key.key_b64):
                    typer.secho(
                        f"⚠️ Endorsement already exists for key {preferred_key.key_b64} in {v_card_destination}. Skipping write.",
                        fg=typer.colors.YELLOW,
                    )
                else:
                    destination.add_line(endorsement_value)
                    destination.to_file()
                    typer.secho(
                        f"✅ Endorsement added to {destination.path}: {endorsement_value}",
                        fg=typer.colors.GREEN,
                    )
            else:
                print(endorsement_value)

        except Exception as e:
            failed += 1
            typer.secho(
                f"Failed to endorse vCard '{vcard_path}': {e}", fg=typer.colors.RED
            )

    if failed:
        raise typer.Exit(code=1)


@app.command(
    help="Generate a QR code from the provided vCard file (support input piping) and save it to a file."
)
def qr(
    input: str = typer.Argument(
        None,
        help="vCard file to encode, or literal text to encode if it is not an existing file",
    ),
    output: str = typer.Option(
        None, "--output", "-o", help="Output file to save the QR code image"
    ),
    image: str = typer.Option(
        None, "--image", "-i", help="Path to an image to include in the QR code"
    ),
    caption_top: str = typer.Option(
        "", "--caption-top", "-t", help="Caption to display above the QR code"
    ),
    caption_bottom: str = typer.Option(
        "", "--caption-bottom", "-b", help="Caption to display below the QR code"
    ),
    font: Path = typer.Option(
        None,
        "--font",
        "-f",
        help="Path to a .ttf font file to use for captions (optional)",
    ),
):
    """
    Generate a QR code from the provided data and save it to a file.
    """
    # Check if the input is provided via standard input (piped data)

    data = None
    if input:
        if os.path.isfile(input):
            with open(input, "r", encoding="utf-8") as file:
                data = file.read().strip()
        else:
            data = input.strip()
    elif not os.isatty(0):  # Check if stdin is not a terminal
        data = sys.stdin.read().strip()

    if not data:
        typer.secho("❌ No input data provided for the QR code.", fg=typer.colors.RED)
        raise typer.Exit(code=1)

    if image and not os.path.isfile(image):
        typer.secho(
            f"❌ The specified image file does not exist: {image}", fg=typer.colors.RED
        )
        raise typer.Exit(code=1)

    if not output:
        typer.secho(
            "❌ Output file path is required to save the QR code image.",
            fg=typer.colors.RED,
        )
        raise typer.Exit(code=1)

    if caption_top or caption_bottom:
        if not font:
            typer.secho(
                "❌ A font file must be specified when using captions.",
                fg=typer.colors.RED,
            )
            raise typer.Exit(code=1)
        elif not os.path.isfile(font):
            typer.secho(
                f"❌ The specified font file does not exist: {font}",
                fg=typer.colors.RED,
            )
            raise typer.Exit(code=1)

    generate_qr(
        image=image or "",
        output=output,
        data=data,
        caption_top=caption_top,
        caption_bottom=caption_bottom,
        font=str(font) if font else "",
    )


def _load_card(source: str, allow_http: bool = False, verify_source: bool = True):
    """Load a vCard from a local path, a URL, or "-" (standard input)."""
    if source == "-":
        return VCard(text=sys.stdin.read())
    if re.match(r"^https?://", source):
        return VCard(url=source, allow_http=allow_http, verify_source=verify_source)
    return VCard(path=Path(source))


@app.command(help="Validate a DSI vCard (file, URL or '-' for stdin).")
def validate(
    source: str = typer.Argument(..., help="Path, URL or '-' to read from stdin"),
    as_json: bool = typer.Option(
        False, "--json", help="Print the result as JSON ({valid, errors, warnings})"
    ),
    strict: bool = typer.Option(
        False, "--strict", help="Treat warnings as errors (exit code 1)"
    ),
    allow_http: bool = typer.Option(
        False, "--allow-http", help="Allow plain HTTP URLs"
    ),
):
    try:
        card = _load_card(source, allow_http)
    except Exception as e:
        if as_json:
            result = ValidationResult()
            result.error("load", str(e))
            print(json.dumps(result.to_dict(), ensure_ascii=False))
        else:
            typer.secho(f"❌ Cannot load '{source}': {e}", fg=typer.colors.RED)
        raise typer.Exit(code=1)

    result = validate_profile(card.profile)
    failed = not result.valid or (strict and result.warnings)

    if as_json:
        print(json.dumps(result.to_dict(), ensure_ascii=False))
    else:
        for issue in result.errors:
            typer.secho(f"❌ [{issue.code}] {issue.message}", fg=typer.colors.RED)
        for issue in result.warnings:
            typer.secho(f"⚠️  [{issue.code}] {issue.message}", fg=typer.colors.YELLOW)
        if failed:
            typer.secho(
                f"Invalid: {len(result.errors)} error(s), {len(result.warnings)} warning(s)",
                fg=typer.colors.RED,
            )
        else:
            typer.secho(
                f"✅ Valid ({len(result.warnings)} warning(s))", fg=typer.colors.GREEN
            )
    if failed:
        raise typer.Exit(code=1)


@app.command(help="Show a read-only summary of a DSI vCard.")
def inspect(
    source: str = typer.Argument(..., help="Path, URL or '-' to read from stdin"),
    allow_http: bool = typer.Option(
        False, "--allow-http", help="Allow plain HTTP URLs"
    ),
):
    profile = _load_card(source, allow_http).profile
    revoked = {r.key_b64 for r in profile.revocations}
    preferred = next((k for k in profile.keys if k.pref == 1), None)

    typer.secho("Identity", bold=True)
    typer.echo(f"  Name:   {profile.fn or '-'}")
    typer.echo(f"  SOURCE: {profile.source or '-'}")
    typer.secho("Keys", bold=True)
    typer.echo(
        f"  Preferred: {preferred.alg or 'unknown algorithm'}"
        if preferred
        else "  Preferred: none"
    )
    typer.echo(f"  Keys: {len(profile.keys)}")
    typer.echo(f"  Revoked: {len(revoked)}")
    typer.secho("Feeds", bold=True)
    for feed in profile.feeds:
        detail = ", ".join(x for x in (feed.language, feed.tags) if x)
        typer.echo(f"  {feed.url}" + (f" ({detail})" if detail else ""))
    if not profile.feeds:
        typer.echo("  -")
    typer.secho("Social", bold=True)
    for social in profile.social:
        typer.echo(f"  {social.platform}: {social.value}")
    if not profile.social:
        typer.echo("  -")
    typer.secho("Endorsements", bold=True)
    typer.echo(f"  {len(profile.endorsements)}")


@app.command(help="Print the deterministic (normalized) form of a vCard.")
def normalize(
    source: str = typer.Argument(..., help="Path, URL or '-' to read from stdin"),
    write: bool = typer.Option(
        False, "--write", help="Overwrite the input file with the normalized form"
    ),
    allow_http: bool = typer.Option(
        False, "--allow-http", help="Allow plain HTTP URLs"
    ),
):
    if write and (source == "-" or re.match(r"^https?://", source)):
        typer.secho("❌ --write needs a local file.", fg=typer.colors.RED)
        raise typer.Exit(code=1)
    card = _load_card(source, allow_http)
    try:
        text = normalize_vcard(card.profile)
    except ValueError as e:
        typer.secho(f"❌ {e}", fg=typer.colors.RED)
        raise typer.Exit(code=1)
    if write:
        Path(source).write_text(text, encoding="utf-8", newline="")
    else:
        sys.stdout.write(text)


@app.command(help="Verify the X-ENDORSE signatures of a vCard against its own keys.")
def verify(
    source: str = typer.Argument(..., help="Path, URL or '-' to read from stdin"),
    allow_http: bool = typer.Option(
        False, "--allow-http", help="Allow plain HTTP URLs"
    ),
):
    profile = _load_card(source, allow_http).profile
    if not profile.endorsements:
        typer.echo("No endorsements.")
        return
    bad = 0
    for outcome in verify_endorsements(profile):
        key = outcome.endorsement.endorsee_key_b64
        if outcome.status == "valid":
            typer.secho(f"✅ valid    {key}", fg=typer.colors.GREEN)
        else:
            bad += outcome.status == "invalid"
            color = (
                typer.colors.RED if outcome.status == "invalid" else typer.colors.YELLOW
            )
            typer.secho(f"{outcome.status:<9} {key} ({outcome.reason})", fg=color)
    if bad:
        raise typer.Exit(code=1)


if __name__ == "__main__":
    app()
