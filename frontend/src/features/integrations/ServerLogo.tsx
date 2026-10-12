import type { ServerState } from "@/mygo";
import { BrandLogo } from "./BrandLogo";
import { DIRECTORY, serverFor } from "./directory";

/** A registered server's company logo, found by its URL in the directory; custom servers get the plug. */
export function ServerLogo({ server, className }: { server: ServerState; className?: string }) {
  const entry = DIRECTORY.find((connector) => serverFor(connector, [server]));
  return <BrandLogo logo={entry?.logo} className={className} />;
}
