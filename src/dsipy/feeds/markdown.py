import os
import datetime
import markdown
from pathlib import Path
from ..core.utils import slugify
import typer


def get_feed_class(format):
    if format == "markdown":
        return MarkdownFeed
    else:
        raise ValueError(f"Unsupported feed format: {format}")


class FeedFileError(ValueError):
    """A markdown feed file is malformed; the message names the file."""


def _unquote(value: str) -> str:
    """Strip one pair of matching surrounding single or double quotes."""
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        return value[1:-1]
    return value


def _parse_date(value: str) -> datetime.datetime:
    """
    Parse an ISO 8601 date/datetime (date only, offsets, fractional seconds,
    space separator, trailing Z). Naive values are taken as UTC.
    Returns a naive datetime expressed in UTC (the form the RSS builder expects).
    Raises ValueError if the value is not a valid ISO date.
    """
    parsed = datetime.datetime.fromisoformat(value.strip())
    if parsed.tzinfo is not None:
        parsed = parsed.astimezone(datetime.timezone.utc).replace(tzinfo=None)
    return parsed


class MarkdownFeed:
    def _parse_frontmatter(lines: list) -> tuple[dict, list]:
        """
        Extract frontmatter and content lines from markdown file lines.
        Parses YAML-like front matter delimited by --- markers.
        Supports any attributes in the format "key: value".
        Args:
            lines (list): List of lines from the markdown file.
        Returns:
            tuple: (frontmatter_dict, content_lines_list)
        """
        frontmatter = {}
        content_lines = []

        # Detect front matter
        if lines and lines[0].strip() == "---":
            i = 1
            while i < len(lines) and lines[i].strip() != "---":
                line = lines[i].strip()
                # Parse any "key: value" format
                if ":" in line:
                    key, value = line.split(":", 1)
                    frontmatter[key.strip()] = _unquote(value.strip())
                i += 1
            if i >= len(lines):
                raise ValueError("unterminated front matter (missing closing '---')")
            content_lines = lines[i + 1 :]
        else:
            content_lines = lines

        return frontmatter, content_lines

    def _parse_file(path: Path, root: Path | None = None) -> dict:
        """
        Extract metadata and content from a markdown file.
        Supports simple YAML-like front matter:
        ---
        title: My Post
        date: 2025-01-01
        link: https://example.com/my-post
        image: https://example.com/image.jpg
        use_html_content: true
        other_metadata_key: value
        ---

        This is the body of the state.

        In the front matter, the canonical attributes are:
            - title: The title of the feed item.
            - date: The publication date of the feed item in ISO format (e.g., YYYY-MM-DD or YYYY-MM-DDTHH:MM:SSZ).
            - link: An optional URL associated with the feed item.
            - image: An optional URL to an image associated with the feed item.

        title and date are required for feed generation, but if they are missing, we will use fallbacks:
            - title: the filename without extension
            - date: the file's last modified time

        You can include any additional metadata as key-value pairs in the frontmatter, and they will be included in the output dictionary under the "metadata" key. This allows for extensibility and custom metadata fields as needed.

        The `use_html_content` attribute in the frontmatter can be set to "true" or "yes" to indicate that the content should be rendered as HTML. The raw content will be processed with the markdown library to convert it to HTML. If `use_html_content` is not set or is false, the content will be treated as plain text.

        The item id defaults to the slugified path of the file relative to `root`
        (the feed directory), without extension, so it does not depend on the
        current directory or on where the feed directory lives. Without `root`,
        the file stem is used. The `id` front matter key overrides it.

        Args:
            path (Path): Path to the markdown file.
            root (Path | None): Feed root directory used to derive the default id.
        Returns:
            dict: A dictionary containing the extracted metadata and content.
        """

        lines = path.read_text(encoding="utf-8").splitlines()
        try:
            metadata, content_lines = MarkdownFeed._parse_frontmatter(lines)
        except ValueError as e:
            raise FeedFileError(f"{path}: {e}") from e

        # Fallback title
        if not metadata.get("title"):
            metadata["title"] = os.path.splitext(os.path.basename(path))[0]

        # Fallback date (file modified time)
        if not metadata.get("date"):
            ts = os.path.getmtime(path)
            metadata["date"] = datetime.datetime.fromtimestamp(
                ts, tz=datetime.timezone.utc
            ).strftime("%Y-%m-%dT%H:%M:%SZ")
        try:
            date = _parse_date(metadata["date"])
        except ValueError:
            raise FeedFileError(
                f"{path}: invalid date '{metadata['date']}' "
                "(expected ISO 8601, e.g. 2025-01-31 or 2025-01-31T12:00:00Z)"
            ) from None

        metadata["file_path"] = str(path)
        metadata["file_name"] = os.path.basename(path)
        metadata["file_dir"] = os.path.dirname(path)
        metadata["file_ext"] = os.path.splitext(path)[1]

        raw_content = "\n".join(content_lines)
        content = raw_content

        use_html_content = metadata.get("use_html_content", "false").lower() in [
            "true",
            "yes",
        ]
        if use_html_content:
            content = markdown.markdown(raw_content, extensions=["extra"])

        rel = Path(path).relative_to(root) if root else Path(Path(path).name)
        default_id = slugify(rel.with_suffix("").as_posix())

        return {
            "id": metadata.get("id") or default_id,
            "title": metadata["title"],
            "date": date,
            "link": metadata.get("link", None),
            "image": metadata.get("image", None),
            "content": content,
            "content_type": "html" if use_html_content else "text",
            "metadata": metadata,
        }

    def _create_state_content(title: str, message: str, date: str | None = None):
        """
        Create the content for a new markdown feed item, including front matter.
        The front matter includes the title and date, and is formatted as YAML-like metadata.
        Args:
            title (str): The title of the feed item.
            message (str): The content/message of the feed item.
            date (str | None): The date of the feed item in ISO format (optional).
        Returns:
            str: The complete content for the markdown file, including front matter and message.
        """
        attributes = {"title": title if title else None, "date": date}

        # Only include attributes that have a value (non-empty)
        front_matter = "\n".join(
            f"{key}: {value}" for key, value in attributes.items() if value
        )

        return f"""---
{front_matter}
---
{message}
"""

    def create_state(
        destination: str, title: str, message: str, date: str | None = None
    ):
        content = MarkdownFeed._create_state_content(title, message, date)
        if os.path.exists(destination):
            typer.secho(
                f"❌ A feed '{destination}' already exists.", fg=typer.colors.RED
            )
            raise typer.Exit(code=1)
        with open(destination, "w", encoding="utf-8") as f:
            f.write(content)
        typer.secho(f"✅ New post created: {destination}", fg=typer.colors.GREEN)

    def collect(directory: str):
        base = Path(directory)
        if base.is_file():
            md_files, root = [base], base.parent
        else:
            root = base
            md_files = [
                Path(dirpath) / f
                for dirpath, _, files in os.walk(directory)
                for f in files
                if f.endswith(".md")
            ]

        states = [MarkdownFeed._parse_file(f, root) for f in md_files]
        states.sort(key=lambda x: x["date"], reverse=True)

        return states
