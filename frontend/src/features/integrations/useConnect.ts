import { useState } from "react";
import { MCP } from "@/mygo";
import { errorText } from "@/features/mcp/shared";

export interface ConnectOptions {
  /** Loopback desktop-app server: registered without auth, no sign-in. */
  local?: boolean;
  /** User-supplied OAuth client (Google). The secret goes to the credential store, never back to the UI. */
  client?: { id: string; secret: string; scopes: string };
}

/**
 * Registers an MCP server and signs in to it. Once the first connection attempt has settled, any
 * server that is not connected gets authorization started straight away (the shell opens the
 * browser at the vendor's consent page); a server that needs no sign-in just ends up connected.
 * A server with a user-supplied client always signs in: it answers unauthenticated requests, so
 * "connected" does not mean authorized.
 * Registration is kept when sign-in fails so the error stays inspectable on the details page.
 */
export async function connectServer(name: string, url: string, options: ConnectOptions = {}): Promise<{ id: string; signingIn: boolean }> {
  const { local, client } = options;
  const added = await MCP.add({
    name, url, adapter: "", authKind: local ? "none" : "oauth",
    oauthClientId: client?.id ?? "", scopes: client?.scopes ?? "", ...(client?.secret ? { clientSecret: client.secret } : {}),
  });
  if (local) return { id: added.id, signingIn: false };
  let state = added;
  for (let attempt = 0; attempt < 25 && state.status === "connecting"; attempt++) {
    const { promise, resolve } = Promise.withResolvers<void>();
    setTimeout(resolve, 400);
    await promise;
    state = (await MCP.list()).find((server) => server.id === added.id) ?? state;
  }
  const signingIn = client !== undefined || state.status !== "connected";
  if (signingIn) await MCP.startAuth(added.id);
  return { id: added.id, signingIn };
}

/** Busy and error state for one-click connects, keyed by whatever the caller uses to name a row. */
export function useConnect(changed: () => void) {
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  async function run(key: string, action: () => Promise<unknown>): Promise<boolean> {
    setBusy(key); setError(null);
    try { await action(); return true; }
    catch (cause) { setError(errorText(cause)); return false; }
    finally { setBusy(null); changed(); }
  }
  return { busy, error, clearError: () => setError(null), run };
}
