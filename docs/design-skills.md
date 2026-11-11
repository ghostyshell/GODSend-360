# UI design skills

The interface work on this repo is guided by a user-wide skill set rather than
vendored copies. The skills live at `~/.claude/skills/` (OpenCode symlinks at
`~/.config/opencode/skills/`) so several repos share one install. The design
decisions they produced are committed in [DESIGN.md](../DESIGN.md); the token
values are in
[src/electron-app/renderer/index.css](../src/electron-app/renderer/index.css).

## Routing

| Task | Skill | Why |
| --- | --- | --- |
| Choose direction, palette, type, layout | `ui-ux-pro-max` | Product-type and style rules, contrast and motion checks |
| Avoid a templated look | `frontend-design`, `design-taste-frontend` | Aesthetic direction and anti-slop constraints |
| Motion timing, easing, choreography | `motion-design`, `emil-design-eng` | Durations, easing, restraint |
| Component and surface polish | `make-interfaces-feel-better`, `better-ui` | Radius, shadows, optical alignment, hover states |
| Typography, layout, colour, copy passes | `better-typography`, `better-layout`, `better-colors`, `better-writing` | Focused reviews |
| Keyboard, focus, ARIA, WCAG 2.2 | `better-accessibility`, `accesslint-audit` | Audit and remedy |
| Whole-interface critique | `better-interface`, `ui-review` | Ranked findings |
| Remove AI writing patterns | `humanizer` | Copy that sounds written by a person |

A practical order for substantive UI work: direction (`ui-ux-pro-max`), then
implementation, then motion (`motion-design`), then review
(`better-interface` plus `accesslint-audit`).

## Refresh

Refresh the user-wide install when a skill updates. The installer script and
the canonical list live outside this repo, in the shared `~/Code/scripts`
toolkit (`install-frontend-skills.sh`, `install-ui-skills.sh`,
`frontend-skills-section.md`).

## Repo rules

1. Read [DESIGN.md](../DESIGN.md) before changing the shell, tokens or motion.
2. Use the token variables for colour. White-alpha hairlines and gradient stops
   are the only exceptions.
3. Every interactive element needs hover, active, focus-visible and disabled
   states.
4. Motion is transform and opacity only, with a reduced-motion path.
5. Preview with the fixture (`npm run renderer:dev`, then `?mock=1`) and
   recapture the site screenshots before shipping.
