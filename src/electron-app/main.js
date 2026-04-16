"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
const electron_1 = require("electron");
electron_1.protocol.registerSchemesAsPrivileged([
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
const bootstrap_1 = require("./app/bootstrap");
(0, bootstrap_1.bootstrapApp)();
