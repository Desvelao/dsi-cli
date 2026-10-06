"""Validation of a DSI vCard against the specification."""

from dataclasses import dataclass, field
import re
from typing import List
from urllib.parse import urlsplit

from ..crypto.keys import load_public_key_b64_der
from ..endorsements.verify import (
    INVALID,
    UNVERIFIABLE,
    parse_dsi_date,
    verify_endorsements,
)
from .model import REVOCATION_REASONS, Profile

CONFIDENCE_LEVELS = {"low", "medium", "high"}
SIGNATURE_RE = re.compile(r"^[0-9a-f]{128}$")
LANGUAGE_RE = re.compile(r"^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*$")
TAG_RE = re.compile(r"^[A-Za-z0-9]+(,[A-Za-z0-9]+)*$")
PLATFORM_RE = re.compile(r"^[a-z]+$")
REVISION_RE = re.compile(r"^\d{2}$")
FEATURE_RE = re.compile(r"^[A-Za-z0-9-]+$")


@dataclass
class Issue:
    code: str
    message: str

    def to_dict(self) -> dict:
        return {"code": self.code, "message": self.message}


@dataclass
class ValidationResult:
    errors: List[Issue] = field(default_factory=list)
    warnings: List[Issue] = field(default_factory=list)

    @property
    def valid(self) -> bool:
        return not self.errors

    def error(self, code: str, message: str) -> None:
        self.errors.append(Issue(code, message))

    def warn(self, code: str, message: str) -> None:
        self.warnings.append(Issue(code, message))

    def to_dict(self) -> dict:
        return {
            "valid": self.valid,
            "errors": [i.to_dict() for i in self.errors],
            "warnings": [i.to_dict() for i in self.warnings],
        }


def _absolute_url(value: str):
    parts = urlsplit(value or "")
    if parts.scheme and parts.netloc and not any(c.isspace() for c in value):
        return parts
    return None


def _check_url(
    result: ValidationResult, code: str, label: str, value: str, require_http=False
) -> None:
    parts = _absolute_url(value)
    if parts is None:
        result.error(f"{code}-invalid", f"{label} is not an absolute URL: '{value}'")
        return
    if require_http and parts.scheme not in ("http", "https"):
        result.error(
            f"{code}-scheme", f"{label} must use http or https, found '{parts.scheme}'"
        )
    elif parts.scheme == "http":
        result.warn(f"{code}-http", f"{label} does not use HTTPS: {value}")


def _count(profile: Profile, attr_name: str) -> int:
    return sum(1 for line in profile.raw_lines if line["attr_name"] == attr_name)


def _duplicates(values) -> set:
    seen, duplicated = set(), set()
    for value in values:
        if value in seen:
            duplicated.add(value)
        seen.add(value)
    return duplicated


def validate_profile(profile: Profile) -> ValidationResult:
    result = ValidationResult()
    raw = profile.raw or ""

    # --- structure -------------------------------------------------------
    stripped = raw.strip()
    if not stripped.startswith("BEGIN:VCARD") or not stripped.endswith("END:VCARD"):
        result.error(
            "structure", "Content must start with BEGIN:VCARD and end with END:VCARD"
        )
    if "\n" in raw.replace("\r\n", ""):
        result.warn("line-endings", "Lines are not terminated with CRLF (RFC 6350)")
    for message in profile.errors:
        result.error("malformed-line", message)

    if profile.version is None:
        result.error("version-missing", "VERSION property is missing")
    elif profile.version != "4.0":
        result.error("version", f"VERSION must be 4.0, found '{profile.version}'")
    if _count(profile, "version") > 1:
        result.error("version-duplicate", "VERSION appears more than once")

    if not profile.fn:
        result.error("fn-missing", "FN property is missing or empty")

    # --- SOURCE ----------------------------------------------------------
    if not profile.source:
        result.error("source-missing", "SOURCE property is required")
    else:
        _check_url(result, "source", "SOURCE", profile.source, require_http=True)
        if _count(profile, "source") > 1:
            result.warn("source-duplicate", "SOURCE appears more than once")

    for attr, label in (("url", "URL"), ("photo", "PHOTO")):
        value = getattr(profile, attr, None)
        if value and not value.startswith("data:"):
            _check_url(result, attr, label, value)

    # --- keys ------------------------------------------------------------
    valid_key_values = []
    for index, key in enumerate(profile.keys, start=1):
        label = f"KEY #{index}"
        if key.alg != "ed25519":
            result.error(
                "key-alg", f"{label}: ALG must be ed25519, found '{key.alg or 'none'}'"
            )
            continue
        try:
            load_public_key_b64_der(key.key_b64)
            valid_key_values.append(key.key_b64)
        except ValueError as e:
            result.error("key-invalid", f"{label}: {e}")
    if not profile.keys:
        result.warn(
            "key-missing", "No KEY: endorsements and feed signatures cannot be verified"
        )
    for duplicated in _duplicates(k.key_b64 for k in profile.keys):
        result.warn(
            "key-duplicate", f"KEY appears more than once: {duplicated[:16]}..."
        )
    preferred = [k for k in profile.keys if k.pref == 1]
    if len(preferred) > 1:
        result.error("key-pref", "More than one KEY is marked PREF=1")
    elif profile.keys and not preferred:
        result.warn("key-pref-missing", "No KEY is marked PREF=1")

    # --- revoked keys ----------------------------------------------------
    revoked_values = set()
    for index, revocation in enumerate(profile.revocations, start=1):
        label = f"REVKEY #{index}"
        revoked_values.add(revocation.key_b64)
        try:
            load_public_key_b64_der(revocation.key_b64)
        except ValueError as e:
            result.error("revkey-invalid", f"{label}: {e}")
        if revocation.reason is None:
            result.warn("revkey-reason-missing", f"{label}: REASON is missing")
        elif revocation.reason not in REVOCATION_REASONS:
            result.error(
                "revkey-reason",
                f"{label}: unknown REASON '{revocation.reason}' "
                f"(expected one of {', '.join(sorted(REVOCATION_REASONS))})",
            )
        if revocation.date is None:
            result.warn("revkey-date-missing", f"{label}: DATE is missing")
        elif parse_dsi_date(revocation.date) is None:
            result.error(
                "revkey-date",
                f"{label}: DATE must use YYYYMMDDThhmmssZ, found '{revocation.date}'",
            )
    for key in preferred:
        if key.key_b64 in revoked_values:
            result.error("key-revoked", "The preferred KEY is also listed as revoked")

    # --- endorsements ----------------------------------------------------
    for index, endorsement in enumerate(profile.endorsements, start=1):
        label = f"X-ENDORSE #{index}"
        if not SIGNATURE_RE.match(endorsement.signature_hex):
            result.error(
                "endorse-sig-format",
                f"{label}: SIG must be 128 lowercase hexadecimal characters",
            )
        if endorsement.date and parse_dsi_date(endorsement.date) is None:
            result.error(
                "endorse-date",
                f"{label}: DATE must use YYYYMMDDThhmmssZ, found '{endorsement.date}'",
            )
        if endorsement.confidence and endorsement.confidence not in CONFIDENCE_LEVELS:
            result.error(
                "endorse-confidence",
                f"{label}: CONFIDENCE must be low, medium or high",
            )
    for duplicated in _duplicates(e.endorsee_key_b64 for e in profile.endorsements):
        result.warn(
            "endorse-duplicate", f"Key endorsed more than once: {duplicated[:16]}..."
        )
    for index, outcome in enumerate(verify_endorsements(profile), start=1):
        label = f"X-ENDORSE #{index}"
        if outcome.status == INVALID and SIGNATURE_RE.match(
            outcome.endorsement.signature_hex
        ):
            result.error("endorse-signature", f"{label}: {outcome.reason}")
        elif outcome.status == UNVERIFIABLE:
            result.warn("endorse-unverifiable", f"{label}: {outcome.reason}")

    # --- feeds -----------------------------------------------------------
    for index, feed in enumerate(profile.feeds, start=1):
        label = f"X-FEED #{index}"
        _check_url(result, "feed", label, feed.url, require_http=True)
        if feed.language and not LANGUAGE_RE.match(feed.language):
            result.error(
                "feed-language",
                f"{label}: LANGUAGE is not a BCP 47 tag: '{feed.language}'",
            )
        if feed.tags and not TAG_RE.match(feed.tags):
            result.error(
                "feed-tags", f"{label}: TAGS must be comma-separated alphanumerics"
            )
    for duplicated in _duplicates(f.url for f in profile.feeds):
        result.warn("feed-duplicate", f"Feed URL appears more than once: {duplicated}")

    # --- social identifiers ----------------------------------------------
    for index, social in enumerate(profile.social, start=1):
        label = f"X-SOCIAL #{index}"
        if not PLATFORM_RE.match(social.platform):
            result.error(
                "social-platform",
                f"{label}: PLATFORM must be lowercase letters a-z, found "
                f"'{social.platform}'",
            )
        if not social.value:
            result.error("social-value", f"{label}: value is empty")
    for duplicated in _duplicates((s.platform, s.value) for s in profile.social):
        result.warn(
            "social-duplicate", f"Duplicate X-SOCIAL: {duplicated[0]}:{duplicated[1]}"
        )

    # --- X-DSI-VERSION ---------------------------------------------------
    if profile.dsi_version:
        if not REVISION_RE.match(profile.dsi_version.revision):
            result.error(
                "dsi-version",
                f"X-DSI-VERSION must be a two-digit revision, found "
                f"'{profile.dsi_version.revision}'",
            )
        for feature in profile.dsi_version.features:
            if not FEATURE_RE.match(feature):
                result.error(
                    "dsi-version-feature", f"Invalid FEATURES token '{feature}'"
                )
    if _count(profile, "x-dsi-version") > 1:
        result.warn("dsi-version-duplicate", "X-DSI-VERSION appears more than once")

    return result
