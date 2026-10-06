"""Differential parity check: run the same command lines through the Python
implementation and the Go binary and compare exit codes, stdout, stderr and the
files they leave behind. Run it with `make parity` (inside the `py` container).

Scenario = fixture files + steps. Each step runs on both implementations in
separate scratch directories. "Cross" scenarios run steps alternately on one
implementation or the other in a single directory (artifacts made by one are
checked by the other).

Differences that are intentional are listed in EXPECTED with the reason; any
other difference fails the run.
"""

import json
import os
import re
import shutil
import struct
import subprocess
import sys
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
GOLDEN = REPO / "testdata" / "golden"
GO_BIN = REPO / "bin" / "dsi"
PY_CODE = f"import sys; sys.path.insert(0, {str(REPO)!r}); from src.dsipy.cli.app import main_app; main_app()"

KEYS = json.loads((GOLDEN / "keys" / "index.json").read_text())
B64 = {name: info["public_b64_der"] for name, info in KEYS.items()}


def key_files(*names):
    files = {}
    for n in names:
        files[f"{n}.pem"] = (GOLDEN / "keys" / f"{n}.priv.pem").read_text()
        files[f"{n}.pub"] = (GOLDEN / "keys" / f"{n}.pub.pem").read_text()
    return files


def vcf(name, source, key=None, *extra, crlf=True):
    lines = ["BEGIN:VCARD", "VERSION:4.0", f"FN:{name}"]
    if source:
        lines.append(f"SOURCE:{source}")
    if key:
        lines.append(f"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:{B64[key]}")
    lines += list(extra) + ["END:VCARD"]
    return ("\r\n" if crlf else "\n").join(lines) + ("\r\n" if crlf else "\n")


def post(title, date, body, extra=""):
    return f"---\ntitle: {title}\ndate: {date}\n{extra}---\n{body}\n"


BASE_FILES = {
    **key_files("alice", "bob", "carol"),
    "alice.vcf": vcf("Alice", "https://alice.example/dsi.vcf", "alice", "X-FEED;LANGUAGE=en-US:https://alice.example/feed.rss",
                     "X-SOCIAL;PLATFORM=github:alice"),
    "bob.vcf": vcf("Bob", "https://bob.example/dsi.vcf", "bob"),
    "lf.vcf": vcf("Lf", "https://lf.example/dsi.vcf", "carol", crlf=False),
    "nokey.vcf": vcf("NoKey", "https://n.example/n.vcf"),
    "bad.vcf": "BEGIN:VCARD\r\nEND:VCARD\r\n",
    "malformed.vcf": "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:M\r\nnot a property\r\nEND:VCARD\r\n",
    "feeds/a.md": post("Post a", "2024-01-01T00:00:00Z", "Body a"),
    "feeds/b.md": post("Post b", "2024-01-02T00:00:00Z", "Body b"),
    "feeds/c.md": post("Post c {{ who }}", "2024-01-03T00:00:00Z", "Some **bold** {{ site }}", "use_html_content: true\n"),
    "vars.env": "# vars\nsite = https://file.example\nwho=file\n",
    "cards/one.vcf": vcf("One", "https://one.example/o.vcf", "alice", "X-FEED;CATEGORY=blog;TAGS=a,b:https://one.example/f.rss"),
    "cards/two.vcard": vcf("Two & <Co>", "https://two.example/t.vcf", None, "X-FEED:https://two.example/f.xml"),
}
FEED_OPTS = ["--title", "T", "--link", "https://example.com/feed.rss", "--description", "D", "--author", "A", "--email", "a@example.com"]


def S(name, files, *steps, cross=False):
    return {"name": name, "files": files, "steps": [s if isinstance(s, dict) else {"args": s, "stdin": None, "impl": None, "help": False} for s in steps], "cross": cross}


def step(args, stdin=None, impl=None, help=False):
    return {"args": args, "stdin": stdin, "impl": impl, "help": help}


# ----------------------------------------------------------------------------- scenarios
SCENARIOS = []
add = SCENARIOS.append

# key
add(S("key create / refuse / force", {}, ["key", "create"], ["key", "create"], ["key", "create", "--force"],
      ["key", "create", "--priv", "x/p.pem", "--pub", "x/q.pem"]))
add(S("key pub-encode/pub-decode", BASE_FILES, ["key", "pub-encode", "alice.pub"], ["key", "pub-encode", "missing.pem"],
      ["key", "pub-encode", "alice.vcf"], ["key", "pub-decode", B64["alice"]], step(["key", "pub-decode"], B64["bob"] + "\n"),
      step(["key", "pub-decode"], "  \n"), ["key", "pub-decode", "@@@"]))
add(S("key rotate", BASE_FILES, ["key", "rotate", "alice.vcf", "--priv", "n.pem", "--pub", "n.pub"], ["vcard", "validate", "alice.vcf"],
      ["key", "rotate", "alice.vcf", "--priv", "n.pem", "--pub", "m.pub"], ["key", "rotate", "missing.vcf"],
      ["key", "rotate", "bob.vcf", "-o", "out.vcf", "--priv", "o.pem", "--pub", "o.pub", "--reason", "superseded"],
      ["key", "rotate", "nokey.vcf", "--priv", "z.pem", "--pub", "z.pub"],
      ["key", "rotate", "bob.vcf", "--priv", "y.pem", "--pub", "y.pub", "--reason", "lost"]))
add(S("key add", BASE_FILES, ["key", "add", "bob.vcf", "--public-key", B64["alice"], "--no-pref"], ["key", "add", "bob.vcf"],
      ["key", "add", "bob.vcf"], ["key", "add", "nokey.vcf", "--priv", "k.pem", "--pub", "k.pub", "-o", "out.vcf"],
      ["key", "add", "bob.vcf", "--public-key", "@@@"], ["key", "add", "bob.vcf", "--public-key", B64["bob"]]))
add(S("key revoke", BASE_FILES, ["key", "revoke", "alice.vcf", "--key", B64["alice"], "--reason", "compromised"],
      ["key", "revoke", "alice.vcf", "--key", B64["alice"], "--reason", "lost"], ["key", "revoke", "bob.vcf", "--pub", "bob.pub", "--reason", "deprecated"],
      ["key", "revoke", "carol.pem", "--reason", "lost"], ["key", "revoke", "lf.vcf", "--reason", "lost"],
      ["key", "revoke", "lf.vcf", "--key", "x", "--pub", "y", "--reason", "lost"], ["key", "revoke", "lf.vcf", "--key", B64["carol"], "--reason", "bored"],
      ["key", "revoke", "lf.vcf", "--key", B64["bob"], "--reason", "lost"], ["key", "revoke", "lf.vcf", "--key", B64["carol"]]))

# vcard create
add(S("vcard create non-interactive", {}, ["vcard", "create", "-o", "c.vcf", "--fn", "Alice, Ex", "--source", "https://x.example/dsi.vcf",
      "--note", "line1\nline2", "--n", "Doe;Jane", "--categories", "a,b", "--email", "a@b.c", "--tel", "+1 555", "--bday", "19900101"],
      ["vcard", "validate", "c.vcf"], ["vcard", "create", "-o", "d/e.vcf", "--fn", "No Source"],
      ["vcard", "create", "-o", "bad.vcf", "--tel", "1\nFN:evil"], ["vcard", "create", "-o", "ml.vcf", "--fn", "x\r\nX-EVIL:1", "--source", "https://x.example/d.vcf"]))
add(S("vcard create --generate-key", {}, ["vcard", "create", "-o", "c.vcf", "--fn", "A", "--source", "https://a.example/d.vcf", "--generate-key"],
      ["vcard", "create", "-o", "d.vcf", "--fn", "A", "--generate-key"], ["vcard", "create", "-o", "d.vcf", "--fn", "A", "--generate-key", "--force"]))
N = 17
def answers(fn="", save="y", tail_yes=""):
    lines = [fn] + [""] * (N - 2) + ["https://example.com/card.vcf"]
    return "\n".join(lines) + "\n" + (tail_yes or "n\nn\nn\nn\nn\n") + save + "\n"
add(S("vcard create interactive", {}, step(["vcard", "create", "-i"], answers("Alice")), step(["vcard", "create", "-i", "-o", "n.vcf"], answers("Bob", "n")),
      step(["vcard", "create", "-i", "--resume", "-o", "r.vcf"], answers("")), step(["vcard", "create", "-i", "-o", "z.vcf"], "A\n")))
add(S("vcard create interactive extras", {}, step(["vcard", "create", "-i"], "Alice\n" + "\n" * 15 + "https://example.com/card.vcf\n" + "n\n"
      "y\nhttps://alice.example/feed.xml\ny\nes-ES\nhttps://alice.example/es.xml\nn\ny\nMastodon\n@alice@example.social\nn\ny\nPronouns\nshe/her\nn\ny\n")))
add(S("vcard create resume legacy", {"vcard_create.tmp": "junk\nfn=Legacy\n"}, step(["vcard", "create", "-i", "--resume"], answers(""))))

# parse / validate / inspect / normalize / verify
add(S("vcard parse", BASE_FILES, ["vcard", "parse", "alice.vcf"], ["vcard", "parse", "lf.vcf"], ["vcard", "parse", "malformed.vcf"], ["vcard", "parse", "bad.vcf"],
      step(["vcard", "parse", "-"], vcf("Stdin", "https://s.example/s.vcf", "bob")), step(["vcard", "parse"], vcf("Stdin2", "https://s.example/s.vcf")),
      step(["vcard", "parse"], ""), step(["vcard", "parse", "-"], ""), ["vcard", "parse", "/nonexistent/x.vcf"],
      step(["vcard", "parse", "-"], "FN:no framing\n")))
add(S("vcard validate", BASE_FILES, *[["vcard", "validate", f] for f in ("alice.vcf", "bob.vcf", "lf.vcf", "nokey.vcf", "bad.vcf", "malformed.vcf", "missing.vcf")],
      *[["vcard", "validate", f, "--json"] for f in ("alice.vcf", "lf.vcf", "bad.vcf", "malformed.vcf", "missing.vcf")],
      ["vcard", "validate", "nokey.vcf", "--strict"], ["vcard", "validate", "alice.vcf", "--strict"], step(["vcard", "validate", "-"], vcf("S", "https://s.example/s.vcf", "alice")),
      ["vcard", "validate", "http://example.com/x.vcf"]))
add(S("vcard inspect/normalize/verify", BASE_FILES, ["vcard", "inspect", "alice.vcf"], ["vcard", "inspect", "nokey.vcf"], ["vcard", "inspect", "missing.vcf"],
      ["vcard", "normalize", "alice.vcf"], ["vcard", "normalize", "lf.vcf"], ["vcard", "normalize", "malformed.vcf"], ["vcard", "normalize", "lf.vcf", "--write"],
      ["vcard", "normalize", "-", "--write"], ["vcard", "verify", "alice.vcf"], ["vcard", "verify", "missing.vcf"]))

# endorse
add(S("vcard endorse", BASE_FILES, ["vcard", "endorse", "bob.vcf", "--priv", "alice.pem"], ["vcard", "endorse", "bob.vcf", "lf.vcf", "--priv", "alice.pem", "-c", "high"],
      ["vcard", "endorse", "bob.vcf", "--priv", "alice.pem", "--write"], ["vcard", "endorse", "bob.vcf", "--priv", "alice.pem", "--write", "--vcard", "alice.vcf"],
      ["vcard", "endorse", "bob.vcf", "--priv", "alice.pem", "--write", "--vcard", "alice.vcf"], ["vcard", "verify", "alice.vcf"], ["vcard", "validate", "alice.vcf"],
      ["vcard", "endorse", "nokey.vcf", "--priv", "alice.pem"], ["vcard", "endorse", "missing.vcf", "--priv", "alice.pem"], ["vcard", "endorse", "bob.vcf", "missing.vcf", "--priv", "alice.pem"],
      ["vcard", "endorse", "bob.vcf", "--priv", "alice.pub"], ["vcard", "endorse", "bob.vcf", "--priv", "alice.pem", "-c", "huge"],
      ["vcard", "endorse", "bob.vcf", "--priv", "missing.pem"], ["vcard", "endorse", "bob.vcf"]))

# qr
add(S("vcard qr", BASE_FILES, ["vcard", "qr", "hello world", "-o", "q.png"], ["vcard", "qr", "alice.vcf", "-o", "q2.png"], step(["vcard", "qr", "-o", "q3.png"], "piped\n"),
      ["vcard", "qr", "data"], ["vcard", "qr", "data", "-o", "x.png", "-t", "Hi"], ["vcard", "qr", "data", "-o", "x.png", "-t", "Hi", "-f", "nope.ttf"],
      ["vcard", "qr", "data", "-o", "x.png", "-i", "nope.png"], step(["vcard", "qr", "-o", "x.png"], "")))

# fetch (offline cases only; the network paths are covered by the Go tests)
add(S("vcard fetch offline", BASE_FILES, ["vcard", "fetch", "missing.vcf"], ["vcard", "fetch", "nokey.vcf", "-n"], ["vcard", "fetch", "http://example.com/dsi.vcf", "--dry-run"]))

# feeds
add(S("feeds init/add", {}, ["feeds", "init", "feeds"], ["feeds", "init", "feeds"], ["feeds", "init", "a/b", "--no-sample"], ["feeds", "init", "a/b", "--no-sample"],
      ["feeds", "add", "-t", "T", "-m", "M", "-f", "new/dir/post.md"], ["feeds", "add", "-t", "T", "-m", "M", "-f", "new/dir/post.md"],
      ["feeds", "add", "-t", "T", "-m", "M", "-f", "notes"], ["feeds", "add", "-f", "x.md"], step(["feeds", "add", "-i", "-f", "i.md"], "\n\n"),
      ["feeds", "init", "x", "--type", "nope"], ["feeds", "new"]))
add(S("feeds build basics", BASE_FILES, ["feeds", "build", "feeds", "-o", "out.rss", *FEED_OPTS], ["feeds", "build", "feeds", "-o", "lim.rss", *FEED_OPTS, "--limit", "2"],
      ["feeds", "build", "feeds", "-o", "zero.rss", *FEED_OPTS, "--limit", "0"], ["feeds", "build", "feeds", "-o", "neg.rss", *FEED_OPTS, "--limit", "-1"],
      ["feeds", "build", "feeds", "-o", "vars.rss", *FEED_OPTS, "--var-file", "vars.env", "--var", "who=cli", "--var", "x=a=b"],
      ["feeds", "build", "feeds", "-o", "bad.rss", *FEED_OPTS, "--var", "foo"], ["feeds", "build", "feeds", "-o", "bad.rss", *FEED_OPTS, "--var-file", "nope.env"],
      ["feeds", "build", "nope", "-o", "no.rss", *FEED_OPTS], ["feeds", "build", "feeds", "-o", "lang.rss", *FEED_OPTS, "-g", "es-ES"],
      ["feeds", "build", "feeds", "-o", "x.rss", "--title", "T"], step(["feeds", "build", "feeds", "-o", "i.rss", "--interactive"], "Title\n\n\n\n\n"),
      ["feeds", "build", "feeds/a.md", "-o", "single.rss", *FEED_OPTS]))
add(S("feeds build signing", BASE_FILES, ["feeds", "build", "feeds", "-o", "signed.rss", *FEED_OPTS, "--sign-priv", "alice.pem", "--sign-pub", "alice.pub"],
      ["feeds", "verify", "signed.rss", "--pub", "alice.pub"], ["feeds", "verify", "signed.rss", "--vcard", "alice.vcf"], ["feeds", "verify", "signed.rss", "--pub", "bob.pub"],
      ["feeds", "verify", "signed.rss"], ["feeds", "build", "feeds", "-o", "one.rss", *FEED_OPTS, "--sign-priv", "alice.pem"],
      ["feeds", "build", "feeds", "-o", "nf.rss", *FEED_OPTS, "--sign-priv", "nope.pem", "--sign-pub", "alice.pub"],
      ["feeds", "verify", "missing.rss", "--pub", "alice.pub"]))

RICH_FILES = {
    **key_files("alice"),
    "posts/crlf.md": "---\r\ntitle: CRLF post\r\ndate: 2025-03-01T10:00:00Z\r\n---\r\nline one\r\nline two\r\n",
    "posts/uni.md": post("Ünïcode ☃ & <tags>", "2025-03-02T10:00:00+02:00", "Café, 日本語, 😀 & <b>raw</b>"),
    "posts/nested/deep/one.md": post("Nested", "2025-03-03", "nested body", "link: https://example.com/custom\nimage: https://example.com/pic.png\n"),
    "posts/quoted.md": "---\ntitle: \"Quoted: Title\"\ndate: '2025-03-04 08:30:00'\nid: my-explicit-id\nauthor: someone\n---\nTemplate {{ title }} {{ author }} {{ file_name }} {{ missing }}\n",
    "posts/html.md": post("Rich", "2025-03-05T00:00:00Z", "# Heading\n\nSome *em*, **strong**, `code` and a [link](https://x.example \"t\").\n\n- item 1\n- item 2\n\nbetween the lists\n\n1. one\n2. two\n\n```\nfenced\n```\n\n| a | b |\n|---|:-:|\n| 1 | 2 |\n\n> quote\n\n---\n", "use_html_content: yes\n"),
    "posts/empty.md": post("Empty body", "2025-03-06", ""),
    "posts/notmd.txt": "ignored",
    "posts/.hidden.md": post("Hidden file", "2025-03-07", "dot files are still markdown files"),
}
add(S("feeds build content variety", RICH_FILES, ["feeds", "build", "posts", "-o", "plain.rss", *FEED_OPTS],
      ["feeds", "build", "posts", "-o", "signed.rss", *FEED_OPTS, "--sign-priv", "alice.pem", "--sign-pub", "alice.pub", "--var", "title=T2"],
      ["feeds", "verify", "signed.rss", "--pub", "alice.pub"], ["feeds", "build", "posts", "-o", "lim.rss", *FEED_OPTS, "-l", "3", "-g", "ja-JP"],
      ["feeds", "build", "posts/uni.md", "-o", "single.rss", *FEED_OPTS]))
add(S("feeds build errors", {"posts/bad.md": "---\ntitle: x\n", "good/ok.md": post("ok", "2025-01-01", "b"), "dates/d.md": post("d", "not-a-date", "b"),
                              "bin/b.md": b"\xff\xfe not utf8"},
      ["feeds", "build", "posts", "-o", "o.rss", *FEED_OPTS], ["feeds", "build", "dates", "-o", "o.rss", *FEED_OPTS], ["feeds", "build", "bin", "-o", "o.rss", *FEED_OPTS],
      ["feeds", "build", "good", "-o", "o.rss", *FEED_OPTS, "--type", "json"]))

add(S("connections feed", {**BASE_FILES, "cards/uni.vcf": vcf("Zoë \"Z\" 'q' ☃", "https://z.example/z.vcf", None, "X-FEED;CATEGORY= a , ,b ;TAGS=b,c;LANGUAGE=es-ES:https://z.example/f.rss?a=1&b=2")}, ["connections", "feed", "cards"], ["connections", "feed", "cards", "-o", "sub/out.opml"], ["connections", "feed", "alice.vcf", "bob.vcf"],
      ["connections", "feed", "malformed.vcf"], ["connections", "feed", "missing"], ["connections", "feed", "nokey.vcf"]))

# top level
add(S("top level", {}, step([], help=True), step(["--debug"], help=True), ["nope"], step(["vcard"], help=True), step(["key"], help=True),
      step(["feeds"], help=True), step(["connections"], help=True), ["vcard", "nope"], ["key", "create", "--nope"], ["feeds", "build"]))
add(S("help screens exit codes", {}, *[step(args + ["--help"], help=True) for args in (
      [], ["vcard"], ["feeds"], ["key"], ["connections"], ["vcard", "create"], ["vcard", "fetch"], ["feeds", "build"], ["key", "rotate"])]))

# ----------------------------------------------------------------------------- cross scenarios
def cross_step(impl, *args, stdin=None):
    return step(list(args), stdin, impl)


add(S("cross: feed signed by python verifies in go and vice versa", BASE_FILES,
      cross_step("py", "feeds", "build", "feeds", "-o", "py.rss", *FEED_OPTS, "--sign-priv", "alice.pem", "--sign-pub", "alice.pub"),
      cross_step("go", "feeds", "verify", "py.rss", "--pub", "alice.pub"),
      cross_step("go", "feeds", "verify", "py.rss", "--vcard", "alice.vcf"),
      cross_step("go", "feeds", "build", "feeds", "-o", "go.rss", *FEED_OPTS, "--sign-priv", "alice.pem", "--sign-pub", "alice.pub"),
      cross_step("py", "feeds", "verify", "go.rss", "--pub", "alice.pub"),
      cross_step("py", "feeds", "verify", "go.rss", "--vcard", "alice.vcf"),
      cross_step("go", "feeds", "verify", "py.rss", "--pub", "bob.pub"),   # wrong key: must fail in both
      cross_step("py", "feeds", "verify", "go.rss", "--pub", "bob.pub"), cross=True))
add(S("cross: endorsements", BASE_FILES,
      cross_step("py", "vcard", "endorse", "bob.vcf", "--priv", "alice.pem", "--write", "--vcard", "alice.vcf"),
      cross_step("go", "vcard", "verify", "alice.vcf"), cross_step("go", "vcard", "validate", "alice.vcf"),
      cross_step("go", "vcard", "endorse", "alice.vcf", "--priv", "bob.pem", "--write", "--vcard", "bob.vcf"),
      cross_step("py", "vcard", "verify", "bob.vcf"), cross_step("py", "vcard", "validate", "bob.vcf"), cross=True))
add(S("cross: generated keys and rotation", {**BASE_FILES},
      cross_step("py", "key", "create", "--priv", "py.pem", "--pub", "py.pub"), cross_step("go", "key", "pub-encode", "py.pub"),
      cross_step("go", "vcard", "endorse", "bob.vcf", "--priv", "py.pem"),
      cross_step("go", "key", "create", "--priv", "go.pem", "--pub", "go.pub"), cross_step("py", "key", "pub-encode", "go.pub"),
      cross_step("py", "vcard", "endorse", "bob.vcf", "--priv", "go.pem"),
      cross_step("py", "key", "rotate", "alice.vcf", "--priv", "r1.pem", "--pub", "r1.pub"), cross_step("go", "vcard", "validate", "alice.vcf"),
      cross_step("go", "key", "rotate", "alice.vcf", "--priv", "r2.pem", "--pub", "r2.pub"), cross_step("py", "vcard", "validate", "alice.vcf"),
      cross_step("py", "key", "revoke", "alice.vcf", "--pub", "r2.pub", "--reason", "lost"), cross_step("go", "vcard", "validate", "alice.vcf"),
      cross_step("go", "key", "add", "alice.vcf", "--priv", "r3.pem", "--pub", "r3.pub"), cross_step("py", "vcard", "validate", "alice.vcf"),
      cross=True))
add(S("cross: created and normalized cards", {},
      cross_step("py", "vcard", "create", "-o", "p.vcf", "--fn", "Pä, Ex", "--source", "https://x.example/p.vcf", "--note", "multi\nline; note", "--generate-key"),
      cross_step("go", "vcard", "validate", "p.vcf"), cross_step("go", "vcard", "normalize", "p.vcf"), cross_step("py", "vcard", "normalize", "p.vcf"),
      cross_step("go", "vcard", "create", "-o", "g.vcf", "--fn", "Gö, Ex", "--source", "https://x.example/g.vcf", "--note", "multi\nline; note"),
      cross_step("py", "vcard", "validate", "g.vcf"), cross_step("py", "vcard", "parse", "g.vcf"), cross_step("go", "vcard", "parse", "g.vcf"), cross=True))

# ----------------------------------------------------------------------------- expected differences
# (scenario name or "*", substring of the step's args as a string) -> reason
# Each entry: (scenario, needle in the step's args, [(regex, replacement)] applied to stdout+stderr of both, reason).
# Only the described text is neutralised; exit codes, files and the rest of the output must still match.
EXPECTED = [
    ("vcard endorse", "alice.pub", [(r"(Failed to load private key from '[^']*': ).*", r"\1<library message>")],
     "the key-parsing library (cryptography vs Go) words its error differently"),
    ("vcard endorse", "-c huge", [(r"Must be one of: .*", "Must be one of: <levels>")],
     "Python lists the levels from an unordered set; Go lists them in a fixed order"),
    ("vcard fetch offline", "fetch", [(r"(Cannot resolve host '[^']*': ).*", r"\1<resolver message>"), (r"^Processing vCards\.\.\..*\n", "")],
     "the DNS error text comes from the OS/resolver and the rich progress bar is not drawn by the Go port"),
]

# ----------------------------------------------------------------------------- running
KNOWN_KEYS = set(B64.values())
KEY_RE = re.compile(r"MCowBQYDK2VwAyEA[A-Za-z0-9+/]{43}=")
PEM_RE = re.compile(r"(-----BEGIN [A-Z ]+-----\n)(.*?)(-----END [A-Z ]+-----)", re.S)
DATE_RE = re.compile(r"(?<![0-9])\d{8}T\d{6}Z")
ISO_RE = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z")
BUILD_RE = re.compile(r"<lastBuildDate>[^<]*</lastBuildDate>")
SLUG_RE = re.compile(r"\d{4}-\d{2}-\d{2}t\d{2}-\d{2}-\d{2}z")


def normalize(text, cwd):
    text = text.replace(str(cwd), "<CWD>")
    text = text.replace("\r\n ", "").replace("\n ", "\n")  # folded lines: key detection only
    text = text.replace("dsipy", "dsi")
    seen = {}

    def key(m):
        k = m.group(0)
        if k in KNOWN_KEYS:
            return k
        return seen.setdefault(k, f"<KEY{len(seen) + 1}>")

    text = KEY_RE.sub(key, text)
    text = PEM_RE.sub(lambda m: m.group(1) + "<PEM BODY>\n" + m.group(3), text)
    text = BUILD_RE.sub("<lastBuildDate>", text)
    text = DATE_RE.sub("<DATE>", text)
    text = ISO_RE.sub("<ISO>", text)
    text = SLUG_RE.sub("<SLUG>", text)
    # the signatures of items whose text changes with the date are not stable
    return text


def snapshot(root):
    out = {}
    for p in sorted(root.rglob("*")):
        rel = str(p.relative_to(root))
        if p.is_dir():
            out[rel + "/"] = "<dir>"
            continue
        mode = p.stat().st_mode & 0o777
        data = p.read_bytes()
        if data.startswith(b"\x89PNG"):
            w, h = struct.unpack(">II", data[16:24])
            content = f"<PNG {w}x{h}>"
        else:
            try:
                content = normalize(data.decode("utf-8"), root)
            except UnicodeDecodeError:
                content = f"<{len(data)} bytes>"
        out[rel] = (oct(mode), content)
    return out


def run_impl(impl, args, cwd, stdin):
    env = {"PATH": os.environ["PATH"], "HOME": str(cwd), "NO_COLOR": "1", "TERM": "dumb", "COLUMNS": "200", "LC_ALL": "C.UTF-8",
           "PYTHONIOENCODING": "utf-8", "PYTHONHASHSEED": "0", "DSI_PLUGIN_DIR": str(cwd / ".noplugins")}
    cmd = [sys.executable, "-c", PY_CODE, *args] if impl == "py" else [str(GO_BIN), *args]
    proc = subprocess.run(cmd, cwd=cwd, env=env, input=(stdin or "").encode(), capture_output=True, timeout=60)
    return proc.returncode, proc.stdout.decode("utf-8", "replace"), proc.stderr.decode("utf-8", "replace")


def populate(root, files):
    for rel, content in files.items():
        path = root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content.encode("utf-8") if isinstance(content, str) else content)


def apply_rules(text, rules):
    for regex, repl in rules:
        text = re.sub(regex, repl, text, flags=re.M)
    return text


def compare(scenario, st, py, go, cwd_py, cwd_go):
    """Return (differences, neutralised): human-readable differences and whether an EXPECTED rule was needed."""
    diffs = []
    args = st["args"]
    rules = []
    for name, needle, rs, _ in EXPECTED:
        if name in ("*", scenario) and needle in " ".join(args):
            rules += rs
    (rc_p, out_p, err_p), (rc_g, out_g, err_g) = py, go
    if rc_p != rc_g:
        diffs.append(f"exit code: python={rc_p} go={rc_g}")
    out_p, out_g = normalize(out_p, cwd_py), normalize(out_g, cwd_go)
    raw = (out_p, out_g)
    out_p, out_g = apply_rules(out_p, rules), apply_rules(out_g, rules)
    neutralised = bool(rules) and raw[0] != raw[1] and out_p == out_g
    if st.get("help"):
        pass  # help screens differ by design (typer/rich vs cobra); the exit code is still compared
    elif out_p != out_g:
        diffs.append(f"stdout differs:\n--- python\n{out_p}\n--- go\n{out_g}")
    if rc_p != 2 and not st.get("help"):  # usage texts differ by design (click vs cobra)
        e_p, e_g = apply_rules(normalize(err_p, cwd_py), rules), apply_rules(normalize(err_g, cwd_go), rules)
        # Python prints tracebacks/warnings on stderr only in debug mode
        if e_p != e_g:
            diffs.append(f"stderr differs:\n--- python\n{e_p}\n--- go\n{e_g}")
    return diffs, neutralised


def describe_diff(a, b, context=120):
    """Short description of how two snapshot entries differ (first differing region)."""
    if a is None or b is None:
        return "only in " + ("go" if a is None else "python")
    if a[0] != b[0]:
        return f"mode python={a[0]} go={b[0]}"
    x, y = (a[1], b[1]) if isinstance(a, tuple) else (a, b)
    i = next((k for k in range(min(len(x), len(y))) if x[k] != y[k]), min(len(x), len(y)))
    lo = max(0, i - context)
    return f"first difference at offset {i}\n      python: ...{x[lo:i + context]!r}\n      go:     ...{y[lo:i + context]!r}"


def run_scenario(sc, stats):
    failures = []
    if sc["cross"]:
        with tempfile.TemporaryDirectory() as tmp:
            cwd = Path(tmp)
            populate(cwd, sc["files"])
            for st in sc["steps"]:
                rc, out, err = run_impl(st["impl"], st["args"], cwd, st["stdin"])
                stats["steps"] += 1
                bad = []
                wrong_key = "bob.pub" in st["args"] and st["args"][:2] == ["feeds", "verify"]
                if wrong_key and rc != 1:
                    bad.append(f"verification with the wrong key must fail (rc={rc})")
                elif not wrong_key and rc != 0:
                    bad.append(f"exit code {rc}")
                if bad:
                    failures.append((st, bad + [out, err]))
        return failures
    with tempfile.TemporaryDirectory() as tp, tempfile.TemporaryDirectory() as tg:
        cwd_p, cwd_g = Path(tp), Path(tg)
        populate(cwd_p, sc["files"])
        populate(cwd_g, sc["files"])
        for st in sc["steps"]:
            py = run_impl("py", st["args"], cwd_p, st["stdin"])
            go = run_impl("go", st["args"], cwd_g, st["stdin"])
            stats["steps"] += 1
            diffs, neutralised = compare(sc["name"], st, py, go, cwd_p, cwd_g)
            if diffs:
                failures.append((st, diffs))
            elif neutralised:
                stats["expected"] += 1
                reason = next(r for n, needle, _, r in EXPECTED if n in ("*", sc["name"]) and needle in " ".join(st["args"]))
                stats["expected_list"].append((sc["name"], " ".join(st["args"]), reason))
            else:
                stats["identical"] += 1
        sp, sg = snapshot(cwd_p), snapshot(cwd_g)
        if sp != sg:
            names = sorted(set(sp) | set(sg))
            lines = []
            for n in names:
                if sp.get(n) != sg.get(n):
                    lines.append(f"  {n}: {describe_diff(sp.get(n), sg.get(n))}")
            failures.append(({"args": ["<final files>"]}, ["files differ:\n" + "\n".join(lines)]))
    return failures


def main():
    if not GO_BIN.exists():
        sys.exit(f"{GO_BIN} not found: run `make build` first")
    only = sys.argv[1] if len(sys.argv) > 1 else None
    stats = {"steps": 0, "identical": 0, "expected": 0, "expected_list": []}
    failed = 0
    for sc in SCENARIOS:
        if only and only not in sc["name"]:
            continue
        failures = run_scenario(sc, stats)
        status = "ok " if not failures else "FAIL"
        print(f"[{status}] {sc['name']} ({len(sc['steps'])} steps)")
        for st, diffs in failures:
            failed += 1
            print(f"    step: {' '.join(st['args'])!r}" + (f" (stdin={st.get('stdin')!r:.40})" if st.get("stdin") else ""))
            for d in diffs:
                print("      " + d.replace("\n", "\n      "))
    print(f"\n{stats['steps']} steps: {stats['identical']} identical, {stats['expected']} expected differences, {failed} unexpected")
    for name, args, reason in stats["expected_list"]:
        print(f"  expected: [{name}] {args}: {reason}")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
