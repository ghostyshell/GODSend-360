# FlareSolverr bundle

`scripts/download-flaresolverr.js` downloads the official FlareSolverr release
for this platform into this folder. The Electron app starts it on a free local
port and points the backend at it (`GODSEND_CF_SOLVER_URL`).

- Official releases cover **Linux x64** and **Windows x64**. There is no macOS
  build; on macOS point Settings -> dlxbgame.com source -> solver URL at a
  FlareSolverr/Byparr instance on your LAN (Byparr works too).
- Nothing here is committed (except this file). The app skips the solver when
  the binary is absent and the backend then only uses direct HTTP.

Fetch it:

```bash
npm run solver:download
```
