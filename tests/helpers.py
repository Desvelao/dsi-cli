"""Shared builders for tests: deterministic Ed25519 keys and DSI vCards."""

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import ed25519

from src.dsipy.crypto.keys import public_key_to_b64der


def make_key(name: str):
    """Deterministic key: the seed is the name padded with zero bytes."""
    private = ed25519.Ed25519PrivateKey.from_private_bytes(
        name.encode().ljust(32, b"\0")
    )
    return private, public_key_to_b64der(private.public_key())


def private_pem(private) -> bytes:
    return private.private_bytes(
        serialization.Encoding.PEM,
        serialization.PrivateFormat.PKCS8,
        serialization.NoEncryption(),
    )


def crlf(*lines: str) -> str:
    return "\r\n".join(lines) + "\r\n"


def dsi_vcard(*extra: str, source="https://alice.example/dsi.vcf", keys=True) -> str:
    _, alice = make_key("alice")
    lines = ["BEGIN:VCARD", "VERSION:4.0", "FN:Alice Example", f"SOURCE:{source}"]
    if keys:
        lines.append(f"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:{alice}")
    lines.extend(extra)
    lines.append("END:VCARD")
    return crlf(*lines)
