const { app, protocol } = require("electron");

protocol.registerSchemesAsPrivileged([
  {
    scheme: "godsend-aurora",
    privileges: {
      standard: true,
      secure: true,
      supportFetchAPI: true,
      corsEnabled: true,
    },
  },
]);

const { bootstrapApp } = require("./app/bootstrap");
bootstrapApp();
