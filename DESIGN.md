# GODsend 360 interface

The desktop app is a console workbench, not a landing page. It has to make a
technical job legible: see the backend, find a title, decide how it installs,
then watch the transfer happen. Everything below serves that.

## Direction

**Console atelier.** Near-black ink, one neon-green laser note, magenta and cyan
as the secondary spectrum, hairline glass and monospaced data. It should read as
a precision instrument for an Xbox 360, not as a game storefront.

Three rules keep it usable:

1. **One job per page.** The top bar is the map. A page shows only what belongs
   to that stage.
2. **State is always visible.** The status chip, queue badge and page title say
   what the console is doing without a hunt.
3. **Motion explains, never decorates.** Entrances and page changes are the only
   places it appears, and it is removed under `prefers-reduced-motion`.

## Shell

```
topbar       eyebrow · page title · console status · navigation icons
workspace    one scrolling page
```

The app runs in a 1440 x 900 reference window and stays legible at 1000 x 760.
The Library grid fills to the window width with 110px minimum tiles. There is no
side rail; navigation is a single icon row and the Toolbox dropdown.

## Tokens

All colour lives in `src/electron-app/renderer/index.css` under `:root`. The Tailwind
theme reads the same values from `tailwind.config.js`. Use the tokens for colour;
white-alpha hairlines (`border-white/[0.09]`) and gradient stops are the only
exceptions.

| Token | Value | Use |
| --- | --- | --- |
| `--background` | `#050912` | app background |
| `--surface` | translucent `#080c15` | panels |
| `--line` / `--line-strong` | white 7% / 14% | hairlines |
| `--text` / `--text-dim` / `--text-faint` | `#eef2f9` / `#a7b1c4` / `#78849a` | type ramp |
| `--laser` | `#34e37a` | primary accent, active nav, focus |
| `--laser-2` | `#67e8f9` | eyebrows, secondary accent |
| `--violet` | `#b79bff` | tertiary note |
| `--brass` | `#f2c078` | warning and the one warm note |
| `--grad-laser` | cyan to green | primary buttons, progress |
| `--good-soft` / `--warn-soft` / `--bad-soft` | tinted 12% | state surfaces |

Type: **Orbitron** for page titles and section heads, **Chakra Petch** for body
copy at 13px root, and **JetBrains Mono** for identifiers, paths, counts and
every number that changes. Body copy never goes below 11px; technical data may
be 9.5px mono.

Shape: 14px panels, 10px controls, 999px pills. Spacing follows 4, 8, 12, 16,
20, 26, 40. There is no separate spacing scale variable; use the rhythm.

## Components

- **Primary button.** Laser gradient with dark ink text and a light sheen on
  hover, lift on press. One per view, at most.
- **Default button.** Hairline border on a 3% white fill, hover brightens the
  border. Everything secondary.
- **Panel.** `.panel` / `.card-surface`: translucent surface, hairline, inset
  highlight, 14px blur. Hover only brightens the border.
- **Pill.** Source and platform tabs. Active is solid green with dark text.
- **Chip.** Rounded, dot plus word. Colour carries state and the word is the
  non-colour cue.
- **Row.** Store rows are focusable buttons with a left laser marker on hover.
- **Empty state.** `.empty-state`: dashed container, icon, one sentence of what
  to do. No dead ends.
- **Progress.** Gradient fill with a sheen only while a job runs, always paired
  with a number.

## Motion

| Moment | Motion | Duration |
| --- | --- | --- |
| Page change | fade and rise 10px | 340ms |
| Panel entrance | fade and rise 10px, 30ms stagger | 420ms |
| Toast / menu | rise and scale from 96% | 380ms |
| Primary hover | sheen sweep | 700ms |
| Active press | translate 1px | 160ms |
| Cover hover | lift 4px, 3D flip | 200ms / 350ms |
| Progress | width ease plus sheen | 260ms / 1.8s loop |

Easing is `cubic-bezier(.22,.8,.24,1)` for state and
`cubic-bezier(.16,1,.3,1)` for entrances. Animate transform and opacity only.
Never animate width, height, top or left. The one width animation is progress
fill, which is a single element and cheap.

`prefers-reduced-motion: reduce` shortens every animation to 0.001ms and stops
the progress sheen.

## Accessibility

- Every control has a visible `:focus-visible` ring in `--laser-2`.
- Navigation buttons carry `aria-label` and `aria-current="page"`.
- The console status chip is `role="status"` with `aria-live="polite"`.
- State is never colour alone: the status chip pairs a dot with a word.
- Icon-only controls keep a `title` and an `aria-label`.
- Text contrast targets WCAG AA against the panel surface.

## Preview and review

The UI can be laid out and clicked without a console or a backend.
`src/electron-app/renderer/mock.ts` answers every bridge channel with a fixture
Aurora library and only loads in the dev server:

```sh
cd src/electron-app
npm run renderer:dev     # http://localhost:1420/?mock=1
```

The fixture holds 24 games with generated box art, a Minerva store list, a live
queue job and an FTP listing. Use a 1440 x 900 viewport for the reference layout
and 1000 x 760 for the compact one.

Before shipping a UI change:

1. `npx vite build` (production bundle).
2. `npx playwright test tests/mock-screenshots.spec.ts` to recapture the site
   screenshots and prove the fixture still renders covers.
3. Check keyboard order, focus rings and the reduced-motion pass.
4. Keep copy short and specific. No dashes used as punctuation, no promises the
   code does not keep.

## Skills

The design work follows the repo skill set installed under `~/.claude/skills`
(OpenCode symlinks under `~/.config/opencode/skills`). The routing that matters
for this app:

| Task | Skill |
| --- | --- |
| Direction, palette, type, product guidance | `ui-ux-pro-max` |
| Distinctive interface, anti-slop | `frontend-design` |
| Motion timing and choreography | `motion-design`, `emil-design-eng` |
| Component polish | `make-interfaces-feel-better`, `better-ui` |
| Accessibility audit | `accesslint-audit`, `better-accessibility` |
| Whole-interface review | `better-interface`, `ui-review` |
| Copy | `better-writing`, `humanizer` |

See `docs/design-skills.md` for the install and refresh commands.
