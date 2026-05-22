import { app, protocol } from "electron";
import { getAppDataDir } from "./services/appDataPath";

// Lock in the app-data directory before any other module reads userData.
// Portable Windows builds default to <exe-dir>/godsend-data; everything else
// stays on the OS platform default unless the user picked an override.
app.setPath("userData", getAppDataDir());

protocol.registerSchemesAsPrivileged([
  {
    scheme: "godsend-aurora",
    privileges: {
      standard:        true,
      secure:          true,
      supportFetchAPI: true,
      corsEnabled:     true,
    },
  },
]);

import { bootstrapApp } from "./app/bootstrap";
bootstrapApp();
