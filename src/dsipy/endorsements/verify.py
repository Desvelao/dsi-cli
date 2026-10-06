from dataclasses import dataclass
from datetime import datetime, timezone
from typing import List, Optional

from ..core.model import DSI_DATE_FORMAT, Endorsement, Profile, RevokedKey
from ..crypto.keys import load_public_key_b64_der
from ..crypto.verification import decode_signature_hex, verify_endorsement_signature

VALID = "valid"
INVALID = "invalid"
UNVERIFIABLE = "unverifiable"


@dataclass
class EndorsementResult:
    endorsement: Endorsement
    status: str  # VALID, INVALID or UNVERIFIABLE
    signer_key_b64: Optional[str] = None
    reason: str = ""


def parse_dsi_date(value: Optional[str]) -> Optional[datetime]:
    """Parse YYYYMMDDThhmmssZ, returning None if it is missing or malformed."""
    if not value:
        return None
    try:
        return datetime.strptime(value, DSI_DATE_FORMAT).replace(tzinfo=timezone.utc)
    except ValueError:
        return None


def _revocation_for(profile: Profile, key_b64: str) -> Optional[RevokedKey]:
    for revocation in profile.revocations:
        if revocation.key_b64 == key_b64:
            return revocation
    return None


def key_accepts_endorsement(
    profile: Profile, key_b64: str, endorsement: Endorsement
) -> tuple[bool, str]:
    """Apply the revocation rules of RFC section 3.2.1.3 to an endorsement."""
    revocation = _revocation_for(profile, key_b64)
    if revocation is None:
        return True, ""
    reason = revocation.reason or ""
    if reason == "compromised":
        return False, "the signing key was revoked as compromised"
    if reason == "deprecated":
        return True, ""
    revoked_at = parse_dsi_date(revocation.date)
    signed_at = parse_dsi_date(endorsement.date)
    if revoked_at is None:
        return False, "the signing key was revoked without a valid DATE"
    if signed_at is None:
        return False, "the signing key was revoked and the endorsement has no DATE"
    if signed_at >= revoked_at:
        return False, "the endorsement was signed after the key was revoked"
    return True, ""


def verify_endorsements(profile: Profile) -> List[EndorsementResult]:
    """Verify each X-ENDORSE of the profile against the profile's own keys."""
    keys = []
    for key in sorted(
        profile.keys, key=lambda k: k.pref if k.pref is not None else float("inf")
    ):
        if key.alg and key.alg != "ed25519":
            continue
        try:
            keys.append((key.key_b64, load_public_key_b64_der(key.key_b64)))
        except ValueError:
            continue

    results = []
    for endorsement in profile.endorsements:
        if not keys:
            results.append(
                EndorsementResult(endorsement, UNVERIFIABLE, reason="no usable KEY")
            )
            continue
        try:
            load_public_key_b64_der(endorsement.endorsee_key_b64)
            decode_signature_hex(endorsement.signature_hex)
        except ValueError as e:
            results.append(EndorsementResult(endorsement, INVALID, reason=str(e)))
            continue

        reason = "signature does not match any key"
        verified = None
        for key_b64, key in keys:
            if verify_endorsement_signature(
                key, endorsement.endorsee_key_b64, endorsement.signature_hex
            ):
                accepted, why = key_accepts_endorsement(profile, key_b64, endorsement)
                if accepted:
                    verified = key_b64
                    break
                reason = why
        if verified:
            results.append(EndorsementResult(endorsement, VALID, verified))
        else:
            results.append(EndorsementResult(endorsement, INVALID, reason=reason))
    return results
