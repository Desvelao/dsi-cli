from ..core.model import Profile
from .escaping import escape_text, fold_line, join_structured

# Default ENCODING of KEY values (Base64), as written by the lifecycle commands
DEFAULT_KEY_ENCODING = "b"
# Default LANGUAGE of NOTE when the card has no lang
DEFAULT_NOTE_LANGUAGE = "en-US"


def build_custom_attribute_social_platform(name):
    """
    Build a custom attribute string for the vCard.

    Args:
        name (str): The name of the custom attribute.

    Returns:
        str: The formatted custom attribute string.
    """
    return f"X-SOCIAL;PLATFORM={name.strip().lower()}"


def build_custom_attribute(name):
    """
    Build a custom attribute string for the vCard.

    Args:
        name (str): The name of the custom attribute.

    Returns:
        str: The formatted custom attribute string.
    """
    return f"{name.strip().upper()}="


def build_custom_attribute_endorsement(
    canonical_value: str, signature_hex, date=None, confidence=None, encoding="b"
):
    """
    Build a custom endorsement attribute string for the vCard.

    Args:
        signature_hex (str): The endorsement signature in hexadecimal format.
        date (str, optional): The date of the endorsement in ISO format (YYYY-MM-DD). Defaults to None.
        confidence (str, optional): The confidence level of the endorsement. Defaults to None.
        encoding (str, optional): The encoding of the endorsement. Defaults to "b".
    Returns:
        str: The formatted custom endorsement attribute string.
    """
    params = []
    params.append(f"SIG={signature_hex}")
    if date:
        params.append(f"DATE={date}")
    if confidence:
        params.append(f"CONFIDENCE={confidence}")
    params.append(f"ENCODING={encoding}")
    params_str = ";" + ";".join(params) if params else ""
    return f"X-ENDORSE{params_str}:{canonical_value}"


def build_content(
    fn=None,
    n=None,
    nickname=None,
    lang=None,
    gender=None,
    email=None,
    categories=None,
    bday=None,
    anniversary=None,
    kind=None,
    adr=None,
    tel=None,
    impp=None,
    photo=None,
    note=None,
    url=None,
    source=None,
    custom_attributes=None,
    keys=None,
):
    """
    Build the vCard content based on the provided fields.

    Args:
        fn (str): Full name.
        n (str): Name components.
        nickname (str): Nickname.
        lang (str): Language.
        gender (str): Gender.
        email (str): Email address.
        categories (str): Categories.
        bday (str): Birthday.
        anniversary (str): Anniversary.
        kind (str): Kind of contact.
        adr (str): Address.
        tel (str): Telephone number.
        impp (str): Instant messaging and presence protocol.
        photo (str): Photo URL.
        note (str): Note.
        url (str): URL.
        source (str): Source URL.
        custom_attributes (dict): Custom attributes as key-value pairs.
        keys (list): List of public keys, each as a dict with 'alg', 'key_b64', and optional 'pref'.

    Returns:
        str: The generated vCard content.
    """
    if custom_attributes is None:
        custom_attributes = {}

    # Values written raw (URIs, dates, ...) must not be able to inject new lines.
    # Text fields are escaped below, so they may contain line breaks.
    escaped_fields = {
        "fn",
        "nickname",
        "email",
        "note",
        "n",
        "lang",
        "gender",
        "categories",
        "kind",
        "adr",
    }
    for field_name, field_value in list(locals().items()):
        if field_name in escaped_fields or field_name in ("custom_attributes", "keys"):
            continue
        if isinstance(field_value, str) and (
            "\n" in field_value or "\r" in field_value
        ):
            raise ValueError(f"Line breaks are not allowed in '{field_name}'")
    for attribute_name, attribute_value in custom_attributes.items():
        if any(c in f"{attribute_name}{attribute_value}" for c in "\r\n"):
            raise ValueError(f"Line breaks are not allowed in '{attribute_name}'")

    lines = ["BEGIN:VCARD", "VERSION:4.0"]

    # N, ADR and GENDER are structured (";"), CATEGORIES is a list (","). A str
    # keeps its separators; pass a list/tuple to get components escaped.
    if fn:
        lines.append(f"FN:{escape_text(fn)}")
    if n:
        lines.append(f"N:{join_structured(n, ';', 5)}")
    if nickname:
        lines.append(f"NICKNAME:{escape_text(nickname)}")
    if lang:
        lines.append(f"LANG:{escape_text(lang)}")
    if gender:
        lines.append(f"GENDER:{join_structured(gender, ';')}")
    if email:
        lines.append(f"EMAIL:{escape_text(email)}")
    if categories:
        lines.append(f"CATEGORIES:{join_structured(categories, ',')}")
    if bday:
        lines.append(f"BDAY:{bday}")
    if anniversary:
        lines.append(f"ANNIVERSARY:{anniversary}")
    if kind:
        lines.append(f"KIND:{escape_text(kind)}")
    if adr:
        lines.append(f"ADR:{join_structured(adr, ';', 7)}")
    if tel:
        lines.append(f"TEL:{tel}")
    if impp:
        lines.append(f"IMPP:{impp}")
    if photo:
        lines.append(f"PHOTO:{photo}")
    if note:
        note_language = format_param_value(
            (lang or DEFAULT_NOTE_LANGUAGE).replace("\r", "").replace("\n", "")
        )
        lines.append(f"NOTE;LANGUAGE={note_language}:{escape_text(note)}")
    if url:
        lines.append(f"URL:{url}")
    if source:
        lines.append(f"SOURCE:{source}")
    for key in keys or []:
        try:
            alg, key_b64 = key["alg"], key["key_b64"]
        except KeyError as e:
            raise ValueError(f"Key entry is missing required field {e}") from e
        encoding = key.get("encoding") or DEFAULT_KEY_ENCODING
        lines.append(
            f"KEY;TYPE=public;ALG={alg.lower()};PREF={key.get('pref', 1)}"
            f";ENCODING={encoding}:{key_b64}"
        )

    for attribute_name, attribute_value in custom_attributes.items():
        lines.append(f"{attribute_name}:{attribute_value}")

    lines.append("END:VCARD")

    return "".join(fold_line(line) + "\r\n" for line in lines)


def build_vcard_from_raw_lines(profile: Profile) -> str:
    """
    Build a vCard string from the raw_lines stored in a Profile object.

    Args:
        profile (Profile): The Profile object containing parsed vCard data.

    Returns:
        str: The reconstructed vCard content.
    """
    # VERSION is always rewritten as 4.0 (the only version DSI supports).
    lines = ["BEGIN:VCARD", "VERSION:4.0"]

    for raw_line in profile.raw_lines:
        line = raw_line.get("line", "")
        if (
            line
            and not line.startswith("BEGIN:")
            and not line.startswith("END:")
            and not line.startswith("VERSION:")
        ):
            lines.append(line)

    lines.append("END:VCARD")
    # Lines are intentionally not folded: this keeps the stored lines byte for
    # byte (existing tests and signed text rely on it); parsers unfold anyway.
    return "".join(f"{line}\r\n" for line in lines)


def format_param_value(value: str) -> str:
    """Quote a parameter value when it contains separators (RFC 6350 section 3.3).

    Raises:
        ValueError: if the value contains a double quote or a line break, which
            cannot be represented inside a quoted parameter value.
    """
    if any(c in value for c in '"\r\n'):
        raise ValueError(f"Invalid character in parameter value: {value!r}")
    if any(c in value for c in ';:"'):
        return '"' + value + '"'
    return value


def format_property(name: str, params, value: str, group: str = None) -> str:
    """Build one unfolded property line from its parts."""
    rendered = "".join(
        f";{p_name}={format_param_value(p_value)}" for p_name, p_value in params
    )
    prefix = f"{group}." if group else ""
    return f"{prefix}{name}{rendered}:{value}"
