from dataclasses import dataclass, field
from typing import List, Optional

DSI_DATE_FORMAT = "%Y%m%dT%H%M%SZ"

REVOCATION_REASONS = {
    "compromised",
    "rotated",
    "superseded",
    "retired",
    "lost",
    "deprecated",
}


@dataclass
class PublicKey:
    alg: str
    key_b64: str
    pref: Optional[int] = None


@dataclass
class Endorsement:
    endorsee_key_b64: str
    signature_hex: str
    date: Optional[str] = None
    confidence: Optional[str] = None


@dataclass
class RevokedKey:
    key_b64: str
    reason: Optional[str] = None
    date: Optional[str] = None


@dataclass
class Feed:
    language: str
    category: str
    url: str
    tags: str = ""


@dataclass
class SocialIdentity:
    platform: str
    value: str


@dataclass
class DsiVersion:
    revision: str
    features: List[str] = field(default_factory=list)


vcard_main_attributes = {
    "fn": {"default": "", "description": "Full Name (FN)"},
    "n": {"default": "", "description": "Name (N) in the format LastName;FirstName"},
    "nickname": {"default": "", "description": "Nickname (NICKNAME)"},
    "lang": {
        "default": "en-US",
        "description": "Language (LANG) in the format 'language-region' (e.g., 'en-US', 'es-ES')",
    },
    "gender": {
        "default": "",
        "description": "Gender (GENDER), e.g., 'M' for Male, 'F' for Female, or 'O' for Other",
    },
    "email": {"default": "", "description": "Email (EMAIL), e.g., 'example@mail.com'"},
    "categories": {
        "default": "",
        "description": "Categories (comma-separated, CATEGORIES), e.g., 'gamer,programmer'",
    },
    "bday": {"default": "", "description": "Birthday (BDAY) in the format YYYY-MM-DD"},
    "anniversary": {
        "default": "",
        "description": "Anniversary date (ANNIVERSARY) in the format YYYY-MM-DD",
    },
    "kind": {
        "default": "individual",
        "description": "Type of entity (KIND), e.g., 'individual' or 'org'",
    },
    "adr": {
        "default": "",
        "description": "Address (ADR) in the format ';;Street;City;State;PostalCode;Country'",
    },
    "tel": {
        "default": "",
        "description": "Telephone number (TEL), e.g., '+1234567890'",
    },
    "impp": {
        "default": "",
        "description": "Instant messaging protocol (IMPP), e.g., 'aim:exampleuser'",
    },
    "photo": {
        "default": "",
        "description": "URL to a photo (PHOTO), e.g., 'http://example.com/photo.jpg'",
    },
    "note": {"default": "", "description": "A short description about you (NOTE)"},
    "url": {
        "default": "",
        "description": "URL to public profile or personal web (URL), e.g., 'https://my.web.example.com/profile'",
    },
    "source": {
        "default": None,
        "description": "URL where the vCard will be hosted or can found (SOURCE)",
    },
}


@dataclass
class Profile:
    fn: Optional[str] = None
    n: Optional[str] = None
    nickname: Optional[str] = None
    photo: Optional[str] = None
    lang: Optional[str] = None
    gender: Optional[str] = None
    email: Optional[str] = None
    categories: Optional[str] = None
    bday: Optional[str] = None
    anniversary: Optional[str] = None
    kind: Optional[str] = None
    adr: Optional[str] = None
    tel: Optional[str] = None
    impp: Optional[str] = None
    note: Optional[str] = None
    url: Optional[str] = None
    source: Optional[str] = None  # REQUIRED
    version: Optional[str] = None  # vCard VERSION
    dsi_version: Optional[DsiVersion] = None
    keys: List[PublicKey] = field(default_factory=list)
    endorsements: List[Endorsement] = field(default_factory=list)
    revocations: List[RevokedKey] = field(default_factory=list)
    feeds: List[Feed] = field(default_factory=list)
    social: List[SocialIdentity] = field(default_factory=list)
    errors: List[str] = field(
        default_factory=list
    )  # malformed lines found while parsing
    raw: str = ""
    raw_lines: List[dict] = field(default_factory=list)  # Store raw lines with metadata
