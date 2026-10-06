"""Hardened HTTP fetching for remote DSI resources.

Protections: HTTPS by default, redirect limit (each hop re-validated),
response-size cap on the decoded body, connect/read timeouts, content-type
filtering and refusal of non-public addresses (SSRF).

The host is resolved once and checked; the connection is then pinned to that
validated IP (TLS SNI and certificate checks still use the original hostname and
the Host header is preserved), so DNS rebinding cannot swap the address.
Proxy environment variables (HTTP_PROXY, HTTPS_PROXY, ...) and netrc are ignored
(trust_env=False): a local DNS check says nothing about where a proxy connects.
"""

from dataclasses import dataclass
import ipaddress
import socket
from typing import Iterable, Optional
from urllib.parse import urljoin, urlsplit, urlunsplit

import requests
from requests.adapters import HTTPAdapter

DEFAULT_MAX_BYTES = 1_000_000
DEFAULT_TIMEOUT = (5, 10)  # (connect, read) seconds
DEFAULT_MAX_REDIRECTS = 5
DEFAULT_ACCEPTED_TYPES = ("text/", "application/octet-stream", "application/vcard")
REDIRECT_STATUSES = {301, 302, 303, 307, 308}


class FetchError(ValueError):
    """Raised when a remote resource cannot be fetched safely."""


@dataclass
class FetchedResponse:
    text: str
    url: str  # final URL after redirects
    headers: dict


def _resolve_addresses(host: str, port: int) -> list[str]:
    try:
        infos = socket.getaddrinfo(host, port, type=socket.SOCK_STREAM)
    except socket.gaierror as e:
        raise FetchError(f"Cannot resolve host '{host}': {e}") from e
    return [info[4][0] for info in infos]


def assert_public_host(host: str, port: int) -> list[str]:
    """Raise FetchError unless every address of the host is globally routable.

    Returns the validated addresses."""
    addresses = _resolve_addresses(host, port)
    if not addresses:
        raise FetchError(f"Host '{host}' did not resolve to any address")
    for address in addresses:
        ip = ipaddress.ip_address(address.split("%")[0])
        if not ip.is_global:
            raise FetchError(
                f"Refusing to fetch '{host}': it resolves to a non-public address ({ip})"
            )
    return addresses


class _PinnedAdapter(HTTPAdapter):
    """Connect to a fixed IP while keeping the original hostname for Host/SNI/TLS."""

    def __init__(self, host: str, port: int, ip: str, **kwargs):
        self._host, self._port, self._ip = host, port, ip
        super().__init__(**kwargs)

    def init_poolmanager(self, connections, maxsize, block=False, **pool_kwargs):
        # Verify the certificate against the hostname, not the IP in the URL.
        pool_kwargs.update(server_hostname=self._host, assert_hostname=self._host)
        super().init_poolmanager(connections, maxsize, block, **pool_kwargs)

    def send(self, request, **kwargs):
        parts = urlsplit(request.url)
        ip = f"[{self._ip}]" if ":" in self._ip else self._ip
        netloc = f"{ip}:{parts.port}" if parts.port else ip
        default_port = 443 if parts.scheme == "https" else 80
        host_header = (
            self._host if self._port == default_port else f"{self._host}:{self._port}"
        )
        request.url = urlunsplit(parts._replace(netloc=netloc))
        request.headers["Host"] = host_header
        return super().send(request, **kwargs)


def _pinned_session(host: str, port: int, ip: str) -> requests.Session:
    session = requests.Session()
    session.trust_env = False  # ignore proxy env vars and netrc
    adapter = _PinnedAdapter(host, port, ip)
    session.mount("https://", adapter)
    session.mount("http://", adapter)
    return session


def _validate_url(url: str, allow_http: bool) -> tuple[str, int]:
    parts = urlsplit(url)
    allowed = ("https", "http") if allow_http else ("https",)
    if parts.scheme not in allowed:
        raise FetchError(
            f"Unsupported URL scheme '{parts.scheme}' in {url} "
            f"(allowed: {', '.join(allowed)})"
        )
    if not parts.hostname:
        raise FetchError(f"URL has no host: {url}")
    if parts.username or parts.password:
        raise FetchError("URLs with embedded credentials are not allowed")
    port = parts.port or (443 if parts.scheme == "https" else 80)
    return parts.hostname, port


def _read_limited(response: requests.Response, max_bytes: int) -> bytes:
    declared = response.headers.get("Content-Length")
    if declared and declared.isdigit() and int(declared) > max_bytes:
        raise FetchError(f"Response too large ({declared} bytes, limit {max_bytes})")
    body = bytearray()
    for chunk in response.iter_content(chunk_size=8192):
        body.extend(chunk)
        if len(body) > max_bytes:
            raise FetchError(f"Response exceeds the {max_bytes} byte limit")
    return bytes(body)


def fetch_text(
    url: str,
    *,
    allow_http: bool = False,
    max_bytes: int = DEFAULT_MAX_BYTES,
    timeout: tuple = DEFAULT_TIMEOUT,
    max_redirects: int = DEFAULT_MAX_REDIRECTS,
    accepted_types: Optional[Iterable[str]] = DEFAULT_ACCEPTED_TYPES,
) -> FetchedResponse:
    """Fetch a text resource following the safety rules in this module."""
    current = url
    for _ in range(max_redirects + 1):
        host, port = _validate_url(current, allow_http)
        ip = assert_public_host(host, port)[0]

        session = _pinned_session(host, port, ip)
        try:
            response = session.get(
                current, timeout=timeout, allow_redirects=False, stream=True
            )
        except requests.RequestException as e:
            session.close()
            raise FetchError(f"Failed to fetch {current}: {e}") from e

        try:
            if response.status_code in REDIRECT_STATUSES:
                location = response.headers.get("Location")
                if not location:
                    raise FetchError(f"Redirect without Location from {current}")
                current = urljoin(current, location)
                continue

            if not response.ok:
                raise FetchError(f"HTTP {response.status_code} fetching {current}")

            content_type = (
                response.headers.get("Content-Type", "").split(";")[0].strip().lower()
            )
            if (
                accepted_types
                and content_type
                and not content_type.startswith(tuple(accepted_types))
            ):
                raise FetchError(f"Unexpected content type '{content_type}'")

            body = _read_limited(response, max_bytes)
            try:
                text = body.decode("utf-8")
            except UnicodeDecodeError as e:
                raise FetchError(f"Response is not valid UTF-8: {e}") from e
            return FetchedResponse(
                text=text, url=current, headers=dict(response.headers)
            )
        finally:
            response.close()
            session.close()

    raise FetchError(f"Too many redirects (limit {max_redirects}) fetching {url}")
