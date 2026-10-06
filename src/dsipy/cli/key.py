from pathlib import Path
import sys
import typer
from .common import Cli, echo_keypair_generated
from ..core.lifecycle import add_key, revoke_key, rotate_key
from ..vcard.parser import parse_vcard
from ..crypto.keys import (
    action_generate_keypair,
    generate_keypair,
    write_private_key,
    load_public_key_pem,
    public_key_to_b64der,
    b64der_to_public_key,
)

app = Cli(
    help="Commands related to keys", no_args_is_help=True
)


@app.command(help="Generate a new Ed25519 keypair and save to PEM files")
def create(
    priv: Path = typer.Option(
        "private.pem", "--priv", help="Path to save the private key PEM file"
    ),
    pub: Path = typer.Option(
        "public.pem", "--pub", help="Path to save the public key PEM file"
    ),
    force: bool = typer.Option(
        False, "--force", help="Overwrite existing key files"
    ),
):
    try:
        _, _, pub_b64 = action_generate_keypair(priv, pub, force=force)
    except FileExistsError as e:
        typer.secho(
            f"❌ {e}; refusing to overwrite a key file (use --force).",
            fg=typer.colors.RED,
        )
        raise typer.Exit(code=1)
    echo_keypair_generated(priv, pub, pub_b64)


@app.command(
    help="Convert a public key PEM file to Base64-encoded DER format for vCard use"
)
def pub_encode(
    file: Path = typer.Argument(..., help="Path to the public key PEM file to convert")
):
    if not file.is_file():
        typer.secho(f"❌ '{file}' is not a file.", fg=typer.colors.RED)
        raise typer.Exit(code=1)

    b64der_text = public_key_to_b64der(load_public_key_pem(file.read_bytes()))

    print(b64der_text)


@app.command(
    help="Convert a Base64-encoded DER format for vCard use to public key PEM file"
)
def pub_decode(
    content: str = typer.Argument(
        None, help="Base64-encoded DER content to decode and display as PEM"
    ),
):
    if content is None:
        content = sys.stdin.read().strip()

    if not content:
        typer.secho("❌ No content provided.", fg=typer.colors.RED)
        raise typer.Exit(code=1)

    print(b64der_to_public_key(content))


@app.command(
    help="Rotate the preferred key of a vCard: generate a new keypair, make it the "
    "preferred KEY and add a REVKEY for the old one."
)
def rotate(
    vcard: Path = typer.Argument(..., help="vCard file to update"),
    priv: Path = typer.Option(
        "private.pem", "--priv", help="Where to save the new private key PEM"
    ),
    pub: Path = typer.Option(
        "public.pem", "--pub", help="Where to save the new public key PEM"
    ),
    old_key: str = typer.Option(
        None,
        "--old-key",
        help="Base64 DER of the key to revoke (default: the PREF=1 key)",
    ),
    reason: str = typer.Option(
        "rotated", "--reason", help="Revocation reason: rotated or superseded"
    ),
    output: Path = typer.Option(
        None,
        "--output",
        "-o",
        help="Write the result here instead of updating the vCard",
    ),
):
    if not vcard.is_file():
        typer.secho(f"❌ '{vcard}' is not a file.", fg=typer.colors.RED)
        raise typer.Exit(code=1)
    for path in (priv, pub):
        if path.exists():
            typer.secho(
                f"❌ '{path}' already exists; refusing to overwrite a key file.",
                fg=typer.colors.RED,
            )
            raise typer.Exit(code=1)

    priv_pem, pub_pem, new_b64 = generate_keypair()
    updated = rotate_key(
        vcard.read_text(encoding="utf-8"), new_b64, old_key_b64=old_key, reason=reason
    )

    write_private_key(priv, priv_pem)
    pub.write_bytes(pub_pem)
    (output or vcard).write_text(updated, encoding="utf-8", newline="")

    typer.secho(f"✅ Key rotated in '{output or vcard}'", fg=typer.colors.GREEN)
    typer.secho(f"   New private key: {priv}", fg=typer.colors.BLUE)
    typer.secho(f"   New public key (Base64 DER): {new_b64}", fg=typer.colors.BLUE)


@app.command(
    help="Add a new key to a vCard without revoking anything (use it when every "
    "key is revoked, or to add an extra key)."
)
def add(
    vcard: Path = typer.Argument(..., help="vCard file to update"),
    priv: Path = typer.Option(
        "private.pem", "--priv", help="Where to save the new private key PEM"
    ),
    pub: Path = typer.Option(
        "public.pem", "--pub", help="Where to save the new public key PEM"
    ),
    public_key: str = typer.Option(
        None,
        "--public-key",
        help="Add this existing Base64 DER public key instead of generating a keypair",
    ),
    pref: bool = typer.Option(
        True,
        "--pref/--no-pref",
        help="Make the new key the preferred one (PREF=1); the other keys lose PREF",
    ),
    output: Path = typer.Option(
        None,
        "--output",
        "-o",
        help="Write the result here instead of updating the vCard",
    ),
):
    if not vcard.is_file():
        typer.secho(f"❌ '{vcard}' is not a file.", fg=typer.colors.RED)
        raise typer.Exit(code=1)

    generated = public_key is None
    if generated:
        for path in (priv, pub):
            if path.exists():
                typer.secho(
                    f"❌ '{path}' already exists; refusing to overwrite a key file.",
                    fg=typer.colors.RED,
                )
                raise typer.Exit(code=1)
        priv_pem, pub_pem, new_b64 = generate_keypair()
    else:
        new_b64 = public_key.strip()

    updated = add_key(vcard.read_text(encoding="utf-8"), new_b64, preferred=pref)

    if generated:
        write_private_key(priv, priv_pem)
        pub.write_bytes(pub_pem)
    (output or vcard).write_text(updated, encoding="utf-8", newline="")

    typer.secho(f"✅ Key added to '{output or vcard}'", fg=typer.colors.GREEN)
    if generated:
        typer.secho(f"   New private key: {priv}", fg=typer.colors.BLUE)
    typer.secho(f"   New public key (Base64 DER): {new_b64}", fg=typer.colors.BLUE)


@app.command(help="Revoke a key listed in a vCard by adding a REVKEY property.")
def revoke(
    vcard: Path = typer.Argument(..., help="vCard file to update"),
    key: str = typer.Option(None, "--key", help="Base64 DER of the key to revoke"),
    pub: Path = typer.Option(
        None, "--pub", help="Public key PEM of the key to revoke (alternative to --key)"
    ),
    reason: str = typer.Option(
        ...,
        "--reason",
        help="compromised, rotated, superseded, retired, lost or deprecated",
    ),
    output: Path = typer.Option(
        None,
        "--output",
        "-o",
        help="Write the result here instead of updating the vCard",
    ),
):
    if not vcard.is_file():
        typer.secho(f"❌ '{vcard}' is not a file.", fg=typer.colors.RED)
        raise typer.Exit(code=1)
    if (key is None) == (pub is None):
        typer.secho("❌ Pass exactly one of --key or --pub.", fg=typer.colors.RED)
        raise typer.Exit(code=1)
    key_b64 = key or public_key_to_b64der(load_public_key_pem(pub.read_bytes()))

    updated = revoke_key(vcard.read_text(encoding="utf-8"), key_b64, reason)
    (output or vcard).write_text(updated, encoding="utf-8", newline="")

    typer.secho(
        f"✅ Key revoked ({reason}) in '{output or vcard}'", fg=typer.colors.GREEN
    )
    remaining = parse_vcard(updated)
    revoked = {r.key_b64 for r in remaining.revocations}
    if not any(k.key_b64 not in revoked for k in remaining.keys):
        typer.secho(
            "⚠️ The vCard has no usable key left. Add one with `dsipy key add`.",
            fg=typer.colors.YELLOW,
        )
    elif reason != "deprecated" and not any(k.pref == 1 for k in remaining.keys):
        typer.secho(
            "⚠️ No key is preferred (PREF=1) now. Use `dsipy key add` or edit the vCard.",
            fg=typer.colors.YELLOW,
        )


# @app.command()
# def priv_decode(
#     content: str = typer.Argument(
#         None, help="Base64-encoded DER content to decode and display as PEM"
#     ),
# ):
#     if content is None:
#         content = sys.stdin.read().strip()

#     if not content:
#         typer.secho("❌ No content provided.", fg=typer.colors.RED)
#         raise typer.Exit(code=1)

#     print(b64der_to_private_key(content))


if __name__ == "__main__":
    app()
