import { app, protocol } from "electron";

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
