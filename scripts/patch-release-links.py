#!/usr/bin/env python3
"""Repoint GODsend-360 download links at a freshly uploaded link manifest.

Usage: python3 -I scripts/patch-release-links.py <links.txt> <file> [file ...]

`links.txt` rows are `name | gofile-url | kiwi-url`. Every download row in the
docs is matched by artifact *identity* with the version stripped out, so the job
survives a version bump: the row is found, and both its label and its URL are
rewritten to the new artifact.

Two row shapes are handled, chosen by file extension:

- `.md` - a markdown link whose display text is a code span,
  ``[`godsend.exe`](https://gofile.io/...)``.
- `.html` - the site's platform cards, whose anchor carries the artifact name in
  `data-artifact`: `<a href="..." data-artifact="godsend-Setup-2.14.1.exe">`.

A row whose fresh URL is missing or EMPTY is left completely alone - a failed
upload must never blank out a link that still works.
"""
import os
import re
import sys
import tempfile
from pathlib import Path

# Markdown link whose display text is a single code span, e.g. [`godsend.exe`](https://...)
LINK_RE = re.compile(r"\[`([^`]+)`\]\((https?://[^)\s]+)\)")
# HTML row on the download page. The artifact name is the anchor's data-artifact,
# and its first span carries the same name, so the regex needs no class names.
SITE_ROW_RE = re.compile(
    r'<a href="([^"]+)"([^>]*?)data-artifact="([^"]+)"([^>]*)><span>([^<]+)</span>'
)
VERSION_RE = re.compile(r"\d+\.\d+\.\d+")
# The two artifact names that also get spelled out in prose, not just in tables.
# The negative lookahead skips `...exe`] so this pass never rewrites the display
# text of a markdown link - patch_links owns that, and a row whose upload failed
# must keep a label that matches the artifact its URL actually serves.
PROSE_RE = re.compile(r"(godsend-(?:Setup|Portable)-)(\d+\.\d+\.\d+)(\.exe)(?!`\])")


def key(name):
    """`godsend-Setup-2.13.3.exe` -> `godsend-Setup-V.exe` (version-independent)."""
    return VERSION_RE.sub("V", name)


def load_links(path):
    table = {}
    for line in Path(path).read_text().splitlines():
        parts = [p.strip() for p in line.split("|")]
        if len(parts) != 3:
            continue
        name, gofile, kiwi = parts
        urls = {"gofile.io": gofile, "file.kiwi": kiwi}
        if any(u.startswith("http") for u in urls.values()):
            table[key(name)] = {"urls": urls, "name": name}
    return table


def patch_links(text, table):
    hits = []

    def sub(m):
        name, url = m.group(1), m.group(2)
        entry = table.get(key(name))
        if not entry:
            return m.group(0)
        for host, fresh in entry["urls"].items():
            # Leave the row untouched unless this host gave us a real fresh URL.
            if host in url and fresh.startswith("http"):
                if fresh != url:
                    hits.append(entry["name"])
                return f"[`{entry['name']}`]({fresh})"
        return m.group(0)

    return LINK_RE.sub(sub, text), hits


def patch_prose(text, new_version):
    """Catch `godsend-Setup-<old>.exe` / `godsend-Portable-<old>.exe` in body text."""
    old = {m.group(2) for m in PROSE_RE.finditer(text)}
    if not old or old == {new_version}:
        return text, 0
    count = 0
    for version in old:
        if version == new_version:
            continue
        text, n = PROSE_RE.subn(rf"\g<1>{new_version}\g<3>", text)
        count += n
    return text, count


def patch_site_rows(text, table):
    """Rewrite the download page's platform rows, keeping every other attribute.

    The site row is explicitly marked with data-artifact, so the fresh GoFile URL
    is applied whatever the previous href was (the first run points the rows away
    from the README anchor). GoFile primary, file.kiwi as the fallback when the
    GoFile upload for that artifact failed.
    """
    hits = []

    def sub(m):
        href, before, artifact, after, label = m.groups()
        entry = table.get(key(artifact))
        if not entry:
            return m.group(0)
        fresh = entry["urls"]["gofile.io"]
        if not fresh.startswith("http"):
            fresh = entry["urls"]["file.kiwi"]
            if not fresh.startswith("http"):
                return m.group(0)
        name = entry["name"]
        if fresh != href or name != artifact or name != label:
            hits.append(name)
        return f'<a href="{fresh}"{before}data-artifact="{name}"{after}><span>{name}</span>'

    return SITE_ROW_RE.sub(sub, text), hits


def patch_file(text, table, version):
    """Dispatch on the row shape the file uses. Returns (text, rows, prose)."""
    if SITE_ROW_RE.search(text):
        rows, hits = patch_site_rows(text, table)
        return rows, hits, 0
    text, hits = patch_links(text, table)
    prose = 0
    if version:
        text, prose = patch_prose(text, version)
    return text, hits, prose


def detect_version(table):
    """The version in the manifest's artifact names, or None if none carry one."""
    for entry in table.values():
        m = VERSION_RE.search(entry["name"])
        if m:
            return m.group(0)
    return None


def main(argv):
    if len(argv) < 3:
        sys.exit(__doc__)
    table = load_links(argv[1])
    if not table:
        sys.exit("no usable rows in the link manifest - run the upload first")
    version = detect_version(table)
    total = 0
    for rel in argv[2:]:
        path = Path(rel)
        text, hits, prose = patch_file(path.read_text(), table, version)
        if hits or prose:
            path.write_text(text)
        total += len(hits) + prose
        print(f"{rel}: {len(hits)} link(s), {prose} prose mention(s) updated")
    print(f"total: {total}")


def demo():
    """Self-check: host routing, version bump, and the leave-it-alone-on-failure rule."""
    table = {
        "godsend.exe": {"urls": {"gofile.io": "https://gofile.io/d/N1", "file.kiwi": "https://file.kiwi/a"}, "name": "godsend.exe"},
        "godsend-Setup-V.exe": {"urls": {"gofile.io": "https://gofile.io/d/N2", "file.kiwi": "EMPTY"}, "name": "godsend-Setup-2.14.0.exe"},
    }
    # A version-bumped Setup row updates text AND url; kiwi failed, so it keeps its old url.
    text = (
        "| [`godsend.exe`](https://gofile.io/d/OLD) | [`godsend.exe`](https://file.kiwi/old) |\n"
        "| [`godsend-Setup-2.13.3.exe`](https://gofile.io/d/OLD2) | [`godsend-Setup-2.13.3.exe`](https://file.kiwi/old2) |\n"
        "Run **`godsend-Setup-2.13.3.exe`** to install.\n"
        "| [`unrelated.exe`](https://gofile.io/d/KEEP) |\n"
    )
    out, hits = patch_links(text, table)
    out, prose = patch_prose(out, "2.14.0")
    assert out == (
        "| [`godsend.exe`](https://gofile.io/d/N1) | [`godsend.exe`](https://file.kiwi/a) |\n"
        "| [`godsend-Setup-2.14.0.exe`](https://gofile.io/d/N2) | [`godsend-Setup-2.13.3.exe`](https://file.kiwi/old2) |\n"
        "Run **`godsend-Setup-2.14.0.exe`** to install.\n"
        "| [`unrelated.exe`](https://gofile.io/d/KEEP) |\n"
    ), out
    assert hits == ["godsend.exe", "godsend.exe", "godsend-Setup-2.14.0.exe"], hits
    assert prose == 1, prose

    # Nothing fresh anywhere: the docs must come back byte-identical.
    empty = {"godsend.exe": {"urls": {"gofile.io": "EMPTY", "file.kiwi": "EMPTY"}, "name": "godsend.exe"}}
    same, _ = patch_links(text, empty)
    assert same == text

    # Version detection drives the prose pass; the server-only binaries carry none.
    assert detect_version(table) == "2.14.0", detect_version(table)
    assert detect_version(empty) is None, detect_version(empty)

    # Manifest parsing: real rows load, EMPTY-only rows still load (so the docs keep
    # their working URL), malformed lines and MISSING rows are skipped entirely.
    with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False) as fh:
        fh.write(
            "godsend.exe | https://gofile.io/d/A | https://file.kiwi/a\n"
            "godsend-linux-x64 | https://gofile.io/d/B | EMPTY\n"
            "godsend-mac | MISSING | MISSING\n"
            "garbage line with no pipes\n"
        )
        manifest = fh.name
    try:
        loaded = load_links(manifest)
    finally:
        os.unlink(manifest)
    assert set(loaded) == {"godsend.exe", "godsend-linux-x64"}, loaded
    assert loaded["godsend-linux-x64"]["urls"]["file.kiwi"] == "EMPTY"
    # An EMPTY kiwi URL must leave that half of the row alone.
    kept, _ = patch_links("| [`godsend-linux-x64`](https://file.kiwi/old) |\n", loaded)
    assert kept == "| [`godsend-linux-x64`](https://file.kiwi/old) |\n", kept

    # The site's platform rows: href and label both follow the manifest, every
    # other attribute is preserved, and an artifact the manifest lacks is untouched.
    site = (
        '<a href="https://github.com/ghostyshell/GODSend-360#quick-installation" rel="noopener" '
        'data-artifact="godsend-Setup-2.13.3.exe" class="flex justify-between"><span>godsend-Setup-2.13.3.exe</span>'
        '<span class="text-xs">installer</span></a>\n'
        '<a href="https://gofile.io/d/KEEP" data-artifact="godsend-unknown.exe"><span>godsend-unknown.exe</span></a>\n'
    )
    patched, site_hits = patch_site_rows(site, table)
    assert patched == (
        '<a href="https://gofile.io/d/N2" rel="noopener" '
        'data-artifact="godsend-Setup-2.14.0.exe" class="flex justify-between"><span>godsend-Setup-2.14.0.exe</span>'
        '<span class="text-xs">installer</span></a>\n'
        '<a href="https://gofile.io/d/KEEP" data-artifact="godsend-unknown.exe"><span>godsend-unknown.exe</span></a>\n'
    ), patched
    assert site_hits == ["godsend-Setup-2.14.0.exe"], site_hits

    # No fresh GoFile link: the row keeps the href it had rather than going blank.
    site_empty = '<a href="https://gofile.io/d/OLD" data-artifact="godsend-unknown.exe"><span>godsend-unknown.exe</span></a>\n'
    same, none = patch_site_rows(site_empty, table)
    assert same == site_empty and none == [], (same, none)

    # GoFile failed but file.kiwi landed: fall back to the mirror.
    kiwi_only = {"godsend.exe": {"urls": {"gofile.io": "EMPTY", "file.kiwi": "https://file.kiwi/z"}, "name": "godsend.exe"}}
    out, _ = patch_site_rows('<a href="https://x" data-artifact="godsend.exe"><span>godsend.exe</span></a>\n', kiwi_only)
    assert out == '<a href="https://file.kiwi/z" data-artifact="godsend.exe"><span>godsend.exe</span></a>\n', out

    # patch_file routes each shape: markdown through links + prose, HTML through rows.
    _, _, prose = patch_file("Run **`godsend-Setup-2.13.3.exe`**\n", table, "2.14.0")
    assert prose == 1, prose
    _, rows, prose2 = patch_file(site, table, "2.14.0")
    assert rows and prose2 == 0, (rows, prose2)

    # The committed page must keep the contract the weekly refresh depends on:
    # each card's data-artifact is the filename its span shows. A hand-edited row
    # that breaks this would be skipped silently, so fail here instead.
    page = Path(__file__).resolve().parent.parent / "docs" / "index.html"
    if page.exists():
        for _href, _pre, artifact, _post, label in SITE_ROW_RE.findall(page.read_text()):
            assert artifact == label, f"docs/index.html: data-artifact {artifact!r} != label {label!r}"

    print("demo OK")


if __name__ == "__main__":
    demo() if "--demo" in sys.argv else main(sys.argv)
