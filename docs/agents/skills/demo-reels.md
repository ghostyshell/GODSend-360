# Skill: Demo videos and guides (demo-reels)

**This repo:** the GODsend 360 desktop app (`addon` = `godsend360`, an application recording from the renderer's `?mock=1` bridge). Guides data `docs/guides.json`, page `docs/video-guides.html` (`node ~/Code/demo-reels/build-site.mjs godsend360`), linked from `docs/index.html`, `docs/sitemap.xml` and the README.

Any change to a tab or flow of the app updates its video and guides entry in the same change. Tooling lives in `~/Code/demo-reels`.

---

# demo-reels

Scripted Playwright recordings turned into demo videos. One script per video, rerunnable after any edit, so a typo or a changed UI is a re-run, not a re-record.

## Where things live

| Path | What |
|---|---|
| `~/Code/demo-reels/lib/reel.mjs` | The runner: `reel({...})`. Wraps testreel `recordPage` (cursor, window chrome, gradient background, mp4) and injects the overlay (captions, spotlight ring + label, title card). |
| `~/Code/demo-reels/<addon>/reels.mjs` | Every reel for `tpb`, `cinema` or `horror`, keyed by output filename. **Add or edit videos here.** Exports HOST, THEME, REELS. |
| `~/Code/demo-reels/run.mjs` | `node run.mjs <addon> [prefix ...]` records matching reels (none = all). `DRY=1` runs the steps with no recording to debug selectors in seconds. |
| `~/Code/demo-reels/guides/<addon>.mjs` | The guides copy: sections, and per video a title, a short walkthrough and numbered steps. |
| `~/Code/demo-reels/publish.mjs` | `node publish.mjs <addon>` uploads each mp4 (faststart) and a jpg poster to R2 (`guides/<addon>/`, content-hashed names) and rewrites the addon repo's guides JSON. |
| `~/Downloads/<addon>-demos/<name>.mp4` | Output (override with `REEL_OUT`). |
| `~/Code/demo-reels/logs/` | Per-reel logs from batch runs. |

Env: `TPB_HOST`, `CINEMA_HOST`, `HORROR_HOST` (defaults are the production hosts), `HEADED=1` to watch the browser, `REEL_OUT` for the output dir.

## Run

```bash
cd ~/Code/demo-reels
DRY=1 node run.mjs cinema            # fast selector check, no video
node run.mjs cinema 04               # one reel by prefix
node run.mjs horror                  # all of an addon (sequential, ~1-2 min each)
printf "%s\n" 02 03 04 | xargs -P3 -I{} sh -c 'node run.mjs tpb {} > logs/{}.log 2>&1'   # parallel
```

A reel takes about as long as the video itself plus post-processing. Run batches in the background and check `logs/`.

## Writing a reel

```js
'16-my-feature': {
  title: 'Short title card', subtitle: 'One line of why it matters',
  steps: async (d) => {
    const { page, r } = d;                      // page = Playwright, r = testreel recorder
    await d.point(locator, 'Label', { side: 'up' });   // ring + label + cursor move, holds 2.6s
    await d.clear();
    await d.say('Caption', 'optional sub line');        // holds by reading time
    await r.click(locator); await r.type(locator, 'text', { delay: 60 });
    await d.tab('Tuning');                              // configure-page tab
  },
},
```

Helpers: `point(sel|locator, label, {side:'down'|'up', hold, move})`, `clear()`, `say(text, sub, holdMs)`, `hush()`, `tab(name)`, `wait(ms)`. Recorder `r`: click, type, fill, hover, scroll, zoom, keyboard, navigate, wait, screenshot. Overlays are re-injected on every navigation, including cross-origin; the title card shows once per recording. Options on a reel: `chromeUrl` (text in the fake address bar), `kicker`, `speed`, `storageState`.

## Application recordings (desktop apps, not just webapps)

A reel is a Playwright page, so any app whose UI is web tech can be recorded. Three modes, in order of preference:

1. **Renderer with its fixture bridge (default for GODsend 360 and GODsend PSP).** Both apps ship their React renderer with a `?mock=1` bridge that fakes the console, library and downloads. Serve it with vite and point the reel at it: `cd ~/Code/godsend-psp/app && npx vite --port 5191`, `cd ~/Code/godsend-360/src/electron-app && npx vite --port 5192`. Real UI, deterministic demo data, no device, no real accounts. The reels set `HOST`, `PATH = '/?mock=1'`, `VIEWPORT = {width:1280,height:800}` and `chromeUrl` = the window title ("GODsend PSP") so the frame reads as an app window, not a browser.
2. **Packaged Electron app (`NATIVE=1 node run.mjs godsend360`).** `lib/reel.mjs` launches it with `_electron.launch` + `recordVideo`, takes `firstWindow()`, and the same overlay and steps apply. The reel file exports `NATIVE = {type:'electron', cwd, args}`. Needs the app built (`npm run renderer:build && npm run tsc`). This shows the real console/library, so only use it with a throwaway profile and never with real credentials. Not smoke-tested yet; if a launch fails, fall back to mode 1.
3. **Tauri or native-UI apps (no Playwright driver).** Use mode 1 if a mock bridge exists. Otherwise record the screen with `ffmpeg -f avfoundation -i "<screen>:none"` or QuickTime, then run it through testreel's background and chrome post-processing by hand; this needs Screen Recording permission for the terminal (`screencapture` fails with "could not create image from display" without it) and gives no overlay, so add captions in post. Prefer adding a `?mock=1` bridge to the app instead.

Rules specific to apps: the fixtures are fake, say so in the guides intro ("The demo data shown is fixture data, no real console is involved"); never click Download, Install, Push, Delete, Restore or Eject in a reel even on fixtures, point at them; nav items in these apps are often icon buttons with only `aria-label`/`title`, so use `getByRole('button', {name, exact:true})` (360) or a regex on the label (PSP: `/Library$/`, the icon letters are part of the text); the 360 app opens on Console or Library depending on connection state, so branch on a visible element instead of assuming the start view.

## Overlay engineering notes

The overlay (captions, ring, title card) lives in a shadow root on a host appended to `<html>`, styled with `adoptedStyleSheets`, and a MutationObserver re-attaches it. Do not go back to a `<style>` element or head injection: Game Debrid's CSP blocks inline styles and Next/React 19 hydration strips unknown head children, both of which left captions unstyled.

## Rules for good reels

1. **Verify every claim on screen against the real UI or the README.** Probe first (screenshot each state), never caption from memory. A wrong caption is worse than none.
2. **Never enter a real key, token or password.** Type obvious fakes (`demo-key-not-real`). Never click Save/Install/Update/Delete controls that persist something on a live host; configure reels are read-only (stateless install URLs only).
3. **Never press play** on adult content in Stremio; stop at the stream list. Posters load lazily, a long wait after navigation shows more of them, keep waits short where thumbnails would show.
4. Hold times: `point` about 2.4-3.6s, `say` is auto from text length. A feature reel lands around 30-70s.
5. Public-facing copy in captions: no em or en dashes (use `-`, commas, colons), no other Stremio addon names as sources.
6. Selectors: prefer `getByText(..., {exact:true})`, `getByRole`, `getByPlaceholder`. All configure tab panels are in the DOM at once, so scope to what is visible. Collapsed cards open on click of the title. Stremio Web uses hashed class names and non-button divs: match on text, `a[href^="#/detail/"]`, `a[href="#/discover"]`.
7. `say()` hides itself after its hold; `point()` stays until `clear()`. Always `clear()` before switching tab or navigating, or a stale ring lands on a hidden element.
8. `point()` only targets the visible match (all tab panels are in the DOM), so do not add `.first()` to its locator.
9. Selectors: cinema accordions toggle by clicking the section heading (`getByRole('heading', {name: /^Stream rules/})`), labels are CSS-uppercased so match with `{exact:false}`, language group rows are `flag + name` so match non-exact, and the install URL is `#preview` (horror: ends in `/configure`, swap for `/manifest.json`). Stremio keeps one addon per manifest id, so a full Absolute Cinema and a catalog-only Absolute Cinema replace each other, but the standalone Horror addon coexists with it.
10. The language pill is a transparent native `<select id="uiLanguage">`: drive it with `selectOption`, the dropdown never renders in the video.
11. Posters on `web.stremio.com` are shown clear. `blurPosters: true` on a reel blurs every catalog image if a shareable version is ever wanted.
12. After each batch, extract frames and look at them before reporting: `ffmpeg -ss 12 -i out.mp4 -frames:v 1 f.png` (ffmpeg-static ships in `node_modules`). Check the title card, the spotlight placement, caption overlap, and that no run logged `FAILED`.

## Stremio install flow (no account needed)

Stremio Web works as a guest and accepts add-ons. Route: configure page, pick preset, Generate Install URLs, read the manifest URL from the install page, `navigate` to `https://web.stremio.com/#/addons`, click Add addon, fill URL, Add, Install, then Discover icon, type dropdown (`Movie`) and pick `Tube`, click a poster, the stream list is on the detail page. The install page's "Stremio Web" link only opens raw manifest JSON, do not use it. "Install in Stremio" is a `stremio://` deep link; point at it, do not click it.

## Guides pages (where the videos are shown)

| Addon | Data file | Page | Linked from |
|---|---|---|---|
| Game Debrid | `game-search-frontend/lib/guides.json` | `/guides` (`app/guides/page.tsx`, CSP `media-src` in `middleware.ts`) | the footer (there is no Help modal) |
| GODsend 360 | `godsend-360/docs/guides.json` | `docs/video-guides.html` on the GitHub Pages site (`node build-site.mjs godsend360`) | the site nav, the sitemap and the README |
| GODsend PSP | `godsend-psp/docs/guides.json` | `docs/video-guides.html` (`node build-site.mjs godsendpsp`) | the site nav, the guides section and the README |
| TPB | `tpb-stremio-addon/src/lib/guides.json` | `/guides` (`src/components/guides/GuidesPage.tsx`) and the docs site `docs/guides.html` (`node scripts/build-guides-html.mjs`) | the Help modal ("Watch the video guides") and the docs footer |
| Cinema | `absolute-cinema/lib/guides.cinema.json` | `/guides` (`lib/guidesPage.ts`, route in `serverless.ts`) | the Help modal |
| Horror | `absolute-cinema/lib/guides.horror.json` | `/guides` on the horror host | the Support and community section |

Static sites (360, PSP) have no e2e; check the generated page opens and every video URL returns 200. Tests: `e2e/tests/guides.spec.js` in the web repos, `src/lib/guides.test.ts` (TPB) and `test/guidesPage.test.ts` (cinema). Run the page locally and point the e2e at it with `E2E_URL_TPB`, `E2E_URL_CINEMA`, `E2E_URL_HORROR`.

## When a feature is added, changed or removed (mandatory)

Any change to user-visible configure-page behaviour updates the guides in the same change:

1. **New feature**: add a reel in `<addon>/reels.mjs` and an entry in `guides/<addon>.mjs` under the right section (or a new section if it is a new area). Title, 2 to 3 sentence walkthrough, numbered steps.
2. **Changed feature**: re-record the affected reel; update its copy if the behaviour or labels moved.
3. **Removed feature**: delete the reel and its guides entry.
4. Record (`node run.mjs <addon> <prefix>`), then look at extracted frames. Never trust a green run alone.
5. `node publish.mjs <addon>`; for TPB also `node scripts/build-guides-html.mjs`.
6. Run the guides tests and add a CHANGELOG line ("Video guides: ...").
7. If the guided setup, presets or install flow changed, re-record `01-install-and-streams` and the guided-setup reel too.

## Adding another app

Copy `gamedebrid/` (webapp) or `godsendpsp/` (desktop app) to `<app>/`, change `HOST`, `THEME` (add a palette to `THEMES` in `lib/reel.mjs`) and the reel list, add `guides/<app>.mjs`, run with `node run.mjs <app>`. The lib is app-agnostic.

## Reel inventory

**TPB** (15): 01 install and streams, 02 presets, 03 sources, 04 debrid, 05 metadata and favorites, 06 MediaFlow/Torznab/Zilean/Usenet, 07 P2P, 08 live cams, 09 tubes, 10 tuning, 11 add-on name and split, 12 account and permanent URL, 13 guided setup, 14 language, 15 contribute.

**Cinema** (17): 01 install and streams, 02 stream sources, 03 stream rules, 04 picture, 05 sound, 06 subtitles, 07 catalog rows, 08 extra rows and lists, 09 live TV, 10 debrid, 11 indexers and servers, 12 usenet, 13 posters, 14 install ticket, 15 guided setup, 16 account, 17 language.

**Game Debrid** (3): 01 browse and find, 02 platforms, 03 sign in and debrid.

**GODsend 360** (5): 01 Xbox library, 02 browse and download, 03 toolbox, 04 console, 05 settings.

**GODsend PSP** (12): 01 console, 02 library, 03 store, 04 downloads, 05 files, 06 homebrew, 07 music, 08 video, 09 images, 10 saves, 11 mods, 12 settings.

**Horror** (12): 01 install and streams, 02 what to include, 03 subgenres, 04 by decade, 05 franchises, 06 special catalogs, 07 streaming services, 08 region and language, 09 home row order, 10 your build, 11 account, 12 language and community.

## Specifications (the user's refinements, newest last)

Keep this list current: when the user gives a style, pacing, content or workflow preference for demo videos, add it here as one line with the date, then apply it to `lib/reel.mjs` or the reels and re-record.

- 2026-11-15: No blurred covers. Posters stay clear in every video (`blurPosters` defaults to false).
- 2026-11-15: Every video is published to R2 and shown on a guides page per addon (TPB: `/guides` plus the docs site; Cinema and Horror: `/guides`), each section titled, each video with a short walkthrough. Videos are kept current: any feature change re-records or adds its video (see the mandatory rule above).
- 2026-11-15: Desktop applications (GODsend 360, GODsend PSP) and Game Debrid get the same treatment: per-feature reels, R2, a guides page on the site, a link from the app's site and README.
- 2026-11-15: One separate video per feature. Each has animations, on-screen messaging and pointers highlighting the feature. Output to `~/Downloads/tpb-demos` for review. Include a full install-to-streams video.
