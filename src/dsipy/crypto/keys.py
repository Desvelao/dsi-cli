import base64
import binascii
import os
from cryptography.hazmat.primitives.asymmetric import ed25519
from cryptography.hazmat.primitives import serialization
from pathlib import Path

# ============================================================
# Key generation
# ============================================================


def generate_keypair() -> tuple[bytes, bytes, str]:
    """
    Generates an Ed25519 private/public keypair.
    Returns (private_key_pem, public_key_pem, public_key_b64_der)
    """
    private_key = ed25519.Ed25519PrivateKey.generate()
    public_key = private_key.public_key()

    # PEM export
    private_pem = private_key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )

    public_pem = public_key.public_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    )

    # DER → base64 (for vCard)
    public_der = public_key.public_bytes(
        encoding=serialization.Encoding.DER,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    )
    public_b64 = base64.b64encode(public_der).decode("ascii")

    return private_pem, public_pem, public_b64


def write_private_key(path: Path, pem: bytes, force: bool = False) -> None:
    """Writes a private key PEM, created with mode 0600 from the start.

    Raises FileExistsError if the file exists and force is False.
    """
    flags = os.O_WRONLY | os.O_CREAT | (os.O_TRUNC if force else os.O_EXCL)
    fd = os.open(path, flags, 0o600)
    try:
        os.fchmod(fd, 0o600)  # also tightens a pre-existing file when forced
        with os.fdopen(fd, "wb") as f:
            fd = -1
            f.write(pem)
    finally:
        if fd != -1:
            os.close(fd)


def action_generate_keypair(
    priv: Path, pub: Path, force: bool = False
) -> tuple[bytes, bytes, str]:
    """Generates an Ed25519 keypair and saves to the specified PEM files.

    Args:
        priv (Path): Path to save the private key PEM file (created with mode 0600).
        pub (Path): Path to save the public key PEM file.
        force (bool): Overwrite existing files. Otherwise FileExistsError is raised.
    Returns:
        tuple: (private_key_pem, public_key_pem, public_key_b64_der)
    """
    if not force:
        for path in (priv, pub):
            if path.exists():
                raise FileExistsError(f"'{path}' already exists")

    priv_pem, pub_pem, pub_b64 = generate_keypair()

    write_private_key(priv, priv_pem, force)
    pub.write_bytes(pub_pem)

    return priv_pem, pub_pem, pub_b64


def public_key_to_der(public_key) -> bytes:
    """
    Export a public key to DER format.

    Args:
        public_key: The public key object to export.

    Returns:
        bytes: The public key in DER format.
    """
    return public_key.public_bytes(
        encoding=serialization.Encoding.DER,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    )


def public_key_to_b64der(public_key) -> bytes:
    """
    Export a public key to DER format and then encode it in Base64.
    Args:
        public_key: The public key object to export.
    Returns:
        str: The public key in Base64-encoded DER format.
    """
    return base64.b64encode(
        public_key.public_bytes(
            encoding=serialization.Encoding.DER,
            format=serialization.PublicFormat.SubjectPublicKeyInfo,
        )
    ).decode("utf-8")


def b64der_to_public_key(content: str) -> str:
    """
    Decode a Base64-encoded DER public key and return it in PEM format.
    Args:
        content (str): The Base64-encoded DER content of the public key.
    Returns:
        str: The public key in PEM format (raw text).
    """
    public_key = load_public_key_b64_der(content)

    # Serialize the public key back to PEM format (raw text)
    pem_key = public_key.public_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    )

    return pem_key.decode("utf-8")


# ============================================================
# Key loading
# ============================================================


def _require_ed25519_public(key):
    if not isinstance(key, ed25519.Ed25519PublicKey):
        raise ValueError("Public key is not an Ed25519 key")
    return key


def load_private_key_pem(pem_bytes: bytes) -> ed25519.Ed25519PrivateKey:
    """
    Load an Ed25519 private key from PEM bytes.
    Args:
        pem_bytes (bytes): The PEM-encoded private key bytes.
    Returns:
        The private key object.
    Raises:
        ValueError: If the PEM is invalid or the key is not Ed25519.
    """
    key = serialization.load_pem_private_key(pem_bytes, password=None)
    if not isinstance(key, ed25519.Ed25519PrivateKey):
        raise ValueError("Private key is not an Ed25519 key")
    return key


def load_public_key_pem(pem_bytes: bytes) -> ed25519.Ed25519PublicKey:
    """
    Load an Ed25519 public key from PEM bytes.
    Args:
        pem_bytes (bytes): The PEM-encoded public key bytes.
    Returns:
        The public key object.
    Raises:
        ValueError: If the PEM is invalid or the key is not Ed25519.
    """
    return _require_ed25519_public(serialization.load_pem_public_key(pem_bytes))


def decode_b64_strict(b64: str) -> bytes:
    """Decode standard Base64, rejecting characters outside the alphabet."""
    try:
        return base64.b64decode(b64.encode("ascii"), validate=True)
    except (binascii.Error, UnicodeEncodeError) as e:
        raise ValueError(f"Invalid Base64 value: {e}") from e


def load_public_key_b64_der(b64: str) -> ed25519.Ed25519PublicKey:
    """
    Load an Ed25519 public key from a Base64-encoded DER string.
    Args:
        b64 (str): The Base64-encoded DER string of the public key.
    Returns:
        The public key object.
    Raises:
        ValueError: If the Base64 is malformed, the DER is invalid or the key
            is not Ed25519.
    """
    der = decode_b64_strict(b64)
    return _require_ed25519_public(serialization.load_der_public_key(der))
