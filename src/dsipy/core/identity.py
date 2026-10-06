from dataclasses import asdict
import json
from pathlib import Path
import re
from typing import List, Optional
from ..crypto.signatures import sign_endorsement
from ..vcard import serializer
from ..vcard.parser import parse_vcard
from ..vcard.serializer import build_vcard_from_raw_lines
from .files import file_is_vcard, get_local_files_from_inputs
from .model import Profile, PublicKey
from .resolver import fetch_vcard_from_url


class VCard:
    def __init__(
        self,
        text: str = None,
        path: Path = None,
        url: str = None,
        allow_http: bool = False,
        verify_source: bool = True,
    ):
        self.profile = Profile()
        if text is not None:
            self.profile = parse_vcard(text)
        elif path:
            if not path.is_file():
                raise ValueError(f"The specified path is not a file: {path}")
            self.path = path
            self.profile = parse_vcard(path.read_bytes().decode("utf-8"))
        elif url:
            text, filename = fetch_vcard_from_url(
                url, allow_http=allow_http, verify_source=verify_source
            )
            self.profile = parse_vcard(text)
            self.url = url
            self.path = Path(filename if file_is_vcard(filename) else f"{filename}.vcf")
        else:
            raise ValueError(
                "Either text, path, or url must be provided to initialize the vCard."
            )

    def parse(self, text: str) -> Profile:
        """Parse vCard text and update the profile."""
        self.profile = parse_vcard(text)
        return self.profile

    def build(self) -> str:
        """Build vCard content from the current profile."""
        return build_vcard_from_raw_lines(self.profile)

    def add_line(self, line: str) -> None:
        """Add a line to the vCard."""
        if "\n" in line or "\r" in line:
            raise ValueError("A vCard line cannot contain line breaks")
        content = self.profile.raw or self.build()
        end_vcard = "END:VCARD"
        if content:
            if not content.strip().endswith(end_vcard):
                raise ValueError(f"Invalid vCard format: missing {end_vcard}")
            newline = "\r\n" if "\r\n" in content else "\n"
            position = content.rindex(end_vcard)
            new_content = f"{content[:position]}{line}{newline}{content[position:]}"
            self.profile = self.parse(new_content)  # Re-parse to update the profile

    def to_string(self) -> str:
        """Return the vCard as a string."""
        return self.profile.raw or self.build()

    def to_file(self, path: Path = None) -> None:
        """Save the vCard to a file."""
        file_path = path if path else getattr(self, "path", None)
        if not file_path:
            raise ValueError("No path specified for saving the vCard.")
        file_path.write_text(self.to_string(), encoding="utf-8", newline="")

    def to_json(self) -> str:
        """Return the vCard profile as a JSON string."""
        return json.dumps(asdict(self.profile), ensure_ascii=False)

    def get_preferred_key(self) -> Optional[PublicKey]:
        """Get the preferred usable public key based on the PREF parameter.

        Keys listed in REVKEY and non-ed25519 keys (the only supported
        algorithm) are skipped. Returns None if no usable key remains.
        """
        revoked = {r.key_b64 for r in self.profile.revocations}
        usable = [
            k
            for k in self.profile.keys
            if k.key_b64 not in revoked and k.alg == "ed25519"
        ]
        if not usable:
            return None
        # The key with the lowest PREF value has the highest priority
        return min(usable, key=lambda k: k.pref if k.pref is not None else float("inf"))

    @staticmethod
    def sign_endorsement(private_key, endorsee_key_b64: str) -> str:
        """Return the endorsement signature in hexadecimal format."""
        return sign_endorsement(private_key, endorsee_key_b64)

    def has_endorsement_for_key(self, endorsee_key_b64: str) -> bool:
        """Check if there is an endorsement for the given endorsee key."""
        return any(
            e.endorsee_key_b64 == endorsee_key_b64 for e in self.profile.endorsements
        )

    @staticmethod
    def build_custom_attribute_social_platform(name):
        return serializer.build_custom_attribute_social_platform(name)

    @staticmethod
    def build_custom_attribute(name):
        return serializer.build_custom_attribute(name)

    @staticmethod
    def build_custom_attribute_endorsement(*args, **kwargs):
        return serializer.build_custom_attribute_endorsement(*args, **kwargs)

    @staticmethod
    def build_content(*args, **kwargs):
        return serializer.build_content(*args, **kwargs)


class VCardInputs:
    def __init__(self, inputs: List[str]):
        self.inputs = inputs
        self.classified = VCardInputs.classify_inputs(inputs)
        self.warnings: List[str] = []
        self.vcard_files = VCardInputs.get_vcard_local_files_from_inputs(
            self.classified["paths"], self.warnings
        )
        self.vcard_urls = self.classified["urls"]

    @staticmethod
    def classify_inputs(inputs):
        """
        Classify inputs as URLs or local file/directory paths.

        Args:
            inputs (list): List of input strings (URLs or file paths).

        Returns:
            dict: Dictionary with 'urls' and 'paths' keys, each containing a list of classified inputs.
        """
        classified = {"urls": [], "paths": []}

        url_pattern = r"^https?://"

        for input_item in inputs:
            if isinstance(input_item, str) and re.match(url_pattern, input_item):
                classified["urls"].append(input_item)
            else:
                classified["paths"].append(
                    Path(input_item) if isinstance(input_item, str) else input_item
                )

        return classified

    @staticmethod
    def get_vcard_local_files_from_inputs(
        inputs: List[Path], warnings: Optional[List[str]] = None
    ) -> List[Path]:

        return get_local_files_from_inputs(inputs, file_is_vcard, warnings)
