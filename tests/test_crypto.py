import unittest

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import ec, ed25519, rsa

from src.dsipy.core.identity import VCard
from src.dsipy.crypto.keys import (
    load_private_key_pem,
    load_public_key_b64_der,
    load_public_key_pem,
    public_key_to_b64der,
)
from src.dsipy.crypto.signatures import sign_endorsement
from src.dsipy.crypto.verification import verify_endorsement_signature

# Keys and signatures of the example vCard in the specification (RFC section 7):
# the seed of each key is the name padded with zero bytes.
ALICE_SEED = b"alice".ljust(32, b"\0")
BOB_KEY = "MCowBQYDK2VwAyEA3XVgQP3VFF4r+YMtJk3QgOSz5zAWvfZXS0zYfqppf14="
ALICE_SIG_FOR_BOB = (
    "0c036138c11d467ad6518502d45dd0eeea4745b17ebea8dac01849cdd3946d67"
    "9a8c6bd039caff7077abfbc2aff9a1d96bff16b20134c1368fc93a2b17f07e00"
)


def alice():
    return ed25519.Ed25519PrivateKey.from_private_bytes(ALICE_SEED)


class TestEndorsementSignature(unittest.TestCase):
    def test_matches_the_specification_example(self):
        self.assertEqual(sign_endorsement(alice(), BOB_KEY), ALICE_SIG_FOR_BOB)

    def test_vcard_helper_signs_the_same_string(self):
        self.assertEqual(VCard.sign_endorsement(alice(), BOB_KEY), ALICE_SIG_FOR_BOB)

    def test_signature_round_trip(self):
        self.assertTrue(
            verify_endorsement_signature(
                alice().public_key(), BOB_KEY, ALICE_SIG_FOR_BOB
            )
        )

    def test_signature_for_another_key_is_rejected(self):
        self.assertFalse(
            verify_endorsement_signature(
                alice().public_key(), BOB_KEY[:-4] + "AAA=", ALICE_SIG_FOR_BOB
            )
        )


class TestStrictKeyLoading(unittest.TestCase):
    def test_accepts_a_valid_ed25519_key(self):
        key = load_public_key_b64_der(BOB_KEY)
        self.assertIsInstance(key, ed25519.Ed25519PublicKey)

    def test_rejects_characters_outside_the_base64_alphabet(self):
        with self.assertRaises(ValueError):
            load_public_key_b64_der("MCowBQYDK2VwAyEA!!3XVgQP3VFF4r+YMtJk3QgOS==")

    def test_rejects_bad_padding(self):
        with self.assertRaises(ValueError):
            load_public_key_b64_der(BOB_KEY[:-1])

    def test_rejects_rsa_public_key(self):
        rsa_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
        pem = rsa_key.public_key().public_bytes(
            serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo
        )
        with self.assertRaises(ValueError):
            load_public_key_pem(pem)
        der_b64 = public_key_to_b64der(rsa_key.public_key())
        with self.assertRaises(ValueError):
            load_public_key_b64_der(der_b64)

    def test_rejects_ec_private_key(self):
        pem = ec.generate_private_key(ec.SECP256R1()).private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.PKCS8,
            serialization.NoEncryption(),
        )
        with self.assertRaises(ValueError):
            load_private_key_pem(pem)


if __name__ == "__main__":
    unittest.main()
