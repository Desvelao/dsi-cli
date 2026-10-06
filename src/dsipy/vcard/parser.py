import io
from typing import Optional

from vobject.base import ParseError, getLogicalLines, parseLine

from ..core.model import (
    DsiVersion,
    Endorsement,
    Feed,
    Profile,
    PublicKey,
    RevokedKey,
    SocialIdentity,
    vcard_main_attributes,
)
from .escaping import unescape_text

# TEXT properties whose value is unescaped in the profile
_TEXT_ATTRIBUTES = {"fn", "nickname", "note", "email", "gender", "kind", "lang"}


def _params(raw_params: list) -> dict:
    """Convert vobject parameter lists into {NAME: "value[,value...]"}."""
    return {p[0].upper(): ",".join(p[1:]) for p in raw_params if p}


def parse_vcard(text: str) -> Profile:
    """Parse the text of a vCard into a Profile.

    Line unfolding, quoted parameter values and the line grammar come from
    vobject. Property values are kept as written (the Base64 values of KEY,
    REVKEY and X-ENDORSE are not decoded) and only TEXT properties are
    unescaped. Malformed lines are reported in ``profile.errors`` instead of
    aborting the parse, since clients must tolerate malformed extensions.
    The original text is kept in ``profile.raw``.
    """
    profile = Profile(raw=text)
    begin_count = 0
    end_seen = False
    stopped = False  # first card is over: only look for further BEGIN lines

    for logical_line, line_number in getLogicalLines(io.StringIO(text)):
        line = logical_line.strip()
        if not line:
            continue

        upper = line.upper()
        if upper == "BEGIN:VCARD":
            begin_count += 1
            if begin_count > 1:
                if begin_count == 2:
                    profile.errors.append(
                        "multiple vCards found; only the first is parsed"
                    )
                stopped = True
                continue
        if stopped:
            continue
        if upper == "END:VCARD":
            end_seen = stopped = True

        try:
            raw_name, raw_params, value, _group = parseLine(line, line_number)
        except ParseError as e:
            profile.errors.append(str(e))
            profile.raw_lines.append(
                {
                    "line": line,
                    "attr_name": None,
                    "value": None,
                    "attributes": {},
                    "name": None,
                    "params": [],
                    "raw_value": None,
                }
            )
            continue

        name = raw_name.upper()
        attributes = _params(raw_params)
        attr_name = None
        parsed_value: Optional[str] = value

        if name == "KEY":
            attr_name = "key"
            profile.keys.append(
                PublicKey(
                    alg=attributes.get("ALG", "").lower(),
                    key_b64=value.strip(),
                    pref=_parse_pref(attributes, profile, line_number),
                )
            )
        elif name == "REVKEY":
            attr_name = "revkey"
            profile.revocations.append(
                RevokedKey(
                    key_b64=value.strip(),
                    reason=attributes.get("REASON"),
                    date=attributes.get("DATE"),
                )
            )
        elif name == "X-ENDORSE":
            attr_name = "x-endorse"
            profile.endorsements.append(
                Endorsement(
                    endorsee_key_b64=value.strip(),
                    signature_hex=attributes.get("SIG", ""),
                    date=attributes.get("DATE"),
                    confidence=attributes.get("CONFIDENCE"),
                )
            )
        elif name == "X-FEED":
            attr_name = "x-feed"
            tags = attributes.get("TAGS", "")
            profile.feeds.append(
                Feed(
                    language=attributes.get("LANGUAGE", ""),
                    category=attributes.get("CATEGORY") or tags,
                    url=value.strip(),
                    tags=tags,
                )
            )
        elif name == "X-SOCIAL":
            attr_name = "x-social"
            profile.social.append(
                SocialIdentity(
                    platform=attributes.get("PLATFORM", "").lower(),
                    value=value.strip(),
                )
            )
        elif name == "X-DSI-VERSION":
            attr_name = "x-dsi-version"
            features = [f for f in attributes.get("FEATURES", "").split(",") if f]
            profile.dsi_version = DsiVersion(revision=value.strip(), features=features)
        elif name == "VERSION":
            attr_name = "version"
            profile.version = value.strip()
        elif name.lower() in vcard_main_attributes:
            attr_name = name.lower()
            parsed_value = (
                unescape_text(value) if attr_name in _TEXT_ATTRIBUTES else value
            )
            setattr(profile, attr_name, parsed_value)

        profile.raw_lines.append(
            {
                "line": line,
                "attr_name": attr_name,
                "value": parsed_value if attr_name else None,
                "attributes": attributes,
                "name": name,
                "params": [(p[0].upper(), ",".join(p[1:])) for p in raw_params if p],
                "raw_value": value,
                "group": _group,
            }
        )

    # Framing is only checked when the text has BEGIN/END at all, so bare
    # property snippets are still accepted. A missing VERSION is reported by the
    # validator (version-missing), not here.
    if begin_count or end_seen:
        if not begin_count:
            profile.errors.append("missing BEGIN:VCARD")
        if not end_seen:
            profile.errors.append("missing END:VCARD")

    return profile


def _parse_pref(attributes: dict, profile: Profile, line_number) -> Optional[int]:
    if "PREF" not in attributes:
        return None
    try:
        return int(attributes["PREF"])
    except ValueError:
        profile.errors.append(
            f"At line {line_number}: invalid PREF value '{attributes['PREF']}'"
        )
        return None
