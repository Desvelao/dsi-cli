import datetime
import os
from pathlib import Path
from rich.progress import Progress
import typer
from typing import List
from ..feeds.rss import RSSFeed
from ..core.identity import VCard
from ..feeds.markdown import get_feed_class
from ..feeds.signing import verify_feed_items
from .common import Cli, show_diff as _show_diff
from ..publishing import PublishConflictError, get_publisher
from ..crypto.keys import (
    load_private_key_pem,
    load_public_key_b64_der,
    load_public_key_pem,
    public_key_to_b64der,
)
from ..core.utils import slugify


def get_option_value(
    option_name: str,
    current_value: str | None,
    prompt_message: str,
    default_value: str,
    interactive: bool,
) -> str:
    """
    Get the value for an option, either from the current value or by prompting the user.
    """
    if not current_value:
        if interactive:
            return typer.prompt(
                prompt_message, default=default_value, show_default=True
            )
        else:
            typer.secho(
                f"❌ The {option_name} cannot be empty. Please provide a {option_name} using the --{option_name} option.",
                fg=typer.colors.RED,
            )
            raise typer.Exit(code=1)
    return current_value


app = Cli(help="Commands related to feeds processing", no_args_is_help=True)


@app.command(help="Create a new feed file with an initial item")
def add(
    title: str = typer.Option("", "--title", "-t", help="Title for the new feed"),
    message: str = typer.Option(
        None, "--message", "-m", help="Content for the new feed item"
    ),
    filename: str = typer.Option(
        None, "--filename", "-f", help="Filename for the new feed"
    ),
    interactive: bool = typer.Option(
        False, "--interactive", "-i", help="Interactive prompts", is_flag=True
    ),
    feed_type: str = typer.Option(
        "markdown", "--type", help="Define the type of feed to create"
    ),
):

    feed_class = get_feed_class(feed_type)

    now = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    filename = get_option_value(
        "filename",
        filename,
        "Provide the filename for the new feed",
        f"feeds/{slugify(now)}.md",
        interactive,
    )

    feed_path = os.path.join(f"{filename}")

    if os.path.exists(feed_path):
        typer.secho(
            f"❌ A feed file with the name '{filename}' already exists. Please choose a different name.",
            fg=typer.colors.RED,
        )
        raise typer.Exit(code=1)

    if not feed_path.lower().endswith(".md"):
        typer.secho(
            f"⚠️  '{filename}' does not end in .md: `dsipy feeds build` only reads .md files.",
            fg=typer.colors.YELLOW,
        )

    title = get_option_value(
        "title",
        title,
        "Provide the title for the new feed",
        "My New Feed",
        interactive,
    )
    message = get_option_value(
        "message",
        message,
        "Provide the message for the new feed",
        "This is the content of my new feed item.",
        interactive,
    )

    parent = os.path.dirname(feed_path)
    if parent:
        os.makedirs(parent, exist_ok=True)

    feed_class.create_state(feed_path, title, message, now)

    typer.secho(f"ℹ️  Edit the file with a text editor: {feed_path}")


@app.command(
    help="Initialize the feeds source directory (creates it and a sample post)."
)
def init(
    directory: Path = typer.Argument(
        Path("feeds"), help="Directory that will hold the feed sources"
    ),
    sample: bool = typer.Option(
        True,
        "--sample/--no-sample",
        help="Create a sample post (hello.md) when it does not exist",
    ),
    feed_type: str = typer.Option(
        "markdown", "--type", help="Define the type of feed to create"
    ),
):
    feed_class = get_feed_class(feed_type)

    if directory.exists() and not directory.is_dir():
        typer.secho(
            f"❌ '{directory}' exists and is not a directory.", fg=typer.colors.RED
        )
        raise typer.Exit(code=1)

    already_existed = directory.is_dir()
    directory.mkdir(parents=True, exist_ok=True)
    typer.secho(
        f"ℹ️  Directory {'already exists' if already_existed else 'created'}: {directory}"
    )

    sample_path = directory / "hello.md"
    if sample and not sample_path.exists():
        now = datetime.datetime.now(datetime.timezone.utc).strftime(
            "%Y-%m-%dT%H:%M:%SZ"
        )
        feed_class.create_state(
            str(sample_path),
            "Hello DSI",
            "This is my first post. Edit or delete this file.",
            now,
        )
    elif not sample and not any(directory.iterdir()):
        # keep the empty directory committable in git
        (directory / ".gitkeep").touch()
        typer.secho(f"✅ Created {directory / '.gitkeep'}", fg=typer.colors.GREEN)

    typer.echo(
        "Next: add posts with `dsipy feeds add --filename "
        f"{directory}/<name>.md` and build with `dsipy feeds build {directory}`."
    )


def _load_signing(signing_key_priv_file, signing_key_public_file):
    """Return the {"key", "id"} signing dict, or None unless both keys are given."""
    sign = None

    if bool(signing_key_priv_file) != bool(signing_key_public_file):
        typer.secho(
            "❌ Signing needs both --sign-priv and --sign-pub; only one was given.",
            fg=typer.colors.RED,
        )
        raise typer.Exit(code=1)

    if signing_key_priv_file and signing_key_public_file:
        priv_key_data = None
        pub_key_data = None

        # A value is either a key file or the PEM text itself (handy for CI secrets).
        def read_key(value, option):
            if os.path.isfile(value):
                return Path(value).read_bytes()
            if "-----BEGIN" in value:
                return value.encode()
            typer.secho(
                f"❌ Signing key file not found for {option}: {value}",
                fg=typer.colors.RED,
            )
            raise typer.Exit(code=1)

        priv_key_data = read_key(signing_key_priv_file, "--sign-priv")
        pub_key_data = read_key(signing_key_public_file, "--sign-pub")

        if priv_key_data and pub_key_data:
            sign = {
                "key": load_private_key_pem(priv_key_data),
                "id": public_key_to_b64der(load_public_key_pem(pub_key_data)),
            }
    return sign


def _parse_template_vars(var_file, var) -> dict:
    """Merge --var-file and --var template variables (--var wins)."""
    vars_dict = {}
    if var_file:
        if var_file.exists():
            for line in var_file.read_text(encoding="utf-8").splitlines():
                line = line.strip()
                if line and "=" in line and not line.startswith("#"):
                    key, value = line.split("=", 1)
                    vars_dict[key.strip()] = value.strip()
        else:
            typer.secho(
                f"❌ Variable file not found: {var_file}",
                fg=typer.colors.RED,
            )
            raise typer.Exit(code=1)

    if var:
        for arg in var:
            if "=" not in arg:
                raise typer.BadParameter(
                    f"Invalid --var '{arg}': expected key=value", param_hint="--var"
                )
            key, value = arg.split("=", 1)
            vars_dict[key] = value
    return vars_dict


def _apply_templates(states, vars_dict) -> None:
    """Replace template variables ({{ title }}, {{ date }}, ...) in each state."""
    for state in states:
        for field in ("title", "id", "link", "image", "content"):
            if state.get(field):
                state[field] = RSSFeed.replace_template_variables(
                    state[field], state["metadata"], vars_dict
                )
        # Allow HTML in RSS description
        if state.get("content") and state.get("content_type") == "html":
            state["content"] = f"<![CDATA[{state['content']}]]>"


@app.command(help="Build an RSS feed from a folder of Markdown posts")
def build(
    directory: str = typer.Argument(
        ..., help="Directory where the feed files are located"
    ),
    output: Path = typer.Option("feed.rss", "--output", "-o", help="Output RSS file"),
    limit: int | None = typer.Option(
        None,
        "--limit",
        "-l",
        min=0,
        help="Limit number of states to include in the feed (0 = empty feed)",
    ),
    title: str = typer.Option(None, "--title", "-t", help="RSS feed title"),
    link: str = typer.Option(None, "--link", "-k", help="Base link for items"),
    description: str = typer.Option(
        None, "--description", "-d", help="RSS feed description"
    ),
    language: str = typer.Option(
        "en-US", "--language", "-g", help="Language of the feed"
    ),
    author: str = typer.Option(None, "--author", "-a", help="Author information"),
    email: str = typer.Option(None, "--email", "-e", help="Email of the author"),
    feed_type: str = typer.Option(
        "markdown", "--type", help="Define the type of states"
    ),
    interactive: bool = typer.Option(
        False, "--interactive", "-i", help="Interactive prompts", is_flag=True
    ),
    signing_key_priv_file: str | None = typer.Option(
        None, "--sign-priv", help="Private key file to sign RSS items"
    ),
    signing_key_public_file: str | None = typer.Option(
        None, "--sign-pub", help="Public key file to verify RSS item signatures"
    ),
    var: List[str] = typer.Option(
        None, "--var", help="Template variables as key=value pairs"
    ),
    var_file: Path = typer.Option(
        None, "--var-file", help="File with template variables (one KEY=VALUE per line)"
    ),
):

    feed_class = get_feed_class(feed_type)

    title = get_option_value(
        "title",
        title,
        "Provide the title for the RSS feed",
        "My RSS Feed",
        interactive,
    )
    link = get_option_value(
        "link",
        link,
        "Provide the base link for the RSS feed items",
        "https://example.com",
        interactive,
    )
    description = get_option_value(
        "description",
        description,
        "Provide the description for the RSS feed",
        "This is my RSS feed",
        interactive,
    )
    author = get_option_value(
        "author",
        author,
        "Provide the author name for the RSS feed",
        "Author Name",
        interactive,
    )
    email = get_option_value(
        "email",
        email,
        "Provide the author email for the RSS feed",
        "author@email.com",
        interactive,
    )

    sign = _load_signing(signing_key_priv_file, signing_key_public_file)
    vars_dict = _parse_template_vars(var_file, var)

    if not Path(directory).exists():
        typer.secho(f"❌ Directory not found: {directory}", fg=typer.colors.RED)
        raise typer.Exit(code=1)

    states = feed_class.collect(directory)

    if limit is not None:
        states = states[:limit]

    _apply_templates(states, vars_dict)

    build_date = datetime.datetime.now()
    # TODO: consider adding more feed formats (e.g. JSON Feed) and allowing users to choose the output format
    feed_content = RSSFeed.build(
        title, link, description, author, email, language, build_date, states, sign
    )

    if output:
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(feed_content, encoding="utf-8")
        typer.secho(f"✅ RSS feed generated: {output}", fg=typer.colors.GREEN)
    else:
        print(feed_content)


FEED_EXTENSIONS = {".xml", ".rss", ".json"}


def iter_feed_files(paths: List[Path]):
    """Yield (file, relative_posix_path) for every feed file under `paths`.

    Files inside a directory are named relative to that directory; a file given
    directly is named by its file name.
    """
    for p in paths:
        if p.is_file() and p.suffix.lower() in FEED_EXTENSIONS:
            yield p, p.name
        elif p.is_dir():
            for file in sorted(p.rglob("*")):
                if file.is_file() and file.suffix.lower() in FEED_EXTENSIONS:
                    yield file, file.relative_to(p).as_posix()


def _parse_provider_args(provider_args) -> dict:
    """Parse repeated --arg key=value options into a dict."""
    kwargs = {}
    if provider_args:
        for arg in provider_args:
            if "=" not in arg:
                raise typer.BadParameter(
                    f"Invalid --arg '{arg}': expected key=value", param_hint="--arg"
                )
            key, value = arg.split("=", 1)
            kwargs[key] = value
    return kwargs


@app.command("publish")
def publish(
    inputs: List[Path] = typer.Argument(...),
    provider: str = typer.Option(
        ..., "--provider", help="Provider: github, s3"
    ),
    prefix: str = typer.Option("", "--prefix", help="Path prefix on provider"),
    dry_run: bool = typer.Option(
        False,
        "--dry-run",
        help="Read the remote copies and report what would change, but write "
        "nothing (still needs working provider credentials)",
    ),
    show_diff: bool = typer.Option(
        False, "--diff", help="Show a diff between the remote and local file"
    ),
    provider_args: List[str] = typer.Option(
        None,
        "--arg",
        help="Provider-specific arguments: key=value",
    ),
):
    """
    Publish feeds to a provider (GitHub Pages, S3). Exits 1 if any file fails.
    """

    kwargs = _parse_provider_args(provider_args)

    prov = get_publisher(provider, **kwargs)

    if prefix and not prefix.endswith("/"):
        prefix += "/"

    feed_files = list(iter_feed_files(inputs))
    if not feed_files:
        typer.secho(
            f"❌ No feed files found in the specified paths: {inputs}",
            fg=typer.colors.RED,
        )
        raise typer.Exit(1)

    published = 0
    would_publish = 0
    unchanged = 0
    failed = 0

    with Progress() as progress:
        task = progress.add_task("Publishing feeds...", total=len(feed_files))

        for file, rel in feed_files:
            rel_path = f"{prefix}{rel}"
            progress.console.print(f"[bold]Publishing:[/bold] {file} → {rel_path}")

            try:
                local_text = file.read_bytes().decode("utf-8")
            except (OSError, UnicodeDecodeError) as e:
                progress.console.print(f"[red]  Failed to read {file}: {e}[/red]")
                failed += 1
                progress.update(task, advance=1)
                continue

            try:
                remote_text, version = prov.get_remote(rel_path)
            except Exception as e:
                progress.console.print(f"[red]  Failed to fetch remote: {e}[/red]")
                failed += 1
                progress.update(task, advance=1)
                continue

            # Diff
            if show_diff and remote_text is not None:
                _show_diff(
                    progress.console, remote_text, local_text, "(remote)", str(file)
                )

            # Skip if unchanged
            if remote_text == local_text:
                progress.console.print("[green]  Unchanged.[/green]")
                unchanged += 1
                progress.update(task, advance=1)
                continue

            if dry_run:
                progress.console.print(
                    "[cyan]  Dry-run: would publish, no changes written.[/cyan]"
                )
                would_publish += 1
                progress.update(task, advance=1)
                continue

            try:
                prov.publish(rel_path, local_text, version)
                progress.console.print(f"[green]  Published:[/green] {rel_path}")
                published += 1
            except PublishConflictError as e:
                progress.console.print(
                    f"[red]  Conflict (remote changed since it was read): {e}[/red]"
                )
                failed += 1
            except Exception as e:
                progress.console.print(f"[red]  Failed to publish: {e}[/red]")
                failed += 1

            progress.update(task, advance=1)

    typer.secho("Summary:", bold=True)
    if dry_run:
        typer.secho(f"  Would publish: {would_publish}", fg=typer.colors.GREEN)
    else:
        typer.secho(f"  Published:  {published}", fg=typer.colors.GREEN)
    typer.secho(f"  Unchanged:   {unchanged}", fg=typer.colors.CYAN)
    typer.secho(f"  Failed:      {failed}", fg=typer.colors.RED)
    if failed:
        typer.secho(f"❌ {failed} file(s) failed to publish.", fg=typer.colors.RED)
        raise typer.Exit(1)
    typer.secho("Done.")


@app.command("verify", help="Verify the signed items of an RSS file")
def verify(
    feed: Path = typer.Argument(..., exists=True, readable=True, help="RSS file"),
    vcard: Path = typer.Option(
        None,
        "--vcard",
        "-v",
        help="vCard whose KEY properties may have signed the feed",
    ),
    pub: Path = typer.Option(
        None, "--pub", help="Public key PEM that signed the feed (alternative)"
    ),
):
    keys = {}
    if vcard:
        for key in VCard(path=vcard).profile.keys:
            try:
                keys[key.key_b64] = load_public_key_b64_der(key.key_b64)
            except ValueError:
                continue
    if pub:
        public_key = load_public_key_pem(pub.read_bytes())
        keys[public_key_to_b64der(public_key)] = public_key
    if not keys:
        typer.secho(
            "❌ Pass --vcard or --pub to provide the verification keys.",
            fg=typer.colors.RED,
        )
        raise typer.Exit(code=1)

    results = verify_feed_items(feed.read_text(encoding="utf-8"), keys)
    invalid = 0
    for item in results:
        label = item.title or item.guid or "(untitled)"
        if item.status == "valid":
            typer.secho(f"✅ valid    {label}", fg=typer.colors.GREEN)
        elif item.status == "unsigned":
            typer.secho(f"⚠️ unsigned {label}", fg=typer.colors.YELLOW)
        else:
            invalid += 1
            typer.secho(f"❌ invalid  {label} ({item.reason})", fg=typer.colors.RED)
    typer.echo(f"{len(results)} item(s), {invalid} invalid")
    if invalid:
        raise typer.Exit(code=1)


if __name__ == "__main__":
    app()
