import io
import tempfile
import unittest
from pathlib import Path
import typer
from src.dsipy.cli import key
from unittest.mock import patch


class TestKeyAppCommands(unittest.TestCase):
    def test_create_calls_action_generate_keypair(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            priv = Path(tmp_dir) / "private.pem"
            pub = Path(tmp_dir) / "public.pem"

            with patch(
                "src.dsipy.cli.key.action_generate_keypair",
                return_value=(b"priv", b"pub", "PUB_B64"),
            ) as mock_action:
                key.create.__wrapped__(priv=priv, pub=pub, force=False)

            mock_action.assert_called_once_with(priv, pub, force=False)

    def test_create_refuses_existing_key_and_force_overwrites(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            priv = Path(tmp_dir) / "private.pem"
            pub = Path(tmp_dir) / "public.pem"
            priv.write_bytes(b"keep")
            with self.assertRaises(typer.Exit):
                key.create.__wrapped__(priv=priv, pub=pub, force=False)
            self.assertEqual(priv.read_bytes(), b"keep")
            key.create.__wrapped__(priv=priv, pub=pub, force=True)
            self.assertNotEqual(priv.read_bytes(), b"keep")

    def test_encode_prints_b64der_for_valid_file(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            pub_file = Path(tmp_dir) / "public.pem"
            pub_file.write_bytes(b"fake-public-pem")

            with (
                patch(
                    "src.dsipy.cli.key.load_public_key_pem", return_value="pub-key"
                ) as mock_load,
                patch(
                    "src.dsipy.cli.key.public_key_to_b64der", return_value="BASE64_DER"
                ) as mock_b64,
                patch("builtins.print") as mock_print,
            ):
                key.pub_encode.__wrapped__(file=pub_file)

            mock_load.assert_called_once_with(b"fake-public-pem")
            mock_b64.assert_called_once_with("pub-key")
            mock_print.assert_called_once_with("BASE64_DER")

    def test_encode_raises_exit_for_missing_file(self):
        missing = Path("/tmp/this-file-should-not-exist-1234567890.pem")

        with patch("src.dsipy.cli.key.typer.secho") as mock_secho:
            with self.assertRaises(typer.Exit):
                key.pub_encode.__wrapped__(file=missing)

        mock_secho.assert_called_once()

    def test_decode_uses_argument_when_provided(self):
        with (
            patch(
                "src.dsipy.cli.key.b64der_to_public_key", return_value="PEM_CONTENT"
            ) as mock_decode,
            patch("builtins.print") as mock_print,
        ):
            key.pub_decode.__wrapped__(content="BASE64_DER")

        mock_decode.assert_called_once_with("BASE64_DER")
        mock_print.assert_called_once_with("PEM_CONTENT")

    def test_decode_reads_stdin_when_content_is_none(self):
        fake_stdin = io.StringIO("BASE64_FROM_STDIN\n")

        with (
            patch("src.dsipy.cli.key.sys.stdin", fake_stdin),
            patch(
                "src.dsipy.cli.key.b64der_to_public_key", return_value="PEM_FROM_STDIN"
            ) as mock_decode,
            patch("builtins.print") as mock_print,
        ):
            key.pub_decode.__wrapped__(content=None)

        mock_decode.assert_called_once_with("BASE64_FROM_STDIN")
        mock_print.assert_called_once_with("PEM_FROM_STDIN")

    def test_decode_raises_exit_when_no_content(self):
        with (
            patch("src.dsipy.cli.key.sys.stdin", io.StringIO("   \n")),
            patch("src.dsipy.cli.key.typer.secho") as mock_secho,
        ):
            with self.assertRaises(typer.Exit):
                key.pub_decode.__wrapped__(content=None)

        mock_secho.assert_called_once_with(
            "❌ No content provided.", fg=typer.colors.RED
        )


if __name__ == "__main__":
    unittest.main()
