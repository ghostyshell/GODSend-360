#!/usr/bin/env python3
"""Repoint GODsend-360 download links at a fresh R2 upload manifest.

Usage: python3 -I scripts/patch-release-links.py <links.txt> <file> [file ...]

`links.txt` rows are `name | url`, where the URL is the versioned Cloudflare R2
object. Every download row is matched by artifact *identity* (name with the
version stripped), so the job survives a version bump: the row's label and URL
are both rewritten to the new artifact.

Two row shapes are handled, chosen by file extension:

- `.md` - a platform table row. Older rows carried two host cells
  (`| ... | [`a.exe`](gofile) | [`a.exe`](kiwi) |`); they collapse to a single
  Download cell, and already-collapsed rows are refreshed in place.
- `.html` - the site's platform cards, whose anchor carries the artifact name in
  `data-artifact`: `<a href="..." data-artifact="godsend-Setup-2.14.1.exe">`.

A row whose fresh URL is missing or EMPTY is left completely alone - a failed
upload must never blank out a link that still works.
"""
import re
import sys
from pathlib import Path

VERSION_RE = re.compile(r"\d+\.\d+\.\d+")
# A table row with the legacy pair of host cells.
TWO_CELL_RE = re.compile(
    r"^(\|[^|\n]*\|)\s*\[`([^`]+)`\]\(([^)\s]+)\)[ \t]*\|[ \t]*\[`([^`]+)`\]\(([^)\s]+)\)[ \t]*\|[ \t]*$",
    re.M)
# A table row already collapsed to one Download cell.
ONE_CELL_RE = re.compile(r"^(\|[^|\n]*\|)[ \t]*\[`([^`]+)`\]\(([^)\s]+)\)[ \t]*\|[ \t]*$", re.M)
# The table's header + separator, which lose the mirror column.
HEADER_RE = re.compile(
    r"^\| Platform \| GoFile \| file\.kiwi backup \|\n\|[-\s|]+\|$", re.M)
# HTML row on the download page. The artifact name is the anchor's data-artifact,
# and its first span carries the same name, so the regex needs no class names.
SITE_ROW_RE = re.compile(
    r'<a href="([^"]+)"([^>]*?)data-artifact="([^"]+)"([^>]*)><span>([^<]+)</span>')
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
        if len(parts) < 2:
            continue
        name, url = parts[0], parts[1]
        if name and url:
            table[key(name)] = {"url": url, "name": name}
    return table


def patch_header(text):
    """Drop the mirror column from the markdown tables (idempotent)."""
    if not HEADER_RE.search(text):
        return text, 0
    return HEADER_RE.sub("| Platform | Download |\n| --- | --- |", text), 1


def patch_links(text, table):
    hits = []

    def collapse(m):
        cell, name, url = m.group(1), m.group(2), m.group(3)
        entry = table.get(key(name))
        if not entry or not entry["url"].startswith("http"):
            return m.group(0)
        # A version-less filename keeps its name across releases; the URL still
        # has to count as a change or patch_file would not write the file back.
        if entry["name"] != name or entry["url"] != url:
            hits.append(entry["name"])
        return f"{cell} [`{entry['name']}`]({entry['url']}) |"

    def refresh(m):
        cell, name = m.group(1), m.group(2)
        entry = table.get(key(name))
        if not entry or not entry["url"].startswith("http"):
            return m.group(0)
        fresh_name, fresh_url = entry["name"], entry["url"]
        if fresh_name != name or fresh_url != m.group(3):
            hits.append(fresh_name)
        return f"{cell} [`{fresh_name}`]({fresh_url}) |"

    text = TWO_CELL_RE.sub(collapse, text)
    text, header = patch_header(text)
    if header:
        hits.append("table header")
    return ONE_CELL_RE.sub(refresh, text), hits


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


# Version mentions on the download page: the hero's status line and the
# structured-data softwareVersion. Both are stamped from the manifest's version.
SITE_VERSION_RES = (
    re.compile(r"(>v)(\d+\.\d+\.\d+)( · )"),
    re.compile(r'("softwareVersion":\s*")(\d+\.\d+\.\d+)(")'),
)


def patch_site_version(text, version):
    """Point the page's visible and structured version at the released one."""
    count = 0

    def sub(m):
        nonlocal count
        if m.group(2) == version:
            return m.group(0)
        count += 1
        return f"{m.group(1)}{version}{m.group(3)}"

    for rx in SITE_VERSION_RES:
        text = rx.sub(sub, text)
    return text, count


def patch_site_rows(text, table):
    """Rewrite the download page's platform rows, keeping every other attribute."""
    hits = []

    def sub(m):
        href, before, artifact, after, label = m.groups()
        entry = table.get(key(artifact))
        if not entry or not entry["url"].startswith("http"):
            return m.group(0)
        name, fresh = entry["name"], entry["url"]
        if fresh != href or name != artifact or name != label:
            hits.append(name)
        return f'<a href="{fresh}"{before}data-artifact="{name}"{after}><span>{name}</span>'

    return SITE_ROW_RE.sub(sub, text), hits


def patch_file(text, table, version):
    """Dispatch on the row shape the file uses. Returns (text, rows, prose)."""
    if SITE_ROW_RE.search(text):
        text, hits = patch_site_rows(text, table)
        prose = 0
        if version:
            text, prose = patch_site_version(text, version)
        return text, hits, prose
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
    table = {
        "godsend.exe": {"url": "https://covers.debrid.in/godsend-360/releases/2.14.0/godsend.exe", "name": "godsend.exe"},
        "godsend-Setup-V.exe": {"url": "https://covers.debrid.in/godsend-360/releases/2.14.0/godsend-Setup-2.14.0.exe", "name": "godsend-Setup-2.14.0.exe"},
    }
    legacy = (
        "| Platform | GoFile | file.kiwi backup |\n|---|---|---|\n"
        "| **Windows (installer)** | [`godsend-Setup-2.13.3.exe`](https://gofile.io/d/OLD2) | [`godsend-Setup-2.13.3.exe`](https://file.kiwi/old2) |\n"
        "| **Windows (backend)** | [`godsend.exe`](https://gofile.io/d/OLD) | [`godsend.exe`](https://file.kiwi/old) |\n"
    )
    out, hits = patch_links(legacy, table)
    assert out == (
        "| Platform | Download |\n| --- | --- |\n"
        "| **Windows (installer)** | [`godsend-Setup-2.14.0.exe`](https://covers.debrid.in/godsend-360/releases/2.14.0/godsend-Setup-2.14.0.exe) |\n"
        "| **Windows (backend)** | [`godsend.exe`](https://covers.debrid.in/godsend-360/releases/2.14.0/godsend.exe) |\n"
    ), out
    # Idempotent: a second pass changes nothing and reports nothing.
    again, none = patch_links(out, table)
    assert again == out and none == [], (again, none)

    # Version-less rows (headless binaries) change URL only; that still counts.
    _, url_hits = patch_links(
        "| Platform | Download |\n| --- | --- |\n| **Windows** | [`godsend.exe`](https://old) |\n",
        {"godsend.exe": {"url": "https://covers.debrid.in/godsend-360/releases/2.14.0/godsend.exe", "name": "godsend.exe"}})
    assert "godsend.exe" in url_hits, url_hits

    # A failed upload leaves the row alone, header rewrite or not.
    fresh, _ = patch_links("| Platform | Download |\n| --- | --- |\n| **X** | [`godsend.exe`](https://old) |\n",
                           {"godsend.exe": {"url": "EMPTY", "name": "godsend.exe"}})
    assert "https://old" in fresh, fresh

    # Prose mentions follow the manifest version; the link's display text does not.
    prose, n = patch_prose("Run **`godsend-Setup-2.13.3.exe`** to install.", "2.14.0")
    assert prose == "Run **`godsend-Setup-2.14.0.exe`** to install." and n == 1, (prose, n)

    site = (
        '<a href="https://gofile.io/d/OLD" rel="noopener" '
        'data-artifact="godsend-Setup-2.13.3.exe" class="x"><span>godsend-Setup-2.13.3.exe</span></a>\n'
        '<a href="https://gofile.io/d/KEEP" data-artifact="godsend-unknown.exe"><span>godsend-unknown.exe</span></a>\n'
    )
    patched, site_hits = patch_site_rows(site, table)
    assert patched.startswith(
        '<a href="https://covers.debrid.in/godsend-360/releases/2.14.0/godsend-Setup-2.14.0.exe" rel="noopener" '
        'data-artifact="godsend-Setup-2.14.0.exe" class="x"><span>godsend-Setup-2.14.0.exe</span></a>'), patched
    assert "godsend-unknown.exe" in patched and site_hits == ["godsend-Setup-2.14.0.exe"], site_hits

    stamps = '<p>v2.13.3 · Windows</p><script>{"softwareVersion": "2.13.3"}</script>\n'
    stamped, n = patch_site_version(stamps, "2.14.0")
    assert stamped == '<p>v2.14.0 · Windows</p><script>{"softwareVersion": "2.14.0"}</script>\n' and n == 2, (stamped, n)

    assert detect_version(table) == "2.14.0", detect_version(table)

    # The committed page must keep the contract the refresh depends on: each
    # card's data-artifact is the filename its span shows.
    page = Path(__file__).resolve().parent.parent / "docs" / "index.html"
    if page.exists():
        for _href, _pre, artifact, _post, label in SITE_ROW_RE.findall(page.read_text()):
            assert artifact == label, f"docs/index.html: data-artifact {artifact!r} != label {label!r}"

    print("demo OK")


if __name__ == "__main__":
    demo() if "--demo" in sys.argv else main(sys.argv)
