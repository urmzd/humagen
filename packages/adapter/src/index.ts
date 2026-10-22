/**
 * The one adapter between the platform API and the shared UI model. It has no React, no DOM and no
 * Slack in it: the platform's wire shapes go in, humagen's model comes out, and
 * humagen's actions go back as platform calls.
 */
export * from "./fold.ts";
export * from "./home.ts";
export * from "./mail.ts";
export * from "./question.ts";
export * from "./run.ts";
