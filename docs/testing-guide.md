# Hands-on testing guide

A step-by-step walkthrough of every `dsi` feature that you can run yourself. Every step shows the command, what you should see, and a `PASS`/`FAIL` line so you know straight away whether it behaved as expected. Failure cases are included on purpose: an error with the right message and exit code *is* the expected result.

- Everything runs in a throw-away folder (`/tmp/dsi-lab`), so no keys, cards or feeds end up in the repository. **Do not run these commands inside the repository checkout.**
- Blocks tagged `bash` can be pasted as they are, in order, in one terminal (they share state and the working directory). Blocks tagged `sh` are optional or interactive and are not part of the automatic run.
- Keys, dates and signatures are random or time-based, so your values will differ from the sample output. The `PASS`/`FAIL` lines and exit codes are what matter.
- For a flag-by-flag reference see [commands.md](commands.md); for templates see [publishing.md](publishing.md); for plugins see [plugins.md](plugins.md).

Contents: [0 Setup](#0-setup) · [1 Keys](#1-keys) · [2 Create a vCard](#2-create-a-vcard) · [3 Validate and inspect](#3-validate-inspect-normalize-parse) · [4 Key rotation and revocation](#4-key-add-rotate-revoke) · [5 Endorsements](#5-endorsements) · [6 QR codes](#6-qr-codes) · [7 Fetch](#7-fetch-remote-cards) · [8 Build feeds](#8-feeds-init-add-build) · [9 Sign and verify](#9-sign-and-verify-feeds) · [10 Plugins](#10-plugins) · [11 OPML](#11-connections-opml) · [12 Automated tests](#12-automated-tests) · [13 Behaviour notes](#13-behaviour-notes) · [14 Cleanup](#14-cleanup)

## 0. Setup

You need the `dsi` binary on your `PATH` (see the [README](../README.md#install)), or build it from a checkout with `make build` (it ends up in `bin/dsi`) and put that directory on the `PATH`. The checks use standard tools (`grep`, `sed` (GNU), `stat`, `file`); `xmllint`, `zbarimg` and `fc-list` are optional.

```sh
dsi --version
dsi --help
```

Now start the lab. This block defines two helpers used everywhere below:

```bash
mkdir -p /tmp/dsi-lab && cd /tmp/dsi-lab
command -v dsi >/dev/null || { echo "dsi is not on the PATH"; return 1 2>/dev/null || exit 1; }

# expect <exit code> <command...>: run a command and compare its exit code
expect() { local want=$1; shift; "$@"; local got=$?
  if [ "$got" -eq "$want" ]; then echo "PASS (exit $got): $*"; else echo "FAIL (wanted exit $want, got $got): $*"; fi; }
# check <description> <command...>: PASS if the command succeeds silently
check() { local d=$1; shift; if "$@" >/dev/null 2>&1; then echo "PASS: $d"; else echo "FAIL: $d"; fi; }

expect 0 dsi --help
expect 0 dsi --version
expect 2 dsi vcard          # a group without a subcommand shows its help and exits 2
expect 0 dsi vcard --help
expect 0 dsi key --help
expect 0 dsi feeds --help
expect 0 dsi connections --help
expect 0 dsi plugin list
expect 2 dsi no-such-command
```

Expected: the top-level help lists `vcard`, `feeds`, `connections`, `key` and `plugin`, plus the global `--debug` option; every line above ends in `PASS`.

**Debug mode.** Errors are normally one red line. `--debug` (or `DSI_DEBUG=1`) also prints a Go stack trace:

```bash
expect 1 dsi key pub-decode "not-base64!!"              # one-line error
expect 1 dsi --debug key pub-decode "not-base64!!"      # error + stack trace
DSI_DEBUG=1 dsi key pub-decode "not-base64!!" 2>&1 | grep -c goroutine   # prints 1 or more
```

## 1. Keys

Ed25519 key pairs, as PEM files. **Needs:** nothing.

```bash
expect 0 dsi key create --priv alice.key --pub alice.pub
check "private key is mode 600" test "$(stat -c %a alice.key)" = 600
```

```text
✅ Keypair generated and saved to 'alice.key' and 'alice.pub'
📋 Public key (Base64-encoded DER for vCard): MCowBQYDK2VwAyEA...=
```

**It never overwrites a key silently:**

```bash
expect 1 dsi key create --priv alice.key --pub alice.pub          # refuses
expect 0 dsi key create --priv alice.key --pub alice.pub --force  # explicit overwrite
check "private key is still mode 600 after --force" test "$(stat -c %a alice.key)" = 600
```

Expected on the refusal: `❌ 'alice.key' already exists; refusing to overwrite a key file (use --force).`

**Encode / decode** the public key between PEM and the Base64 form that goes in a vCard:

```bash
B64=$(dsi key pub-encode alice.pub)
echo "$B64"
check "pub-decode (argument) round-trips" test "$(dsi key pub-decode "$B64")" = "$(cat alice.pub)"
check "pub-decode (stdin) round-trips"    test "$(echo "$B64" | dsi key pub-decode)" = "$(cat alice.pub)"
expect 1 dsi key pub-decode "not-base64!!"
```

## 2. Create a vCard

**Needs:** nothing (this creates your identity for the rest of the guide).

```bash
expect 0 dsi vcard create -o alice.vcf --fn "Alice Example" \
  --source https://alice.example/dsi.vcf --generate-key
cat alice.vcf
check "private key written with mode 600" test "$(stat -c %a vcard_private.pem)" = 600
check "card has a KEY line"    grep -q '^KEY;' alice.vcf
check "card has the SOURCE"    grep -q '^SOURCE:https://alice.example/dsi.vcf' alice.vcf
```

```text
BEGIN:VCARD
VERSION:4.0
FN:Alice Example
LANG:en-US
KIND:individual
SOURCE:https://alice.example/dsi.vcf
KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:MCowBQYDK2VwAyEA...
 ...=                      <- long lines are folded at 75 characters
END:VCARD
```

**Key files are protected:** running it again must not clobber `vcard_private.pem`. Prove `--force` in a side folder so `alice.vcf` keeps matching its key:

```bash
expect 1 dsi vcard create -o again.vcf --fn "Alice Example" --source https://alice.example/dsi.vcf --generate-key
check "alice's key file untouched" test -f vcard_private.pem
mkdir -p forcedemo && ( cd forcedemo \
  && dsi vcard create -o a.vcf --fn A --source https://a.example/dsi.vcf --generate-key >/dev/null \
  && expect 1 dsi vcard create -o a.vcf --fn A --source https://a.example/dsi.vcf --generate-key \
  && expect 0 dsi vcard create -o a.vcf --fn A --source https://a.example/dsi.vcf --generate-key --force )
```

**More fields** (text is escaped properly, so commas and semicolons in values are safe):

```bash
expect 0 dsi vcard create -o rich.vcf --fn "Rich, Card" --n "Card;Rich" --nickname rich \
  --email rich@example.com --categories "tech,music" --note "Hello; world" \
  --url https://rich.example --source https://rich.example/dsi.vcf
expect 0 dsi vcard validate rich.vcf
```

**Interactive mode and resume.** `-i` asks for every field. If you cancel (Ctrl-C), your answers are kept in `vcard_create.tmp` and `--resume` offers them as defaults; the file is deleted once the card is saved. Try it for real:

```sh
dsi vcard create -i -o carol.vcf       # type a name, then press Ctrl-C
ls vcard_create.tmp                        # exists
dsi vcard create -i --resume -o carol.vcf   # your earlier answers are the defaults; finish it
ls vcard_create.tmp                        # gone
```

Or scripted (answers piped in; `SOURCE` is mandatory in interactive mode):

```bash
mkdir -p inter && cd inter
printf 'Carol Example\nCarol\n' | expect 1 dsi vcard create -i -o carol.vcf     # input ends early = cancel
check "temp file kept after a cancel" test -f vcard_create.tmp
mkdir -p ign && cp vcard_create.tmp ign/        # a plain run overwrites the temp file, so test on a copy
check "temp file ignored without --resume" sh -c 'cd ign && printf "\n" | dsi vcard create -i -o x.vcf 2>&1 | grep -q "Full Name (FN) \[\]"'
( for i in $(seq 16); do echo; done; echo https://carol.example/dsi.vcf; printf 'n\nn\nn\nn\nn\ny\n' ) \
  | expect 0 dsi vcard create -i --resume -o carol.vcf
check "card was saved with the resumed name" grep -q '^FN:Carol Example' carol.vcf
check "temp file deleted after saving" test ! -f vcard_create.tmp
cd ..
```

## 3. Validate, inspect, normalize, parse

**Needs:** `alice.vcf` from step 2.

```bash
expect 0 dsi vcard validate alice.vcf
dsi vcard validate alice.vcf --json
expect 0 dsi vcard validate alice.vcf --strict
cat alice.vcf | expect 0 dsi vcard validate -
expect 0 dsi vcard inspect alice.vcf
```

```text
✅ Valid (0 warning(s))
{"valid": true, "errors": [], "warnings": []}
Identity
  Name:   Alice Example
  SOURCE: https://alice.example/dsi.vcf
Keys
  Preferred: ed25519
  Keys: 1
  Revoked: 0
...
```

**Things that must be rejected** (exit 1):

```bash
check "creating without --source warns" sh -c 'dsi vcard create -o nosource.vcf --fn "No Source" 2>&1 | grep -q "No SOURCE"'
check "the card was still created" test -f nosource.vcf
expect 1 dsi vcard validate nosource.vcf          # ❌ [source-missing] SOURCE property is required
dsi vcard validate nosource.vcf --json || true    # {"valid": false, "errors": [{"code": "source-missing", ...
cat alice.vcf alice.vcf > two.vcf
expect 1 dsi vcard validate two.vcf               # ❌ [malformed-line] multiple vCards found; only the first is parsed
grep -v '^END' alice.vcf > noend.vcf
expect 1 dsi vcard validate noend.vcf             # ❌ ... missing END:VCARD
expect 1 dsi vcard validate does-not-exist.vcf    # ❌ Cannot load ...
```

`--strict` also fails on warnings. A card with no KEY has a warning only, so:

```bash
printf 'BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Zed\r\nSOURCE:https://z.example/dsi.vcf\r\nEND:VCARD\r\n' > nokey.vcf
expect 0 dsi vcard validate nokey.vcf             # valid, with a [key-missing] warning
expect 1 dsi vcard validate nokey.vcf --strict    # warning becomes a failure
```

**Normalize** prints the deterministic form used for signing (CRLF line endings, sorted parameters). With `--write` it rewrites the file:

```bash
printf 'BEGIN:VCARD\nVERSION:4.0\nSOURCE:https://z.example/dsi.vcf\nFN:Zed\nEND:VCARD\n' > messy.vcf
dsi vcard validate messy.vcf                      # warning: [line-endings] Lines are not terminated with CRLF
expect 0 dsi vcard normalize messy.vcf --write
check "line-ending warning is gone" sh -c '! dsi vcard validate messy.vcf | grep -q line-endings'
expect 1 dsi vcard normalize https://localhost/x.vcf --write     # ❌ --write needs a local file.
```

**Parse** prints the card as JSON (from a file, from a pipe, or `-`):

```bash
dsi vcard parse alice.vcf | grep -o '"fn": "[^"]*"\|"source": "[^"]*"'
check "parse prints the name" sh -c 'dsi vcard parse alice.vcf | grep -q "\"fn\": \"Alice Example\""' 
cat alice.vcf | expect 0 dsi vcard parse > /dev/null
expect 0 dsi vcard parse - < alice.vcf > /dev/null
expect 1 dsi vcard parse < /dev/null             # ❌ No input data provided for parsing.
```

## 4. Key add, rotate, revoke

**Needs:** `alice.vcf`. These work on a copy so the identity from step 2 stays intact.

```bash
cp alice.vcf work.vcf

# add: generates a NEW key pair (so --priv/--pub must be paths that do not exist yet) ...
expect 0 dsi key add work.vcf --priv add.key --pub add.pub --no-pref -o work_added.vcf
check "two KEY lines after add" test "$(grep -c '^KEY' work_added.vcf)" = 2
# ... or adds a public key you already have
dsi key create --priv k2.key --pub k2.pub >/dev/null
expect 0 dsi key add work.vcf --public-key "$(dsi key pub-encode k2.pub)" --pref -o work_pk.vcf
expect 1 dsi key add work.vcf --priv add.key --pub add.pub -o x.vcf      # refuses to overwrite add.key
```

**Rotate** makes a new preferred key and revokes the old one in one step:

```bash
expect 0 dsi key rotate work.vcf --priv rot.key --pub rot.pub --reason rotated -o work_rot.vcf
grep -E '^(KEY|REVKEY)' work_rot.vcf | cut -c1-80
check "one REVKEY line"  test "$(grep -c '^REVKEY' work_rot.vcf)" = 1
check "rotated card is valid" dsi vcard validate work_rot.vcf
dsi vcard inspect work_rot.vcf | sed -n 4,8p                  # Keys: 2 / Revoked: 1
```

**Revoke** a key explicitly (reasons: compromised, rotated, superseded, retired, lost, deprecated):

```bash
expect 0 dsi key revoke work_rot.vcf --pub rot.pub --reason compromised -o work_rev.vcf
# ⚠️ The vCard has no usable key left. Add one with `dsi key add`.
expect 0 dsi vcard validate work_rev.vcf              # valid, but warns [key-pref-missing]
expect 1 dsi vcard validate work_rev.vcf --strict
expect 1 dsi key revoke work_rot.vcf --pub rot.pub --reason bogus   # Unknown reason 'bogus' ...
```

## 5. Endorsements

An endorsement is your signature over someone else's public key, stored in your card as `X-ENDORSE`. **Needs:** `alice.vcf` and `vcard_private.pem` (step 2).

Create a second identity, Bob, in his own folder:

```bash
mkdir -p bob && ( cd bob && dsi vcard create -o bob.vcf --fn "Bob Builder" \
  --source https://bob.example/dsi.vcf --generate-key >/dev/null )
ls bob
```

Endorse Bob with Alice's key. Without `--write` it only prints the line; with it, the line is added to your card:

```bash
expect 0 dsi vcard endorse bob/bob.vcf --priv vcard_private.pem
expect 0 dsi vcard endorse bob/bob.vcf --priv vcard_private.pem -c high --vcard alice.vcf --write
check "X-ENDORSE line added to alice.vcf" grep -q '^X-ENDORSE' alice.vcf
expect 0 dsi vcard verify alice.vcf
expect 0 dsi vcard validate alice.vcf
dsi vcard inspect alice.vcf | tail -2                  # Endorsements 1
```

```text
✅ valid    MCowBQYDK2VwAyEA...=
```

**Tampering must be caught.** Change one hex digit of the signature, then try an uppercase signature (the format requires lowercase):

```bash
# flip the first hex digit of the first signature (0 becomes 1, anything else becomes 0)
sed -E '0,/SIG=[0-9a-f]/{s/SIG=0/SIG=1/;t;s/SIG=[1-9a-f]/SIG=0/}' alice.vcf > alice_bad.vcf
# uppercase every signature (GNU sed)
sed -E 's/SIG=([0-9a-f]+)/SIG=\U\1/' alice.vcf > alice_upper.vcf
expect 1 dsi vcard verify alice_bad.vcf       # invalid ... (signature does not match any key)
expect 1 dsi vcard verify alice_upper.vcf     # invalid ... (signature must be lowercase hexadecimal)
expect 1 dsi vcard validate alice_upper.vcf   # ❌ [endorse-sig-format] ... lowercase hexadecimal
```

**A revoked signer is not trusted.** Revoke Alice's signing key and re-verify:

```bash
expect 0 dsi key revoke alice.vcf --key "$(dsi key pub-encode vcard_public.pem)" --reason compromised -o alice_revoked.vcf
expect 1 dsi vcard verify alice_revoked.vcf   # invalid ... (the signing key was revoked as compromised)
expect 2 dsi vcard endorse bob/bob.vcf        # usage error: missing --priv
expect 1 dsi vcard endorse missing.vcf --priv vcard_private.pem   # ❌ No valid vCard files found.
```

## 6. QR codes

**Needs:** `alice.vcf`. For captions you need a `.ttf` font (the sample command finds one on Linux; elsewhere set `FONT` yourself).

```bash
expect 0 dsi vcard qr alice.vcf -o qr.png                 # the whole card as a QR code
expect 0 dsi vcard qr "https://alice.example" -o qr_text.png   # any text that is not a file
cat alice.vcf | expect 0 dsi vcard qr -o qr_pipe.png      # from a pipe
file qr.png                                                  # PNG image data, 1210 x 1210 (size grows with the data)

FONT="${FONT:-$(fc-list 2>/dev/null | grep -i 'DejaVuSans.ttf' | head -1 | cut -d: -f1)}"; echo "font: $FONT"
expect 0 dsi vcard qr alice.vcf -o qr_cap.png -t "Alice" -b "alice.example" -f "$FONT"
printf 'GIF89a\x01\x00\x01\x00\x80\x00\x00\xc8\x1e\x1e\x00\x00\x00\x2c\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02\x44\x01\x00\x3b' > logo.gif   # a 1x1 red GIF (any PNG, JPEG, GIF or WebP works)
expect 0 dsi vcard qr alice.vcf -o qr_logo.png -i logo.gif     # logo in the centre
```

Open `qr_cap.png` in an image viewer: "Alice" above the code and "alice.example" below. If `zbarimg` is installed (`zbar-tools`), prove the codes decode back to the data:

```bash
command -v zbarimg >/dev/null && {
  check "qr.png decodes to the vCard"       sh -c 'zbarimg --quiet --raw qr.png 2>/dev/null | head -1 | grep -q "^BEGIN:VCARD"'
  check "logo QR still decodes"             sh -c 'zbarimg --quiet --raw qr_logo.png 2>/dev/null | head -1 | grep -q "^BEGIN:VCARD"'
  check "text QR decodes to the URL"        sh -c 'test "$(zbarimg --quiet --raw qr_text.png 2>/dev/null)" = "https://alice.example"'
} || echo "zbarimg not installed: scan qr.png with your phone instead"
```

**Errors** (all exit 1): captions need an existing font, an output file is mandatory, a missing logo is rejected:

```bash
expect 1 dsi vcard qr alice.vcf -o e1.png -t "Alice"                  # ❌ A font file must be specified when using captions.
expect 1 dsi vcard qr alice.vcf -o e2.png -t "Alice" -f /nope.ttf     # ❌ The specified font file does not exist
expect 1 dsi vcard qr alice.vcf                                       # ❌ Output file path is required
expect 1 dsi vcard qr alice.vcf -o e3.png -i nope.png                 # ❌ The specified image file does not exist
```

## 7. Fetch remote cards

`vcard fetch` re-downloads a card from the URL in its own `SOURCE` (or from a URL you pass) so your local copy follows the published one. **Constraints that protect you from SSRF:** only `https://`; the host must resolve to a *public* address (so `localhost`, `127.0.0.1`, `192.168.x.x` and a local test server are all refused); and the downloaded card's `SOURCE` must match the URL unless you pass `--no-verify-source`.

**Checks that need no internet** (the failures are the expected result; each prints a summary with `Failed: 1` and exits 1):

```bash
expect 1 dsi vcard fetch http://example.com/dsi.vcf --dry-run     # Unsupported URL scheme 'http' (allowed: https)
expect 1 dsi vcard fetch https://localhost/dsi.vcf --dry-run       # Refusing to fetch 'localhost': it resolves to a non-public address
expect 1 dsi vcard fetch https://127.0.0.1/dsi.vcf -n              # same, for an IP
check "a failed fetch does not print Done." sh -c '! dsi vcard fetch https://127.0.0.1/dsi.vcf -n 2>&1 | grep -q "Done\."'
expect 1 dsi vcard fetch https://192.168.1.10/dsi.vcf -n           # same, for a private range
expect 1 dsi vcard fetch alice.vcf --dry-run                       # Cannot resolve host 'alice.example' (a reserved, never-resolving name)
expect 0 dsi vcard fetch nosource.vcf --dry-run                    # No SOURCE property found. Skipping.  (Skipped: 1)
expect 1 dsi vcard fetch nope.vcf                                  # No valid .vcf files or URLs provided.
```

**The full flow needs a card hosted on a real HTTPS URL** (GitHub Pages, any static host). Put a card there whose `SOURCE` is exactly that URL, then:

```sh
URL=https://YOU.github.io/dsi.vcf
dsi vcard create -o remote.vcf --fn "Remote" --source $URL     # publish remote.vcf at $URL first
mkdir mirror && dsi vcard fetch $URL -o mirror                 # downloads mirror/dsi.vcf   (Downloaded: 1)
dsi vcard fetch $URL -o mirror                                 # nothing changed            (Unchanged: 1)
# change the hosted card (e.g. edit FN), then:
dsi vcard fetch $URL -o mirror --dry-run --diff               # shows the diff, writes nothing (Would update: 1)
dsi vcard fetch $URL -o mirror --backup                       # updates and keeps mirror/dsi.vcf.bak (Updated: 1)
dsi vcard fetch mirror/dsi.vcf --dry-run                      # re-reads the SOURCE inside a local file
```

You can also verify the whole flow offline: the automated tests run it against a local TLS server (`make test`, see [12](#12-automated-tests)).

## 8. Feeds: init, add, build

A feed is a folder of Markdown posts with a small front matter block. **Needs:** nothing.

```bash
expect 0 dsi feeds init feeds                    # creates feeds/ and a sample feeds/hello.md
cat feeds/hello.md
expect 0 dsi feeds init feeds                    # idempotent: "Directory already exists"
expect 0 dsi feeds init nosample --no-sample     # only a .gitkeep
expect 0 dsi feeds add --title "Second post" --message "More news & <b>things</b>" --filename feeds/second.md
expect 1 dsi feeds add --title "Second post" --message "x" --filename feeds/second.md    # refuses to overwrite
```

Now a few posts that exercise the interesting rules (dates in different formats, nested folders, a pinned id, HTML, special characters):

```bash
mkdir -p feeds/2025
printf -- '---\ntitle: "Quoted & <title>"\ndate: 2025-03-01T10:00:00+02:00\n---\nNested post with R&D < 5.\n' > feeds/2025/nested.md
printf -- '---\ntitle: Pinned id\ndate: 2025-01-15\nid: my-pinned-id\n---\nBody\n' > feeds/pinned.md
printf -- '---\ntitle: Html post\ndate: 2025-02-01 08:30\nuse_html_content: true\n---\n<p>Hello <b>html</b> & more</p>\n' > feeds/html.md
```

**Build.** Title, link, description, author and e-mail are required:

```bash
expect 1 dsi feeds build feeds -o feed.rss </dev/null    # ❌ The title cannot be empty. (fails fast, never hangs waiting for input)
expect 1 dsi feeds build no-such-folder -o x.rss -t T -k https://a.example -d D -a A -e a@a.example   # ❌ Directory not found
expect 0 dsi feeds build feeds -o feed.rss -t "Alice Feed" -k https://alice.example \
  -d "Alice's news" -a Alice -e alice@alice.example
```

Verify the result is valid XML and every rule worked:

```bash
command -v xmllint >/dev/null && check "feed.rss is well-formed XML" xmllint --noout feed.rss
echo "items: $(grep -o '<item>' feed.rss | wc -l)"
grep -o '<item><title>[^<]*</title>\|<guid[^>]*>[^<]*</guid>\|<pubDate>[^<]*</pubDate>' feed.rss
check "media namespace is declared" grep -q 'xmlns:media="http://search.yahoo.com/mrss/"' feed.rss
check "HTML post is wrapped in CDATA" grep -q '<!\[CDATA\[<p>Hello' feed.rss
```

```text
items: 5
<item><title>Second post</title>
<pubDate>Sat, 03 Oct 2026 15:47:41 GMT</pubDate>
<guid isPermaLink="false">second</guid>
...
<item><title>Quoted &amp; &lt;title&gt;</title>
<pubDate>Sat, 01 Mar 2025 08:00:00 GMT</pubDate>      <- +02:00 converted to UTC
<guid isPermaLink="false">2025-nested</guid>           <- id = path without extension
...
<guid isPermaLink="false">my-pinned-id</guid>          <- `id:` front matter wins
```

The ids come from the file path *relative to the folder you build*, so they are the same wherever the folder lives:

```bash
cp -r feeds feeds_copy
dsi feeds build feeds_copy -o copy.rss -t T -k https://alice.example -d D -a A -e a@a.example
check "guids identical for a copied folder" sh -c 'diff <(grep -o "<guid[^>]*>[^<]*" feed.rss | sort) <(grep -o "<guid[^>]*>[^<]*" copy.rss | sort)'
```

**`--limit`** keeps the newest N items; `0` gives an empty feed; negatives are rejected:

```bash
B="-t T -k https://a.example -d D -a A -e a@a.example"
expect 0 dsi feeds build feeds -o lim2.rss $B --limit 2
expect 0 dsi feeds build feeds -o lim0.rss $B --limit 0
expect 2 dsi feeds build feeds -o limneg.rss $B --limit -1
check "limit 2 gives 2 items" test "$(grep -o '<item>' lim2.rss | wc -l)" = 2
check "limit 0 gives 0 items" test "$(grep -o '<item>' lim0.rss | wc -l)" = 0
check "no file written for a rejected limit" test ! -f limneg.rss
```

**Templates and variables.** `{{ name }}` (one space inside the braces) is replaced in title, id, link, image and content. Values come from `--var key=value`, a `--var-file`, or the post's own front matter (`file_name`, `file_path`, `file_dir`, `file_ext` are always available). `--var` beats `--var-file`:

```bash
mkdir -p tpl
printf -- '---\ntitle: Release {{ version }}\ndate: 2025-03-01\nlink: {{ site }}/posts/{{ file_name }}\nuse_html_content: true\n---\nRead **more** at {{ site }}. Unknown: {{ nope }}\n' > tpl/rel.md
printf '# comment\nsite=https://file.example\nversion=0.9\n' > vars.env
expect 0 dsi feeds build tpl -o tpl.rss $B --var site=https://cli.example --var version=1.2 --var-file vars.env
grep -o '<title>Release[^<]*</title>\|<link>https://cli[^<]*</link>\|<description>.*</description>' tpl.rss
```

```text
<title>Release 1.2</title>
<link>https://cli.example/posts/rel.md</link>
<description><![CDATA[<p>Read <strong>more</strong> at https://cli.example. Unknown: {{ nope }}</p>]]></description>
```

Errors (the build never half-succeeds silently, and the message names the file):

```bash
expect 2 dsi feeds build tpl -o x.rss $B --var foo                  # Invalid --var 'foo': expected key=value
expect 1 dsi feeds build tpl -o x.rss $B --var-file missing.env     # Variable file not found
printf -- '---\ntitle: Bad date\ndate: not-a-date\n---\nx\n' > tpl/bad.md
expect 1 dsi feeds build tpl -o x.rss $B                            # tpl/bad.md: invalid date 'not-a-date' ...
rm tpl/bad.md
printf -- '---\ntitle: Unterminated\ndate: 2025-01-01\nBody without closing marker\n' > tpl/unterm.md
expect 1 dsi feeds build tpl -o x.rss $B                            # tpl/unterm.md: unterminated front matter
rm tpl/unterm.md
```

**Interactive** (`-i` asks for what is missing; the `feeds add` prompts are filename, then title, then message). Accepting every default must give a proper `.md` post inside `feeds/`:

```bash
mkdir -p addtest && ( cd addtest && printf '\n\n\n' | dsi feeds add -i >/dev/null \
  && check "default post is feeds/<timestamp>.md" sh -c 'ls feeds/*.md >/dev/null' )
```

Try the rest by hand:

```sh
dsi feeds add -i
dsi feeds build feeds -o interactive.rss -i
```

## 9. Sign and verify feeds

Signing adds an Ed25519 `<signature>` to every item so readers can prove a post came from the owner of a key. **Needs:** `feeds/` (step 8) and Alice's key pair `vcard_private.pem` / `vcard_public.pem` (step 2).

```bash
expect 0 dsi feeds build feeds -o signed.rss $B --sign-priv vcard_private.pem --sign-pub vcard_public.pem
check "every item has a signature" test "$(grep -o '<signature' signed.rss | wc -l)" = "$(grep -o '<item>' signed.rss | wc -l)"
expect 0 dsi feeds verify signed.rss --pub vcard_public.pem     # public key
expect 0 dsi feeds verify signed.rss --vcard alice.vcf          # keys taken from the vCard
```

```text
✅ valid    Second post
...
5 item(s), 0 invalid
```

**Failure cases:**

```bash
sed 's|<title>Hello DSI</title>|<title>Hello EVIL</title>|' signed.rss > tampered.rss
expect 1 dsi feeds verify tampered.rss --vcard alice.vcf        # ❌ invalid  Hello EVIL (bad signature)
expect 1 dsi feeds verify signed.rss --pub k2.pub               # wrong key: no matching key for key-id ...
expect 1 dsi feeds verify signed.rss                            # ❌ Pass --vcard or --pub
expect 0 dsi feeds verify feed.rss --vcard alice.vcf            # unsigned feed: ⚠️ unsigned (warnings, not failures)
expect 1 dsi feeds build feeds -o one.rss $B --sign-priv vcard_private.pem   # ❌ Signing needs both --sign-priv and --sign-pub
expect 1 dsi feeds build feeds -o one.rss $B --sign-pub vcard_public.pem
expect 1 dsi feeds build feeds -o one.rss $B --sign-priv nope.pem --sign-pub vcard_public.pem   # ❌ Signing key file not found for --sign-priv: nope.pem
check "no half-signed file was written" test ! -f one.rss
```

## 10. Plugins

`dsi` can be extended with executables named `dsi-<name>`; `dsi <name> args...` runs them (details in [plugins.md](plugins.md)). Publishing feeds to GitHub or S3 is meant to be such a plugin (`dsi-publish`, not released yet), so here we use a small stand-in. **Needs:** nothing.

```bash
mkdir -p plugins
cat > plugins/dsi-hello <<'EOF'
#!/bin/sh
echo "hello from a plugin: args=[$*] version=$DSI_VERSION"
[ "$1" = "--fail" ] && exit 7
[ "$1" = "--stdin" ] && cat
exit 0
EOF
chmod +x plugins/dsi-hello
export DSI_PLUGIN_DIR="$PWD/plugins"

expect 0 dsi hello a "b c" --flag        # hello from a plugin: args=[a b c --flag] version=...
expect 7 dsi hello --fail                # the plugin's exit code is dsi's exit code
echo "piped text" | dsi hello --stdin    # stdin is passed through
check "plugin list shows it"    sh -c 'dsi plugin list | grep -q "^hello"'
check "dsi --help lists it"     sh -c 'dsi --help | grep -A3 "^Plugins" | grep -q hello'
expect 2 dsi publish                     # no dsi-publish plugin installed: No such command 'publish'
```

**Plugins cannot shadow core commands, and names cannot escape the directory:**

```bash
printf '#!/bin/sh\necho "SHADOWED"\n' > plugins/dsi-key && chmod +x plugins/dsi-key
check "dsi key still runs the core command" sh -c '! dsi key --help | grep -q SHADOWED'
dsi plugin list | grep key                                  # key  ...  (shadowed by the core command, cannot be run)
expect 2 dsi ../plugins/dsi-hello
rm plugins/dsi-key
unset DSI_PLUGIN_DIR
```

## 11. connections (OPML)

`connections feed` turns a set of cards into an OPML subscription list: one `<outline>` per `X-FEED`, with `language` and `category` when present. **Needs:** `alice.vcf`, `bob/bob.vcf`.

```bash
mkdir -p friends
sed 's|^END:VCARD|X-FEED;LANGUAGE=en-US;CATEGORY=tech:https://alice.example/feed.rss\r\nX-FEED;LANGUAGE=es-ES;TAGS=cocina,viajes:https://alice.example/es.rss\r\nEND:VCARD|' alice.vcf > friends/alice.vcf
sed 's|^END:VCARD|X-FEED:https://bob.example/feed.rss\r\nEND:VCARD|' bob/bob.vcf > friends/bob.vcf
cp alice.vcf friends/nofeed.vcf                                   # valid card, no feed
printf 'this is not a vcard\n' > friends/broken.vcf                # garbage
mkdir -p onlyfeeds && cp friends/alice.vcf friends/bob.vcf onlyfeeds/
expect 0 dsi connections feed friends                  # OPML to stdout; "Skipping malformed vCard in friends/broken.vcf"
expect 0 dsi connections feed friends -o following.opml
grep -o '<outline [^>]*>' following.opml
```

```text
<outline text="Alice Example" type="rss" category="tech" xmlUrl="https://alice.example/feed.rss" language="en-US" title="Alice Example" />
<outline text="Alice Example" type="rss" category="cocina,viajes" xmlUrl="https://alice.example/es.rss" language="es-ES" title="Alice Example" />
<outline text="Bob Builder" type="rss" xmlUrl="https://bob.example/feed.rss" title="Bob Builder" />
```

Three outlines from two cards (Alice has two feeds), the broken card was skipped with a warning, and the card without feeds contributed nothing. More cases:

```bash
check "alice's two feeds both present" test "$(grep -o 'alice.example/[a-z]*.rss' following.opml | sort -u | wc -l)" = 2
expect 0 dsi connections feed onlyfeeds/alice.vcf onlyfeeds/bob.vcf -o sub/dir/two.opml     # creates parent folders
check "output folder created" test -f sub/dir/two.opml
expect 1 dsi connections feed friends/nofeed.vcf     # ❌ No valid vCards with feed URLs found
expect 1 dsi connections feed does-not-exist         # ❌ No vCard files found
```

## 12. Automated tests

The same behaviour is covered by the Go test suite. From the repository root (tests run in the Docker dev container, no local Go needed):

```sh
make test      # go test ./...
make lint      # go vet + gofmt check
```

| Feature | Tests |
|---|---|
| `key` (create/add/rotate/revoke/encode) | `internal/cli/key_test.go`, `internal/crypto/crypto_test.go`, `internal/core/lifecycle_test.go` |
| `vcard create` (flags, interactive, resume) | `internal/cli/vcard_test.go`, `internal/vcard/serializer_test.go` |
| `vcard validate/inspect/normalize/parse` | `internal/core/validator_test.go`, `internal/vcard/parser_test.go`, `internal/canonical/canonical_test.go`, `internal/cli/vcard_test.go` |
| `vcard endorse/verify` | `internal/endorsements/verify_test.go`, `internal/cli/vcard_test.go` |
| `vcard fetch` and URL safety | `internal/cli/fetch_test.go`, `internal/core/fetch_test.go`, `internal/core/resolver_golden_test.go` |
| `vcard qr` | `internal/vcard/qr_test.go`, `internal/cli/vcard_test.go` |
| `feeds init/add/build/verify`, signing, OPML | `internal/cli/feeds_test.go`, `internal/feeds/feeds_test.go`, `internal/feeds/isodate_test.go` |
| `connections feed` | `internal/cli/key_test.go`, `internal/feeds/feeds_test.go` |
| Plugins | `internal/plugin/plugin_test.go`, `internal/cli/plugin_test.go` |
| CLI help text and error handling | `internal/cli/app_test.go` |

Most packages also compare against the frozen golden fixtures (`testdata/golden`, see `testdata/README.md`).

## 13. Behaviour notes

Things that can surprise you, all intended:

- There is no `feeds publish` in the core; publishing is meant to be a `dsi-publish` plugin (see step 10).
- A command group run without a subcommand (`dsi vcard`) prints its help and exits `2`.
- `vcard create` without `--source` still creates the card but warns; `vcard validate` rejects it until a `SOURCE` is set. In interactive mode `SOURCE` is mandatory.
- `feeds verify` on an unsigned feed warns (`unsigned`) and exits `0`; only an invalid signature exits `1`.
- `--sign-priv` / `--sign-pub` accept a key *file* or the PEM *text* itself (handy for CI secrets); a value that is neither is reported as "not found".
- `feeds build` only reads `.md` files, and `feeds add` warns if you name a post without that extension.
- Changing the item id rules (or building from a different folder name) changes feed `guid`s, so readers see all items as new once. Pin them with `id:` in the front matter.

## 14. Cleanup

```sh
rm -rf /tmp/dsi-lab      # the whole sandbox, including the demo private keys
```

The demo keys here are throw-away. Never reuse keys from this folder for a real identity, and keep real `*.pem` files out of git (the repository `.gitignore` already excludes `*.pem`, `out/` and `*.vcf`).
