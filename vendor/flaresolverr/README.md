# FlareSolverr bundle

The Electron app starts a bundled FlareSolverr on a free local port and points
the backend at it (`GODSEND_CF_SOLVER_URL`).

- **Linux x64 / Windows x64:** `npm run solver:download` fetches the official
  release into this folder.
- **macOS arm64 / x64:** built from FlareSolverr 3.5.2 with PyInstaller and the
  matching Chrome for Testing build. Place the resulting `flaresolverr` +
  `_internal/` here before packaging that DMG.
- **Linux arm64:** built by `.github/workflows/build-flaresolverr-arm64.yml`
  (Debian Chromium + chromedriver, PyInstaller). Download the workflow artifact
  and extract it here before packaging the arm64 AppImage.

Nothing here is committed except this file. The app reuses an already-running
solver on `127.0.0.1:8191`/`:8192` instead of starting a second one. If no
bundle is present, the backend only uses direct HTTP; set the solver URL in
Settings for a LAN FlareSolverr/Byparr.

Fetch the official bundle:

```bash
npm run solver:download
```
