# Chart: GODsend-360

Xbox 360 library manager (Electron + React renderer, Go backend, static docs site). Light unslop pass only.

## Accepted exceptions (brand / design language)
- Neon green "laser" accents, status dot with soft glow in MainNav/HomePage: the connection indicator is functional and on-brand.
- Tracked-caps mono eyebrows and section labels in the app and docs: part of the type system.
- Skeleton shimmer (`animate-pulse` gradients) and the "checking" pulse: loading feedback; global `prefers-reduced-motion` rule in index.css already neutralises animation (detector reduced-motion hits are false positives).
- Docs hero stats and mobile-menu button (flagged icon-tile) are real content / a plain control.
- `docs/site/tailwind.css`, `renderer-dist/`, built `index-*.js/css`: generated, not edited.
- Pre-existing `tsc` errors (MainNav home comparison, SettingsPage hasTorPassword) are unrelated to this pass.
