"""Generate golden fixtures from the Python implementation (the spec for the Go port).

Run from the repo root (see `make golden`):

    PYTHONPATH=. python testdata/generate.py

Everything is deterministic: keys come from fixed seeds, dates are fixed and
no output depends on the working directory or the clock. Re-running must not
change the committed files unless the Python implementation changed.
"""

import json
import shutil
import sys
from dataclasses import asdict
from datetime import datetime, timezone
from importlib import metadata
from pathlib import Path

from cryptography.hazmat.primitives import serialization

from src.dsipy.core.canonical import (
    canonical_endorsement_string,
    canonical_feed_string,
    normalize_vcard,
)
from src.dsipy.core.lifecycle import add_key, revoke_key, rotate_key
from src.dsipy.core.resolver import normalize_url, safe_filename
from src.dsipy.core.utils import slugify
from src.dsipy.core.validator import validate_profile
from src.dsipy.crypto.keys import public_key_to_b64der
from src.dsipy.crypto.signatures import sign_endorsement, sign_feed_item
from src.dsipy.endorsements.verify import verify_endorsements
from src.dsipy.feeds.markdown import MarkdownFeed
from src.dsipy.feeds.opml import generate_opml_from_vcards
from src.dsipy.feeds.rss import RSSFeed
from src.dsipy.feeds.signing import verify_feed_items
from src.dsipy.vcard import escaping
from src.dsipy.vcard.parser import parse_vcard
from src.dsipy.vcard.serializer import (
    build_content,
    build_vcard_from_raw_lines,
    format_property,
)

ROOT = Path(__file__).resolve().parent
OUT = ROOT / "golden"
KEY_NAMES = ["alice", "bob", "carol", "dave"]

from cryptography.hazmat.primitives.asymmetric import ed25519

KEYS = {
    name: ed25519.Ed25519PrivateKey.from_private_bytes(name.encode().ljust(32, b"\0"))
    for name in KEY_NAMES
}
B64 = {name: public_key_to_b64der(k.public_key()) for name, k in KEYS.items()}


def write(rel: str, content, binary=False):
    path = OUT / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    if binary or isinstance(content, bytes):
        path.write_bytes(content)
    else:
        # newline="" keeps CRLF exactly as produced
        with open(path, "w", encoding="utf-8", newline="") as f:
            f.write(content)


def write_json(rel: str, data):
    write(rel, json.dumps(data, ensure_ascii=False, indent=2, sort_keys=True) + "\n")


def crlf(*lines):
    return "\r\n".join(lines) + "\r\n"


# ------------------------------------------------------------------ keys
def gen_keys():
    index = {}
    for name, private in KEYS.items():
        priv_pem = private.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.PKCS8,
            serialization.NoEncryption(),
        )
        pub_pem = private.public_key().public_bytes(
            serialization.Encoding.PEM,
            serialization.PublicFormat.SubjectPublicKeyInfo,
        )
        write(f"keys/{name}.priv.pem", priv_pem)
        write(f"keys/{name}.pub.pem", pub_pem)
        index[name] = {
            "seed_hex": name.encode().ljust(32, b"\0").hex(),
            "public_b64_der": B64[name],
        }
    write_json("keys/index.json", index)


# ------------------------------------------------------------------ crypto helpers
def gen_crypto():
    import base64
    from cryptography.hazmat.primitives.asymmetric import ec, rsa
    from src.dsipy.crypto.keys import (
        b64der_to_public_key,
        decode_b64_strict,
        load_private_key_pem,
        load_public_key_b64_der,
        load_public_key_pem,
    )

    inputs = [
        "", "AAAA", "AAA=", "AA==", "AAAA=", "AAAA==", "AAAA===", "A", "AAAAA", "AAAAAA",
        "AA=A", "=AAA", "=", "AAAA\n", "AA AA", "AA\nAA", "@@@@", "AAA", "AAAAAAAA=", "AB==", "AAAB=",
        "AA==AA", "AAA=AAAA", "é", "A=", "A==", "A===", "AAAAA=", "AAAAA==", "AAAAAA=", "AAAAAA==", "AAAAAA===", "AAAAAAA=", "AAA==", "AA===", "AA=", "AAA=A", "AA==A", "AAAA=A", "A=AA", "AAAAA=A", "AAAA\r", "\x00AAA", "AAA\x7f", "AAAAAA=A", "AA=\n", "AAAAAAAA", "AAAAAAAAA", "AAAAAAAAAA=", "AAAAAAAAAAA", "AAAAAAAAAA==", "AA\u00e9A", B64["alice"], B64["alice"][:-1], B64["alice"] + "A",
        base64.b64encode(b"hello").decode(),
    ]
    out = []
    for text in inputs:
        try:
            out.append({"in": text, "ok": True, "hex": decode_b64_strict(text).hex()})
        except ValueError as e:
            out.append({"in": text, "ok": False, "error": str(e)})
    write_json("crypto/decode_b64_strict.json", out)

    rsa_pub = rsa.generate_private_key(public_exponent=65537, key_size=2048).public_key()
    rsa_b64 = base64.b64encode(
        rsa_pub.public_bytes(serialization.Encoding.DER, serialization.PublicFormat.SubjectPublicKeyInfo)
    ).decode()
    loads = {"alice": B64["alice"], "garbage_der": base64.b64encode(b"hello").decode(), "rsa": rsa_b64, "empty": ""}
    res = {}
    for name, b in loads.items():
        try:
            load_public_key_b64_der(b)
            res[name] = {"in": b, "ok": True}
        except ValueError as e:
            res[name] = {"in": b, "ok": False, "error": str(e)}
    write_json("crypto/load_public_key_b64_der.json", res)
    write("crypto/rsa.pub.b64", rsa_b64)
    ec_priv = ec.generate_private_key(ec.SECP256R1()).private_bytes(
        serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()
    )
    write("crypto/ec.priv.pem", ec_priv)
    write("crypto/rsa.pub.pem", rsa_pub.public_bytes(serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo))
    errs = {}
    for name, fn, data in [
        ("private_ec", load_private_key_pem, ec_priv),
        ("private_garbage", load_private_key_pem, b"not a pem"),
        ("public_rsa", load_public_key_pem, rsa_pub.public_bytes(serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo)),
        ("public_garbage", load_public_key_pem, b"not a pem"),
    ]:
        try:
            fn(data)
            errs[name] = None
        except ValueError as e:
            errs[name] = str(e)
    write_json("crypto/pem_errors.json", errs)
    write_json("crypto/b64der_to_pem.json", {"in": B64["bob"], "out": b64der_to_public_key(B64["bob"])})


# ------------------------------------------------------------------ escaping
def gen_escaping():
    texts = [
        "plain",
        "a,b;c\\d",
        "line1\nline2",
        "line1\r\nline2",
        "emoji 😀 and ñandú",
        "",
        "trailing backslash\\",
    ]
    cases = {
        "escape_text": [{"in": t, "out": escaping.escape_text(t)} for t in texts],
        "unescape_text": [
            {"in": t, "out": escaping.unescape_text(t)}
            for t in ["a\\,b\\;c\\\\d", "x\\ny", "x\\Ny", "\\q", "no escapes", "end\\"]
        ],
        "split_structured": [
            {"in": v, "sep": s, "out": escaping.split_structured(v, s)}
            for v, s in [
                ("a;b;c", ";"),
                ("a\\;b;c", ";"),
                ("a,b\\,c,d", ","),
                ("", ";"),
                (";;", ";"),
            ]
        ],
        "fold_line": [
            {"in": t, "out": escaping.fold_line(t)}
            for t in [
                "NOTE:" + "x" * 200,
                "NOTE:" + "ñ" * 80,
                "NOTE:" + "😀" * 40,
                "SHORT:line",
                "A" * 75,
                "A" * 76,
            ]
        ],
        "slugify": [
            {"in": t, "out": slugify(t)}
            for t in ["Hello World", "Árbol ñandú", "  --x--  ", "2025/01/a.md", "日本語", ""]
        ],
    }
    for name, value in cases.items():
        write_json(f"escaping/{name}.json", value)

    props = [
        ("FN", [], "Alice", None),
        ("KEY", [("TYPE", "public"), ("PREF", "1")], "abc", None),
        ("TEL", [("TYPE", "work;home")], "123", None),
        ("ADR", [("LABEL", "a:b")], "x", "item1"),
    ]
    write_json(
        "escaping/format_property.json",
        [
            {
                "name": n,
                "params": [list(p) for p in ps],
                "value": v,
                "group": g,
                "out": format_property(n, ps, v, g),
            }
            for n, ps, v, g in props
        ],
    )


# ------------------------------------------------------------------ vCards
def dsi(*extra, source="https://alice.example/dsi.vcf", keys=True, version="4.0"):
    lines = ["BEGIN:VCARD", f"VERSION:{version}", "FN:Alice Example"]
    if source:
        lines.append(f"SOURCE:{source}")
    if keys:
        lines.append(f"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:{B64['alice']}")
    lines.extend(extra)
    lines.append("END:VCARD")
    return crlf(*lines)


def endorse(signer, endorsee, date=None, confidence=None, sig=None):
    sig = sig or sign_endorsement(KEYS[signer], B64[endorsee])
    params = [f"SIG={sig}"]
    if date:
        params.append(f"DATE={date}")
    if confidence:
        params.append(f"CONFIDENCE={confidence}")
    params.append("ENCODING=b")
    return f"X-ENDORSE;{';'.join(params)}:{B64[endorsee]}"


def vcard_cases():
    long_note = "NOTE;LANGUAGE=en-US:" + "word " * 60
    folded = escaping.fold_line(long_note)
    cases = {
        "minimal": dsi(),
        "lf_endings": dsi().replace("\r\n", "\n"),
        "full": crlf(
            "BEGIN:VCARD",
            "VERSION:4.0",
            "FN:Álice \\, Example",
            "N:Example;Alice;;;",
            "NICKNAME:Al",
            "PHOTO:https://alice.example/me.png",
            "BDAY:19900101",
            "ANNIVERSARY:20100202",
            "GENDER:F",
            "item1.ADR;LABEL=\"Main St; 1\":;;1 Main St;Town;;12345;ES",
            "TEL;TYPE=work:+34 600 000 000",
            "EMAIL:alice@alice.example",
            "IMPP:xmpp:alice@alice.example",
            "LANG:en-US",
            "KIND:individual",
            "CATEGORIES:gamer,programmer",
            "URL:https://alice.example",
            "SOURCE:https://alice.example/dsi.vcf",
            f"KEY;TYPE=public;ALG=ed25519;PREF=2;ENCODING=b:{B64['carol']}",
            f"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:{B64['alice']}",
            "X-DSI-VERSION;FEATURES=feeds,social:01",
            "X-FEED;LANGUAGE=en-US;CATEGORY=blog;TAGS=a,b:https://alice.example/feed.rss",
            "X-FEED;LANGUAGE=es-ES:https://alice.example/es.rss",
            "X-SOCIAL;PLATFORM=mastodon:@alice@example.social",
            endorse("alice", "bob", "20250101T000000Z", "high"),
            "X-CUSTOM;FOO=bar:custom value",
            "END:VCARD",
        ),
        "no_end": dsi().replace("END:VCARD\r\n", ""),
        "folded_note": dsi().replace("END:VCARD", folded + "\r\nEND:VCARD"),
        "no_source": dsi(source=None),
        "no_keys": dsi(keys=False),
        "bad_source": dsi(source="not a url"),
        "http_source": dsi(source="http://alice.example/dsi.vcf"),
        "malformed_line": dsi("this line has no colon"),
        "bad_pref": dsi(f"KEY;TYPE=public;ALG=ed25519;PREF=x;ENCODING=b:{B64['bob']}"),
        "dup_pref": dsi(f"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:{B64['bob']}"),
        "bad_key_b64": dsi("KEY;TYPE=public;ALG=ed25519;PREF=2;ENCODING=b:@@@@"),
        "wrong_alg": dsi(f"KEY;TYPE=public;ALG=rsa;PREF=2;ENCODING=b:{B64['bob']}"),
        "version_3": dsi(version="3.0"),
        "empty": "",
        "no_begin": "FN:x\r\nEND:VCARD\r\n",
        # endorsements
        "endorse_valid": dsi(endorse("alice", "bob", "20250101T000000Z", "medium")),
        "endorse_bad_sig": dsi(
            endorse("alice", "bob", sig=sign_endorsement(KEYS["carol"], B64["bob"]))
        ),
        "endorse_bad_hex": dsi(endorse("alice", "bob", sig="XYZ")),
        "endorse_no_key": dsi(
            endorse("alice", "bob"), keys=False
        ),
        "endorse_revoked_compromised": dsi(
            endorse("alice", "bob", "20240101T000000Z"),
            f"REVKEY;TYPE=public;ALG=ed25519;REASON=compromised;DATE=20250101T000000Z;ENCODING=b:{B64['alice']}",
        ),
        "endorse_revoked_before": dsi(
            endorse("alice", "bob", "20240101T000000Z"),
            f"REVKEY;TYPE=public;ALG=ed25519;REASON=rotated;DATE=20250101T000000Z;ENCODING=b:{B64['alice']}",
        ),
        "endorse_revoked_after": dsi(
            endorse("alice", "bob", "20260101T000000Z"),
            f"REVKEY;TYPE=public;ALG=ed25519;REASON=rotated;DATE=20250101T000000Z;ENCODING=b:{B64['alice']}",
        ),
        "endorse_revoked_nodate": dsi(
            endorse("alice", "bob", "20240101T000000Z"),
            f"REVKEY;TYPE=public;ALG=ed25519;REASON=retired;ENCODING=b:{B64['alice']}",
        ),
        "endorse_revoked_deprecated": dsi(
            endorse("alice", "bob", "20990101T000000Z"),
            f"REVKEY;TYPE=public;ALG=ed25519;REASON=deprecated;DATE=20250101T000000Z;ENCODING=b:{B64['alice']}",
        ),
        "two_keys_second_signs": dsi(
            f"KEY;TYPE=public;ALG=ed25519;PREF=2;ENCODING=b:{B64['carol']}",
            endorse("carol", "bob", "20250101T000000Z"),
        ),
        "complete_valid": dsi(
            f"REVKEY;TYPE=public;ALG=ed25519;REASON=rotated;DATE=20260221T110230Z;ENCODING=b:{B64['dave']}",
            "X-DSI-VERSION;FEATURES=tags,endorse:00",
            "X-FEED;LANGUAGE=en-US;TAGS=nature:https://alice.example/feed.xml",
            "X-SOCIAL;PLATFORM=github:alice",
            endorse("alice", "bob", "20260210T120000Z", "high"),
        ),
        "key_problems": dsi(
            "KEY;TYPE=public;ALG=rsa;ENCODING=b:AAAA",
            "KEY;TYPE=public;ALG=ed25519;ENCODING=b:not base64!!",
            "KEY;TYPE=public;ALG=ed25519;ENCODING=b:aGVsbG8=",
            "KEY;TYPE=public;ENCODING=b:" + B64["bob"],
            "KEY;TYPE=public;ALG=ed25519;ENCODING=b:" + B64["bob"] + "AA",
            keys=False,
        ),
        "two_preferred": dsi(f"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:{B64['bob']}"),
        "no_preferred": dsi(
            f"KEY;TYPE=public;ALG=ed25519;PREF=3;ENCODING=b:{B64['bob']}", keys=False
        ),
        "preferred_revoked": dsi(
            f"REVKEY;ALG=ed25519;REASON=compromised;DATE=20260221T110230Z;ENCODING=b:{B64['alice']}"
        ),
        "revkey_checks": dsi(
            f"REVKEY;ALG=ed25519;REASON=whatever;DATE=2026-02-21;ENCODING=b:{B64['dave']}",
            f"REVKEY;ALG=ed25519;ENCODING=b:{B64['bob']}",
            "REVKEY;ALG=ed25519;REASON=lost;DATE=20260221T110230Z;ENCODING=b:@@",
        ),
        "date_variants": dsi(
            *[
                f"REVKEY;ALG=ed25519;REASON=lost;DATE={d};ENCODING=b:{B64['dave']}"
                for d in [
                    "20250101T000000Z", "2025011T000000Z", "202511T000000Z", "20250101t000000z",
                    "20250230T000000Z", "20250101T000060Z", "20250101T000059Z", "20250101T240000Z",
                    "00000101T000000Z", "20250101T0000Z", "20250101T1:0:0Z", "20250101T000000",
                    " 20250101T000000Z", "20250101T000000Z ", "20240229T000000Z", "20250229T000000Z",
                ]
            ]
        ),
        "endorse_format_errors": dsi(
            f"X-ENDORSE;SIG=ABCD;DATE=today;CONFIDENCE=huge;ENCODING=b:{B64['bob']}"
        ),
        "endorse_zero_sig": dsi(
            f"X-ENDORSE;SIG={'0' * 128};DATE=20260210T120000Z;ENCODING=b:{B64['bob']}"
        ),
        "endorse_duplicate": dsi(
            endorse("alice", "bob", "20260210T120000Z"),
            endorse("alice", "bob", "20260210T120000Z"),
        ),
        "endorse_unverifiable": dsi(
            f"X-ENDORSE;SIG={'a' * 128};ENCODING=b:{B64['bob']}", keys=False
        ),
        "endorse_signed_by_other": dsi(endorse("dave", "bob")),
        "endorse_bad_endorsee": dsi(
            f"X-ENDORSE;SIG={'a' * 128};ENCODING=b:@@@"
        ),
        "endorse_odd_hex": dsi(endorse("alice", "bob", sig="abc")),
        "feed_checks": dsi(
            "X-FEED:feed.xml",
            "X-FEED;LANGUAGE=not_a_tag:https://a.example/f.xml",
            "X-FEED;TAGS=a b:https://a.example/g.xml",
            "X-FEED:ftp://a.example/h.xml",
            "X-FEED:http://a.example/i.xml",
            "X-FEED;LANGUAGE=en-US-x-private1:https://a.example/j.xml",
            "X-FEED;LANGUAGE=e:https://a.example/k.xml",
        ),
        "url_variants": dsi(
            "X-FEED:HTTP://A.example/x",
            "X-FEED:https://",
            "X-FEED:https:///path",
            "X-FEED:https://[::1]/x",
            "X-FEED:https://[v1.x]/x",
            "X-FEED://a.example/x",
            "X-FEED:mailto:a@b.example",
            "X-FEED:https://a b.example/",
            "X-FEED:  https://a.example/trim",
            "X-FEED:https://a.example:abc/x",
            "X-FEED:1http://a.example/",
            "X-FEED:ht+tp-x.y://a.example/",
            "URL:data:text/plain;base64,AAAA",
            "URL:relative/path",
            "PHOTO:http://a.example/p.png",
        ),
        # Python's validator raises ValueError (crashes) on these; the Go port reports them as invalid URLs
        "url_crash_unclosed_bracket": dsi("X-FEED:https://[::1/x"),
        "url_crash_ipv4_in_brackets": dsi("X-FEED:https://[1.2.3.4]/x"),
        "social_checks": dsi(
            "X-SOCIAL;PLATFORM=GitHub1:alice",
            "X-SOCIAL;PLATFORM=x:",
            "X-SOCIAL;PLATFORM=x:dup",
            "X-SOCIAL;PLATFORM=x:dup",
        ),
        "dsi_version_checks": dsi(
            "X-DSI-VERSION;FEATURES=a b,ok,:0", "X-DSI-VERSION:01"
        ),
        "duplicates": dsi(
            f"KEY;TYPE=public;ALG=ed25519;PREF=2;ENCODING=b:{B64['alice']}",
            "SOURCE:https://alice.example/dsi.vcf",
            "VERSION:4.0",
            "X-FEED:https://a.example/f",
            "X-FEED:https://a.example/f",
            "X-FEED:https://a.example/g",
            "X-FEED:https://a.example/g",
        ),
        "validator_mix": dsi(
            "X-FEED;LANGUAGE=en-US:ftp://bad/feed",
            "X-FEED:https://a.example/f.rss",
            "X-FEED:https://a.example/f.rss",
            "X-SOCIAL;PLATFORM=Mastodon:@x",
            "X-DSI-VERSION:1",
            "EMAIL:not-an-email",
            "BDAY:yesterday",
            "LANG:xx_YY",
            "KIND:robot",
        ),
    }
    return cases


def gen_vcards():
    cases = vcard_cases()
    for name, text in cases.items():
        write(f"vcards/{name}.vcf", text)
        profile = parse_vcard(text)
        write_json(f"vcards/{name}.parse.json", asdict(profile))
        # exact `json.dumps(asdict(profile), ensure_ascii=False)` as printed by `vcard parse`
        write(f"vcards/{name}.parse.pyjson", json.dumps(asdict(profile), ensure_ascii=False))
        try:
            write(f"vcards/{name}.normalized.vcf", normalize_vcard(profile))
        except ValueError as e:
            write(f"vcards/{name}.normalize_error.txt", str(e) + "\n")
        try:
            write_json(f"vcards/{name}.validate.json", validate_profile(profile).to_dict())
            write(f"vcards/{name}.validate.pyjson", json.dumps(validate_profile(profile).to_dict(), ensure_ascii=False))
        except ValueError as e:
            write_json(f"vcards/{name}.validate.json", {"python_error": str(e)})
        write_json(
            f"vcards/{name}.verify.json",
            [
                {
                    "endorsee": r.endorsement.endorsee_key_b64,
                    "status": r.status,
                    "signer": r.signer_key_b64,
                    "reason": r.reason,
                }
                for r in verify_endorsements(profile)
            ],
        )
        write(f"vcards/{name}.rebuilt.vcf", build_vcard_from_raw_lines(profile))

    # build_content (vcard create)
    builds = {
        "basic": dict(fn="Alice", source="https://alice.example/dsi.vcf"),
        "everything": dict(
            fn="Alice, \"A\" Example",
            n="Example;Alice",
            nickname="Al",
            lang="en-US",
            gender="F",
            email="a@alice.example",
            categories="x,y",
            bday="19900101",
            anniversary="20100101",
            kind="individual",
            adr=";;1 Main St;Town;;12345;ES",
            tel="+34 600",
            impp="xmpp:a@x",
            photo="https://alice.example/p.png",
            note="multi\nline; note, with specials \\",
            url="https://alice.example",
            source="https://alice.example/dsi.vcf",
            custom_attributes={"X-SOCIAL;PLATFORM=mastodon": "@a@b", "FEED=": "x"},
            keys=[
                {"alg": "ed25519", "key_b64": B64["alice"], "pref": 1},
                {"alg": "ed25519", "key_b64": B64["bob"]},
            ],
        ),
        "long_note": dict(fn="A", note="word " * 80, source="https://a.example/x"),
    }
    out = {}
    for name, kwargs in builds.items():
        args = dict(kwargs)
        if "custom_attributes" in args:  # ordered pairs: JSON objects lose order here
            args["custom_attributes"] = [list(kv) for kv in args["custom_attributes"].items()]
        out[name] = {"args": args, "out": build_content(**kwargs)}
    errors = {
        "break_in_tel": dict(tel="1\nFN:evil"),
        "break_in_url": dict(url="https://e.com\r\nX-EVIL:1"),
        "break_in_custom": dict(custom_attributes={"X-A": "v\r\nFN:evil"}),
        "key_missing_alg": dict(keys=[{"key_b64": B64["alice"]}]),
        "key_missing_b64": dict(keys=[{"alg": "ed25519"}]),
        "quote_in_lang": dict(lang='en"US', note="x"),
    }
    err_out = {}
    for name, kwargs in errors.items():
        try:
            build_content(**kwargs)
            err_out[name] = {"error": None}
        except ValueError as e:
            err_out[name] = {"error": str(e)}
    out["__errors__"] = err_out
    write_json("vcards/build_content.json", out)


# ------------------------------------------------------------------ lifecycle
def gen_lifecycle():
    when = datetime(2025, 3, 4, 5, 6, 7, tzinfo=timezone.utc)
    base = dsi()
    two = dsi(f"KEY;TYPE=public;ALG=ed25519;PREF=2;ENCODING=b:{B64['carol']}")
    nokey = dsi(keys=False)
    results = {}

    def run(name, fn):
        try:
            results[name] = {"ok": True, "out": fn()}
        except ValueError as e:
            results[name] = {"ok": False, "error": str(e)}

    run("revoke_preferred", lambda: revoke_key(base, B64["alice"], "lost", when))
    run("revoke_second", lambda: revoke_key(two, B64["carol"], "compromised", when))
    run("revoke_unknown_key", lambda: revoke_key(base, B64["bob"], "lost", when))
    run("revoke_bad_reason", lambda: revoke_key(base, B64["alice"], "bored", when))
    run("rotate_default_old", lambda: rotate_key(base, B64["bob"], None, "rotated", when))
    run("rotate_superseded", lambda: rotate_key(two, B64["bob"], B64["carol"], "superseded", when))
    run("rotate_bad_reason", lambda: rotate_key(base, B64["bob"], None, "lost", when))
    run("rotate_ambiguous", lambda: rotate_key(two.replace("PREF=2", "PREF=1"), B64["bob"], None, "rotated", when))
    run("rotate_existing_new", lambda: rotate_key(base, B64["alice"], None, "rotated", when))
    run("add_preferred", lambda: add_key(base, B64["bob"], True))
    run("add_not_preferred", lambda: add_key(base, B64["bob"], False))
    run("add_to_empty", lambda: add_key(nokey, B64["bob"], True))
    run("add_existing", lambda: add_key(base, B64["alice"], True))
    run("add_to_malformed", lambda: add_key(dsi("no colon here"), B64["bob"], True))
    run("add_invalid_key", lambda: add_key(base, "@@@", True))
    write_json("lifecycle/cases.json", results)
    write("lifecycle/base.vcf", base)
    write("lifecycle/two_keys.vcf", two)
    write("lifecycle/no_key.vcf", nokey)


# ------------------------------------------------------------------ canonical + signatures
def gen_canonical():
    endorse_cases = [
        {"signer": s, "endorsee": e, "canonical": canonical_endorsement_string(B64[e]).decode(),
         "signature_hex": sign_endorsement(KEYS[s], B64[e])}
        for s, e in [("alice", "bob"), ("bob", "alice"), ("carol", "dave")]
    ]
    feed_cases = []
    for s, pub, title, desc in [
        ("alice", "Wed, 01 Jan 2025 10:00:00 GMT", "Hello", "Plain body"),
        ("alice", "Wed, 01 Jan 2025 10:00:00 GMT", "", "No title"),
        ("bob", "Thu, 02 Jan 2025 11:30:00 GMT", "Ünïcode ☃", "<p>html</p>\n"),
    ]:
        feed_cases.append(
            {
                "signer": s,
                "pub_date": pub,
                "title": title,
                "description": desc,
                "canonical": canonical_feed_string(pub, title, desc).decode(),
                "signature_hex": sign_feed_item(KEYS[s], pub, title, desc),
            }
        )
    write_json("canonical/endorsements.json", endorse_cases)
    write_json("canonical/feed_items.json", feed_cases)


# ------------------------------------------------------------------ resolver helpers
def gen_resolver():
    urls = [
        "HTTPS://Alice.Example:443/dsi.vcf#frag",
        "https://alice.example/a/./b/../c.vcf",
        "http://alice.example:80/",
        "https://alice.example",
        "https://alice.example./x",
        "https://alice.example/%7euser/%2f",
        "https://alice.example:8443/x?q=1",
        "https://alice.example:abc/x",
        "  https://alice.example/x  ",
        "https://e.com/%7Ealice", "https://e.com/a%2fb", "https://e.com/a?x=%2f", "https://e.com/a/b/..",
        "https://e.com/../a", "https://e.com:99999/a", "https://e.com:0/a", "https://e.com:00443/a",
        "HTTP://[::1]:443/", "http://[2001:DB8::1]:80/p", "https://user:pw@E.com/x", "https://e.com?x=1",
        "https://e.com/%zz", "https://e.com/a%41b%7e", "https:e.com", "e.com/path", "", "https://",
        "ftp://e.com:21/x", "mailto:a@b.example", "https://e.com/a//b/./c/", "https://e.com/.", "https://e.com/..",
        "https://e.com/a/../../b", "https://e.com/a/.", "https://E.COM./X?Q=%7e#f", "//e.com/x",
        "https://e.com:/x", "https://[::1]/x", "https://e.com/%E2%82%AC", "https://e.com/€",
        "https://exämple.com/", "http://e.com:80", "https://e.com/?", "https://e.com/#",
    ]
    out = []
    for u in urls:
        try:
            out.append({"in": u, "out": normalize_url(u)})
        except ValueError as e:
            out.append({"in": u, "error": str(e)})
    write_json("resolver/normalize_url.json", out)
    names = [
        ("", "https://a.example/x/card.vcf"),
        ('attachment; filename="me.vcf"', "https://a.example/x"),
        ('attachment; filename="../../etc/passwd"', "https://a.example/x"),
        ('attachment; filename="..\\\\evil.vcf"', "https://a.example/x"),
        ('attachment; filename=".hidden"', "https://a.example/x/ok.vcf"),
        ("", "https://a.example/"),
        ("", "https://a.example/.."),
        ('attachment; filename=plain.vcf', "https://a.example/x"),
        ('attachment; filename="with space.vcf"', "https://a.example/x"),
        ("attachment; filename*=UTF-8''caf%C3%A9.vcf", "https://a.example/x"),
        ("attachment; filename*=UTF-8''..%2F..%2Fevil.vcf", "https://a.example/x"),
        ('inline', "https://a.example/path/card.vcf?x=1"),
        ('attachment; filename=""', "https://a.example/path/card.vcf"),
        ('attachment; name="n.vcf"', "https://a.example/path/card.vcf"),
        ('attachment; filename="a/b/c.vcf"', "https://a.example/x"),
        ('attachment; filename="C:\\Users\\me\\card.vcf"', "https://a.example/x"),
        ("", "https://a.example/.hidden"),
        ("", "https://a.example/dir/"),
        ("", "https://a.example/a%20b.vcf"),
        ('attachment; filename=.', "https://a.example/ok.vcf"),
        ('attachment; filename="x.vcf"; filename="y.vcf"', "https://a.example/z"),
    ]
    import ipaddress
    from src.dsipy.core.http import _validate_url
    from urllib.parse import urljoin
    ips = [
        "8.8.8.8", "1.1.1.1", "93.184.216.34", "0.0.0.0", "0.255.255.255", "10.0.0.1", "10.255.255.255", "100.64.0.1",
        "100.127.255.255", "100.128.0.1", "127.0.0.1", "127.255.255.255", "169.254.169.254", "172.15.255.255",
        "172.16.0.1", "172.31.255.255", "172.32.0.1", "192.0.0.1", "192.0.0.9", "192.0.0.10", "192.0.0.11", "192.0.0.170",
        "192.0.0.171", "192.0.2.1", "192.88.99.1", "192.168.0.1", "198.17.255.255", "198.18.0.1", "198.19.255.255",
        "198.20.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "239.255.255.255", "240.0.0.1", "255.255.255.255",
        "::", "::1", "::2", "::ffff:8.8.8.8", "::ffff:10.0.0.1", "::ffff:127.0.0.1", "::ffff:100.64.0.1",
        "64:ff9b::808:808", "64:ff9b:1::1", "100::1", "100:0:0:1::1", "2001::1", "2001:1::1", "2001:1::2", "2001:1::3",
        "2001:3::1", "2001:4:112::1", "2001:20::1", "2001:30::1", "2001:db8::1", "2001:4860:4860::8888", "2002::1",
        "2606:4700:4700::1111", "3fff::1", "3ffe::1", "fc00::1", "fd12:3456::1", "fe80::1", "fec0::1", "ff02::1",
        "2a00:1450:4009::200e", "::ffff:0:0", "::127.0.0.1", "fe80::1%eth0",
    ]
    write_json(
        "resolver/is_global.json",
        [{"ip": ip, "global": ipaddress.ip_address(ip.split("%")[0]).is_global} for ip in ips],
    )
    joins = [
        ("https://example.com/a/b", loc)
        for loc in ["/x", "c", "../c", "./c", "//other.example/x", "https://x.example/y", "?q=1", "#frag", "", "http://x.example",
                    "../../../c", "c/d?e#f", "/x/../y", "ftp://x/y", "javascript:alert(1)", "//x", "x:y", "/\\evil.example"]
    ]
    write_json("resolver/urljoin.json", [{"base": b, "location": l, "out": urljoin(b, l)} for b, l in joins])
    urls = ["https://e.com/a", "http://e.com/a", "ftp://e.com/a", "file:///etc/passwd", "https:///a", "https://u:p@e.com/a",
            "https://u@e.com/a", "https://:p@e.com/a", "https://e.com:0/a", "https://e.com:8443/a", "https://e.com:abc/a",
            "https://[::1]:8443/a", "HTTPS://E.com/a", "https://", "", "//e.com/a", "e.com", "https://e.com:99999/",
            "https://[::1/a"]
    vres = []
    for allow in (False, True):
        for u in urls:
            try:
                h, port = _validate_url(u, allow)
                vres.append({"url": u, "allow_http": allow, "host": h, "port": port})
            except ValueError as e:
                vres.append({"url": u, "allow_http": allow, "error": str(e)})
    write_json("resolver/validate_url.json", vres)
    bodies = [b"ok", "caf\u00e9".encode(), b"\xff", b"abc\xff", b"\xc3", b"\xc3\x28", b"\xe2\x82", b"\xe2\x82\x28", b"\xe2", b"a\xe2\x28\xa1",
              b"\xf0\x9f\x98", b"\xf0\x28\x8c\xbc", b"\xed\xa0\x80", b"\xc0\x80", b"\xf5\x80\x80\x80", b"\x80", b"\xe0\x80\x80",
              b"\xf4\x90\x80\x80", b"\xe2\x82\xac\xe2\x82"]
    ures = []
    for b in bodies:
        try:
            b.decode("utf-8"); ures.append({"hex": b.hex(), "ok": True})
        except UnicodeDecodeError as e:
            ures.append({"hex": b.hex(), "ok": False, "error": str(e)})
    write_json("resolver/utf8_errors.json", ures)
    write_json(
        "resolver/safe_filename.json",
        [{"content_disposition": cd, "url": u, "out": safe_filename(cd, u)} for cd, u in names],
    )


# ------------------------------------------------------------------ feeds
POSTS = {
    "hello.md": "---\ntitle: Hello World\ndate: 2025-01-01\n---\nFirst post body.\n",
    "html.md": (
        "---\ntitle: HTML Post\ndate: 2025-01-03T10:00:00Z\nuse_html_content: true\n"
        "image: https://a.example/pic.png\nlink: https://a.example/html\n---\n"
        "# Heading\n\nSome *emphasis*, **strong**, `code` and a [link](https://x.example).\n\n"
        "| a | b |\n|---|---|\n| 1 | 2 |\n\n"
        "- item 1\n- item 2\n\nTerm\n:   Definition\n\n```\nfenced\n```\n"
    ),
    "nested/deep post.md": (
        "---\ntitle: \"Quoted: Title\"\ndate: 2025-01-02T12:30:00+02:00\ncustom: value\n---\n"
        "Body with {{ custom }} and {{ site }} and {{ missing }}.\n"
    ),
    "nofront.md": "Just text, no front matter.\n",
    "explicit_id.md": "---\nid: my-id\ntitle: Explicit\ndate: 2024-12-31 23:59:59\nuse_html_content: yes\n---\nA & B < C\n",
}
BAD_POSTS = {
    "unterminated.md": "---\ntitle: x\n",
    "baddate.md": "---\ntitle: x\ndate: not-a-date\n---\nbody\n",
}


def state_to_json(state, root):
    s = dict(state)
    s["date"] = state["date"].isoformat()
    meta = dict(state["metadata"])
    for k in ("file_path", "file_dir"):
        meta.pop(k, None)
    s["metadata"] = meta
    return s


def gen_qr():
    import qrcode
    from PIL import Image
    texts = [
        "A", "12345", "HELLO WORLD", "hello", "https://example.com/vcard", "https://alice.example/dsi.vcf",
        "BEGIN:VCARD\nVERSION:4.0\nFN:Alice\nEND:VCARD", "caf\u00e9 \u2603 \u65e5\u672c\u8a9e", "x" * 120, "1234567890" * 20,
        "ABC123abc 456 DEF", "https://example.com/" + "a" * 300, "A" * 400, "\u2603" * 60,
    ]
    out = []
    for text in texts:
        q = qrcode.QRCode(error_correction=qrcode.constants.ERROR_CORRECT_H)
        q.add_data(text)
        q.make()
        matrix = q.get_matrix()  # includes the 4-module border
        out.append({
            "data": text, "version": q.version, "mask": q.mask_pattern, "border": q.border, "box_size": q.box_size,
            "rows": ["".join("1" if c else "0" for c in row) for row in matrix],
        })
    write_json("qr/matrices.json", out)
    # image size of generate_qr's default rendering
    img = q.make_image(fill_color="Black", back_color="white").convert("RGB")
    write_json("qr/image.json", {"size": list(img.size), "mode": img.mode})


def gen_feed_primitives():
    import markdown as md
    from src.dsipy.feeds.markdown import _parse_date, _unquote
    dates = [
        "2025-01-01", "2025-01-01T10:00:00", "2025-01-01 10:00:00", "2025-01-01T10:00:00Z", "2025-01-01T10:00:00+02:00",
        "2025-01-01T10:00:00-0530", "2025-01-01T10:00:00+05", "2025-01-01T10:00", "2025-01-01T10", "2025-01-01T10:00:00.5",
        "2025-01-01T10:00:00.123456", "2025-01-01T10:00:00.1234567", "2025-01-01T10:00:00,5", "20250101", "20250101T100000",
        "20250101T1000", "2025-W01-1", "2025W011", "2025-W01", "2025-W53-1", "2025-01-01X10:00", "2025-01-01t10:00:00",
        "2025-1-1", "2025-13-01", "2025-02-30", "2024-02-29", "2025-01-01T25:00", "2025-01-01T24:00:00", "2025-01-01T10:60",
        "2025-01-01T10:00:60", "  2025-01-01  ", "", "today", "2025", "2025-01", "2025-01-01T", "2025-01-01T10:00:00+24:00",
        "2025-01-01T10:00:00+05:30:15", "2025-01-01T10:00:00Z ", "0001-01-01T00:00:00+01:00", "9999-12-31T23:59:59-01:00",
        "2025-01-01T10:00:00+00:00", "2025-01-01 10:00:00 +02:00", "2025-01-01T10:00:00z", "٢٠٢٥-٠١-٠١", "2025-01-01T10:00:00.Z",
        "2025-01-01T10:00:00.1Z", "2025-01-01T1000", "2025-01-01T100000", "2025-01-01T10:0000", "20250101T10:00:00",
    ]
    out = []
    for d in dates:
        try:
            out.append({"in": d, "out": _parse_date(d).isoformat()})
        except (ValueError, OverflowError) as e:  # OverflowError is an uncaught crash in Python
            out.append({"in": d, "error": True})
    import random
    rnd = random.Random(20250101)
    dts = ["2025-01-01", "20250101", "2025-W01-1", "2025W011", "2025-W01", "2025W01", "2025-W53-1", "2020-W53-7", "2025-02-29",
           "2024-02-29", "0001-01-01", "9999-12-31", "2025-1-01", "2025-01-1", "2025-W00-1", "2025-W01-0", "2025-W01-8", "2025-W5-1", "2025-001"]
    seps = ["T", " ", "t", "_", "\u00e9", "T "]
    times = ["", "10", "10:00", "10:00:00", "1000", "100000", "10:0000", "1000:00", "10:00:00.5", "10:00:00.123456789", "10:00:00,5",
             "10:00:00.", "24:00", "23:59:59", "00:00:00", "10:00:00x", "10:60", "1", "10:", "10:00:", "10-00", "10:00:00.1234", "10:00.5"]
    tzs = ["", "Z", "z", "+02:00", "-02:00", "+0200", "+02", "+2", "+24:00", "+23:59", "+05:30:15", "+05:30:15.5", " +02:00", "-00:00",
           "+", "+0200Z", "Z ", "+02:0", "+02:00:", "+ab"]
    fuzz = set()
    while len(fuzz) < 1800:
        fuzz.add(rnd.choice(dts) + rnd.choice(seps) + rnd.choice(times) + rnd.choice(tzs))
    for d in sorted(fuzz):
        try:
            out.append({"in": d, "out": _parse_date(d).isoformat()})
        except (ValueError, OverflowError):
            out.append({"in": d, "error": True})
    write_json("feeds/iso_dates.json", out)

    snippets = [
        "plain text", "# Heading\n\ntext", "Setext\n======\n\ntext", "## H2 ##\n", "*em* **strong** ***both*** `code` ~~strike~~",
        "line one  \nline two", "line one\nline two", "> quote\n> more\n\n> second", "- a\n- b\n    - nested\n- c", "1. one\n2. two\n10. ten",
        "* a\n\n* b (loose)", "[link](https://x.example \"title\") and ![img](https://x.example/i.png \"t\")", "<https://auto.example> and <me@x.example>",
        "<div>raw <b>html</b></div>\n\ntext", "inline <span class=\"x\">html</span> ok", "A & B < C > D &copy; &amp; &#169;", "```python\nprint('hi')\n```",
        "    indented code\n    more", "~~~\ntilde fence\n~~~", "---\n\n***\n\n___", "| a | b |\n|---|:-:|\n| 1 | 2 |\n| 3 | 4 |",
        "Term\n:   Definition\n:   Another\n\nTerm 2\n:   Def 2", "Footnote ref[^1].\n\n[^1]: The note.", "*[HTML]: Hyper Text Markup Language\n\nThe HTML spec",
        "## Heading {#custom-id .cls}\n", "para {: .cls }", "<div markdown=\"1\">\n*inside*\n</div>", "escaped \\*not em\\* and \\# not heading",
        "http://bare.example/url not autolinked", "[ref link][1]\n\n[1]: https://ref.example \"Ref\"", "Unicode: café ☃ 日本語 😀", "Tab\there", "", "   \n\n", "a\r\nb\r\n\r\nc",
        "<!-- comment -->\n\ntext", "1) paren list\n2) two", "+ plus list\n+ two", "Hard  \nbreak\\\nbackslash", "## \n", "#NoSpace", "text\n- list right after",
        "![alt *em*](x.png)", "[empty]()", "`` `tick` ``", "**unclosed", "_under_ and __dunder__ and snake_case_word", "5 * 3 * 2", "a<b and c>d",
    ]
    write_json(
        "feeds/markdown_cases.json",
        [{"in": t, "out": md.markdown(t, extensions=["extra"])} for t in snippets],
    )
    write_json("feeds/unquote.json", [{"in": v, "out": _unquote(v)} for v in ["'a'", '"a"', "\"a'", "'", "\"\"", "a", "''", "'a b' c", " 'a' "]])


def gen_feeds():
    posts_dir = ROOT / "golden" / "feeds" / "posts"
    if posts_dir.exists():
        shutil.rmtree(posts_dir)
    for rel, text in POSTS.items():
        write(f"feeds/posts/{rel}", text)
    # nofront.md has no date: use a fixed mtime so the fallback is deterministic
    import os
    for p in posts_dir.rglob("nofront.md"):
        os.utime(p, (1735689601, 1735689601))  # 2025-01-01T00:00:01Z (distinct from hello.md: ties depend on directory order)

    states = MarkdownFeed.collect(str(posts_dir))
    write_json(
        "feeds/collect.json", [state_to_json(s, posts_dir) for s in states]
    )

    bad = {}
    for rel, text in BAD_POSTS.items():
        p = ROOT / "golden" / "feeds" / "bad" / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(text, encoding="utf-8")
        try:
            MarkdownFeed._parse_file(p, p.parent)
            bad[rel] = "no error"
        except ValueError as e:
            bad[rel] = str(e).replace(str(p), rel)
    write_json("feeds/bad_posts.json", bad)

    # templating + CDATA wrapping, exactly as `feeds build` does it
    from src.dsipy.cli.feeds import _apply_templates

    vars_dict = {"site": "https://a.example", "custom": "overridden"}
    items = [dict(s, metadata=dict(s["metadata"])) for s in states]
    _apply_templates(items, vars_dict)

    build_date = datetime(2025, 2, 3, 4, 5, 6)
    args = dict(
        title="Alice's Feed",
        link="https://alice.example",
        description="A <test> feed & more",
        author_name="Alice",
        author_email="alice@alice.example",
        language="en-US",
        build_date=build_date,
        items=items,
    )
    unsigned = RSSFeed.build(**args)
    write("feeds/feed_unsigned.rss", unsigned)
    signed = RSSFeed.build(**args, sign={"key": KEYS["alice"], "id": B64["alice"]})
    write("feeds/feed_signed.rss", signed)
    write_json(
        "feeds/feed_inputs.json",
        {
            "vars": vars_dict,
            "build_date": build_date.isoformat(),
            "signer": "alice",
            "items_after_templating": [state_to_json(i, posts_dir) for i in items],
        },
    )

    def fmt(results):
        return [
            {"title": r.title, "guid": r.guid, "status": r.status, "reason": r.reason}
            for r in results
        ]

    keys = {B64["alice"]: KEYS["alice"].public_key()}
    write_json("feeds/verify_signed_alice.json", fmt(verify_feed_items(signed, keys)))
    write_json(
        "feeds/verify_signed_wrong_key.json",
        fmt(verify_feed_items(signed, {B64["alice"]: KEYS["bob"].public_key()})),
    )
    write_json("feeds/verify_signed_nokeys.json", fmt(verify_feed_items(signed, {})))
    write_json("feeds/verify_unsigned.json", fmt(verify_feed_items(unsigned, keys)))
    tampered = signed.replace("Hello World", "Hello Mallory", 1)
    write("feeds/feed_tampered.rss", tampered)
    write_json("feeds/verify_tampered.json", fmt(verify_feed_items(tampered, keys)))

    # empty feed
    write(
        "feeds/feed_empty.rss",
        RSSFeed.build(**{**args, "items": []}),
    )

    # OPML
    tmp = ROOT / "golden" / "opml" / "in"
    tmp.mkdir(parents=True, exist_ok=True)
    (tmp / "alice.vcf").write_text(cases_for_opml()["alice"], encoding="utf-8", newline="")
    (tmp / "bob.vcf").write_text(cases_for_opml()["bob"], encoding="utf-8", newline="")
    (tmp / "nofeeds.vcf").write_text(dsi(), encoding="utf-8", newline="")
    (tmp / "carol.vcf").write_text(
        crlf("BEGIN:VCARD", "VERSION:4.0", "FN:Carol\\nTwo \\\\ \u2028 \x1f", "SOURCE:https://c.example/d.vcf",
             "X-FEED;CATEGORY= a , ,b ;TAGS=b,c:https://c.example/f.rss?a=1&b=2", "X-FEED:", "END:VCARD"),
        encoding="utf-8", newline="")
    warnings = []
    xml = generate_opml_from_vcards(
        [tmp / "alice.vcf", tmp / "bob.vcf", tmp / "nofeeds.vcf", tmp / "carol.vcf", tmp / "missing.vcf"], warnings
    )
    write("opml/feeds.opml", xml)
    write_json("opml/warnings.json", warnings)


def cases_for_opml():
    return {
        "alice": dsi(
            "X-FEED;LANGUAGE=en-US;CATEGORY=blog;TAGS=a,b:https://alice.example/feed.rss",
            "X-FEED:https://alice.example/other.rss",
        ),
        "bob": crlf(
            "BEGIN:VCARD",
            "VERSION:4.0",
            "FN:Bob & <Co> \"B\" 'x' é\ttab",
            "SOURCE:https://bob.example/dsi.vcf",
            "X-FEED;CATEGORY=news,blog;TAGS=blog,tech:https://bob.example/f.xml",
            "END:VCARD",
        ),
    }


def write_versions():
    pkgs = ["rfeed", "opyml", "markdown", "vobject", "cryptography", "pillow", "qrcode"]
    lines = [f"python {sys.version.split()[0]}"]
    for p in pkgs:
        try:
            lines.append(f"{p}=={metadata.version(p)}")
        except metadata.PackageNotFoundError:
            lines.append(f"{p}: not installed")
    write("VERSIONS.txt", "\n".join(lines) + "\n")


def main():
    if OUT.exists():
        shutil.rmtree(OUT)
    OUT.mkdir(parents=True)
    gen_keys()
    gen_crypto()
    gen_escaping()
    gen_vcards()
    gen_lifecycle()
    gen_canonical()
    gen_resolver()
    gen_qr()
    gen_feed_primitives()
    gen_feeds()
    write_versions()
    print(f"golden files written to {OUT}")


if __name__ == "__main__":
    main()
