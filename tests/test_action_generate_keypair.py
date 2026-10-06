import os
import stat
import tempfile
import unittest
from pathlib import Path

from src.dsipy.crypto.keys import (
    action_generate_keypair,
    load_private_key_pem,
    load_public_key_b64_der,
    load_public_key_pem,
)


class TestActionGenerateKeypair(unittest.TestCase):
    def test_generates_keypair_saves_files_and_returns_content(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            priv_path = Path(tmp_dir) / "private.pem"
            pub_path = Path(tmp_dir) / "public.pem"

            priv_pem, pub_pem, pub_b64 = action_generate_keypair(priv_path, pub_path)

            self.assertTrue(priv_path.exists())
            self.assertTrue(pub_path.exists())

            self.assertEqual(priv_path.read_bytes(), priv_pem)
            self.assertEqual(pub_path.read_bytes(), pub_pem)

            self.assertGreater(len(pub_b64), 0)
            self.assertIsInstance(pub_b64, str)

            private_key = load_private_key_pem(priv_pem)
            public_key_pem = load_public_key_pem(pub_pem)
            public_key_b64 = load_public_key_b64_der(pub_b64)

            self.assertIsNotNone(private_key)
            self.assertIsNotNone(public_key_pem)
            self.assertIsNotNone(public_key_b64)

    def test_private_key_is_created_with_mode_0600(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            priv_path = Path(tmp_dir) / "private.pem"
            old_umask = os.umask(0o022)
            try:
                action_generate_keypair(priv_path, Path(tmp_dir) / "public.pem")
            finally:
                os.umask(old_umask)
            self.assertEqual(stat.S_IMODE(priv_path.stat().st_mode), 0o600)

    def test_refuses_to_overwrite_existing_files(self):
        for existing in ("private.pem", "public.pem"):
            with tempfile.TemporaryDirectory() as tmp_dir:
                priv_path = Path(tmp_dir) / "private.pem"
                pub_path = Path(tmp_dir) / "public.pem"
                (Path(tmp_dir) / existing).write_bytes(b"keep")
                with self.assertRaises(FileExistsError):
                    action_generate_keypair(priv_path, pub_path)
                self.assertEqual((Path(tmp_dir) / existing).read_bytes(), b"keep")
                other = pub_path if existing == "private.pem" else priv_path
                self.assertFalse(other.exists())

    def test_force_overwrites_and_tightens_mode(self):
        with tempfile.TemporaryDirectory() as tmp_dir:
            priv_path = Path(tmp_dir) / "private.pem"
            pub_path = Path(tmp_dir) / "public.pem"
            priv_path.write_bytes(b"old")
            priv_path.chmod(0o644)
            priv_pem, _, _ = action_generate_keypair(priv_path, pub_path, force=True)
            self.assertEqual(priv_path.read_bytes(), priv_pem)
            self.assertEqual(stat.S_IMODE(priv_path.stat().st_mode), 0o600)


if __name__ == "__main__":
    unittest.main()
