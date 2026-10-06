from email.message import Message
import re
from pathlib import Path, PurePosixPath
from typing import Optional
from urllib.parse import urlsplit, urlunsplit

from ..vcard.parser import parse_vcard
from .files import file_is_vcard
from .http import FetchError, fetch_text

DEFAULT_PORTS = {"http": 80, "https": 443}
UNRESERVED = frozenset(
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
)
PERCENT_ESCAPE = re.compile(r"%([0-9A-Fa-f]{2})")


def _normalize_percent_encoding(component: str) -> str:
    """Uppercase percent-escapes and decode those of unreserved characters."""

    def replace(match: re.Match) -> str:
        char = chr(int(match.group(1), 16))
        return char if char in UNRESERVED else f"%{match.group(1).upper()}"

    return PERCENT_ESCAPE.sub(replace, component)


def _remove_dot_segments(path: str) -> str:
    """Remove "." and ".." segments from a path (RFC 3986 §5.2.4)."""
    output: list[str] = []
    segments = path.split("/")
    for index, segment in enumerate(segments):
        last = index == len(segments) - 1
        if segment == ".":
            if last:
                output.append("")
        elif segment == "..":
            if len(output) > 1:
                output.pop()
            if last:
                output.append("")
        else:
            output.append(segment)
    return "/".join(output)


class SourceMismatchError(ValueError):
    """The SOURCE of a fetched vCard does not match the URL it was fetched from."""


def normalize_url(url: str) -> str:
    """Normalize a URL for SOURCE comparison (RFC §5.1).

    Lowercase scheme and host, drop the default port and a trailing dot in the
    host, use "/" for an empty path, normalize percent-encoding and dot
    segments (RFC 3986 §6.2) and drop the fragment.

    Raises:
        ValueError: if the URL has an invalid port.
    """
    parts = urlsplit(url.strip())
    scheme = parts.scheme.lower()
    host = (parts.hostname or "").lower().rstrip(".")
    if ":" in host:  # IPv6 literal
        host = f"[{host}]"
    port = parts.port
    netloc = host
    if port and port != DEFAULT_PORTS.get(scheme):
        netloc = f"{host}:{port}"
    path = _remove_dot_segments(_normalize_percent_encoding(parts.path))
    query = _normalize_percent_encoding(parts.query)
    return urlunsplit((scheme, netloc, path or "/", query, ""))


def source_matches(requested_url: str, source: Optional[str]) -> bool:
    """True if the vCard SOURCE equals the requested URL after normalization."""
    if not source:
        return False
    try:
        return normalize_url(requested_url) == normalize_url(source)
    except ValueError:  # invalid port
        return False


def safe_filename(content_disposition: str, url: str) -> str:
    """Pick a file name that cannot escape the output directory."""
    candidates = []
    if content_disposition:
        message = Message()
        message["content-disposition"] = content_disposition
        candidates.append(message.get_filename() or "")
    candidates.append(PurePosixPath(urlsplit(url).path).name)
    for candidate in candidates:
        name = PurePosixPath(candidate.replace("\\", "/")).name
        if name and name not in (".", "..") and not name.startswith("."):
            return name
    return "vcard.vcf"


def fetch_vcard_from_url(
    url: str, *, allow_http: bool = False, verify_source: bool = True
) -> tuple[str, str]:
    """Fetch a vCard and return (text, filename).

    Raises:
        FetchError: if the resource cannot be fetched safely or is not a vCard.
        SourceMismatchError: if verify_source is set and the returned SOURCE
            does not match the requested URL.
    """
    response = fetch_text(url, allow_http=allow_http)
    text = response.text
    if not text.strip().startswith("BEGIN:VCARD"):
        raise FetchError(f"URL does not contain a valid vCard: {url}")

    if verify_source:
        source = parse_vcard(text).source
        if not source_matches(url, source):
            raise SourceMismatchError(
                f"SOURCE mismatch for {url}: the vCard declares "
                f"{source or 'no SOURCE'}"
            )

    filename = safe_filename(response.headers.get("Content-Disposition", ""), url)
    return text, filename


def fetch_save_vcard_from_url(
    url, output_dir: Path = None, overwrite: bool = False, **fetch_options
) -> tuple[Path, str]:
    """
    Fetch a vCard from a URL and save it to the specified output directory.

    Args:
        url (str): The URL to fetch the vCard from.
        output_dir (Path, optional): The directory to save the fetched vCard. Defaults to None.
        overwrite (bool): Replace the destination if it already exists. Defaults to False.
        **fetch_options: Passed to fetch_vcard_from_url (allow_http, verify_source).

    Returns:
        tuple: A tuple containing the destination Path and the vCard content as text.

    Raises:
        FileExistsError: if the destination exists and overwrite is not set.
    """

    text, filename = fetch_vcard_from_url(url, **fetch_options)

    if output_dir:
        output_dir.mkdir(parents=True, exist_ok=True)
    filename = filename if file_is_vcard(filename) else f"{filename}.vcf"
    destination = output_dir / filename if output_dir else Path(filename)
    try:
        with open(
            destination, "w" if overwrite else "x", encoding="utf-8", newline=""
        ) as f:
            f.write(text)
    except FileExistsError:
        raise FileExistsError(
            f"{destination} already exists; pass overwrite=True to replace it"
        ) from None
    return destination, text
