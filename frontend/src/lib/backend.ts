import { isMyGo } from "mygo-runtime";

/** True when running inside the MyGo desktop window (daemon-backed). */
export const inDesktop: boolean = (() => {
  try {
    return isMyGo();
  } catch {
    return false;
  }
})();

/**
 * Plain-browser preview (vite dev / Playwright). There is no daemon: only UI
 * preferences persist, in localStorage. Integrations show their setup state;
 * nothing pretends to be real data.
 */
export const previewKey = "orole.preview.settings";
