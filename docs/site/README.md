# GODsend 360 — marketing site

Single-file static landing page for [GODsend-360](https://github.com/ghostyshell/GODSend-360). No build step.

## Stack
- `index.html` — hand-written HTML, Tailwind via Play CDN, vanilla JS for the dynamic year.
- Google Fonts: **Russo One** (display) + **Chakra Petch** (body) + **JetBrains Mono** (code).
- SVG-only assets — `favicon.svg`, `og-image.svg` — so the site stays text-diffable.

## SEO checklist (applied)
- Semantic `<header>`, `<main>`, `<section>`, `<nav>`, `<footer>`, `<article>`, `<figure>`, `<ol>`, `<details>`.
- Single `<h1>`; sequential heading hierarchy.
- `<title>`, `<meta description>`, `<meta keywords>`, `<link rel="canonical">`.
- Open Graph + Twitter Card with 1200×630 SVG OG image.
- JSON-LD: `SoftwareApplication`, `FAQPage`, `WebSite`.
- `robots.txt` + `sitemap.xml`.
- `site.webmanifest` for PWA / install prompt.
- `prefers-reduced-motion` respected (animations gated).
- WCAG-conscious contrast (slate-100 on `#020617` body, `#22C55E` accents on dark surfaces).
- `loading="lazy"` not needed — no raster images on the page; mock screenshots are pure CSS.
- `<link rel="preconnect">` for Google Fonts; `dns-prefetch` for github / gofile / file.kiwi.
- Skip-link `Skip to content` for keyboard users.
- `aria-label`s on icon-only SVGs; `aria-hidden` on decorative ones.

## Layout
- `docs/index.html` — landing page (GitHub Pages serves this at the root URL).
- `docs/site/` — all supporting assets:
  - `favicon.svg`, `og-image.svg`
  - `robots.txt`, `sitemap.xml`
  - `site.webmanifest`
  - this `README.md`

The split exists because GitHub Pages can only deploy from `/` or `/docs`. Putting the page at `docs/index.html` is what makes Pages publish it; keeping the assets inside `docs/site/` keeps the marketing files isolated from the technical reference docs (`docs/features.md`, `docs/api-reference.md`, etc.).

## Local preview
```bash
cd docs
python3 -m http.server 8000
# then open http://localhost:8000
```

## Deploy (GitHub Pages)
- **Settings → Pages → Source: Deploy from a branch**, branch `main`, folder `/docs`.
- The site publishes to `https://ghostyshell.github.io/GODSend-360/`.
- All canonical / OG / sitemap / manifest URLs in `index.html` already point there.
- To switch to a custom domain, search-and-replace `ghostyshell.github.io/GODSend-360` → your domain in `index.html` and `docs/site/{robots.txt,sitemap.xml,site.webmanifest}`.

## Updating
When a release ships, update three things in `docs/index.html`:
1. Version string in the JSON-LD `softwareVersion`, hero version pill, and footer.
2. Filenames in the Downloads section.
3. The hero stats (libraries, etc.) if those have shifted.

`docs/site/sitemap.xml` `<lastmod>` should be bumped on any non-trivial copy change.
