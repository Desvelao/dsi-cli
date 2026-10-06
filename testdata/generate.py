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
        try:
            write(f"vcards/{name}.normalized.vcf", normalize_vcard(profile))
        except ValueError as e:
            write(f"vcards/{name}.normalize_error.txt", str(e) + "\n")
        write_json(f"vcards/{name}.validate.json", validate_profile(profile).to_dict())
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
        out[name] = {"args": kwargs, "out": build_content(**kwargs)}
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
    ]
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


def gen_feeds():
    posts_dir = ROOT / "golden" / "feeds" / "posts"
    if posts_dir.exists():
        shutil.rmtree(posts_dir)
    for rel, text in POSTS.items():
        write(f"feeds/posts/{rel}", text)
    # nofront.md has no date: use a fixed mtime so the fallback is deterministic
    import os
    for p in posts_dir.rglob("nofront.md"):
        os.utime(p, (1735689600, 1735689600))  # 2025-01-01T00:00:00Z

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
    warnings = []
    xml = generate_opml_from_vcards(
        [tmp / "alice.vcf", tmp / "bob.vcf", tmp / "nofeeds.vcf"], warnings
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
            "FN:Bob & <Co>",
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
    gen_escaping()
    gen_vcards()
    gen_lifecycle()
    gen_canonical()
    gen_resolver()
    gen_feeds()
    write_versions()
    print(f"golden files written to {OUT}")


if __name__ == "__main__":
    main()
