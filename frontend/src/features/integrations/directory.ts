import {
  siCloudflare, siGithub, siIntercom, siLinear, siMixpanel, siNeon, siNetlify, siNotion, siPaypal,
  siPrisma, siRailway, siSentry, siStripe, siSupabase, siVercel, siZapier, type SimpleIcon,
} from "simple-icons";
import type { ServerState } from "@/mygo";

export const CATEGORIES = ["Developer", "Productivity", "Finance", "Analytics"] as const;
export type Category = (typeof CATEGORIES)[number];

export interface Connector {
  id: string;
  name: string;
  description: string;
  /** A sentence or two for the details page. */
  about: string;
  category: Category;
  icon: SimpleIcon;
  /** Shown first, before the categories. */
  featured?: boolean;
  /** Remote MCP endpoint (Streamable HTTP, OAuth). GitHub has none: it uses the local gh CLI integration. */
  url?: string;
  native?: "github";
}

/**
 * Official remote MCP servers. Each URL was checked against the live server: it answers 401 with
 * RFC 9728 protected-resource metadata and its authorization server accepts dynamic client
 * registration, so Orole's OAuth flow can connect with nothing more than a click.
 * Servers that need a pre-registered client (Slack, Render, Asana, ...) are left to "Custom connector".
 */
export const DIRECTORY: Connector[] = [
  { id: "github", name: "GitHub", description: "Search repos and PRs", about: "Search repositories and code, read issues and pull requests, check CI, and work with your notifications through the GitHub CLI you are already signed in with.", category: "Developer", icon: siGithub, featured: true, native: "github" },
  { id: "vercel", name: "Vercel", description: "Manage your deploys", about: "Manage Vercel projects and deployments: list and inspect deployments, read build and runtime logs, work with domains, DNS and environment variables, and search the Vercel documentation.", category: "Developer", icon: siVercel, featured: true, url: "https://mcp.vercel.com" },
  { id: "cloudflare", name: "Cloudflare", description: "Workers, Pages, DNS and more", about: "Work with your Cloudflare account through the Cloudflare API: Workers, Pages, DNS, storage and more.", category: "Developer", icon: siCloudflare, featured: true, url: "https://mcp.cloudflare.com/mcp" },
  { id: "notion", name: "Notion", description: "Search pages and docs", about: "Search your Notion workspace, read pages and databases, and create or update content.", category: "Productivity", icon: siNotion, featured: true, url: "https://mcp.notion.com/mcp" },
  { id: "linear", name: "Linear", description: "Track issues and projects", about: "Find, create and update Linear issues, projects and comments.", category: "Productivity", icon: siLinear, featured: true, url: "https://mcp.linear.app/mcp" },
  { id: "stripe", name: "Stripe", description: "Look up payments", about: "Look up customers, payments, subscriptions and invoices in your Stripe account.", category: "Finance", icon: siStripe, featured: true, url: "https://mcp.stripe.com" },

  { id: "netlify", name: "Netlify", description: "Manage your deploys", about: "Manage Netlify sites and deploys, and their configuration.", category: "Developer", icon: siNetlify, url: "https://mcp.netlify.com/mcp" },
  { id: "railway", name: "Railway", description: "Manage services and environments", about: "Manage Railway projects: services, deployments, variables, domains and volumes, plus logs, metrics and the Railway documentation.", category: "Developer", icon: siRailway, url: "https://mcp.railway.com" },
  { id: "supabase", name: "Supabase", description: "Databases, auth and storage", about: "Manage Supabase projects: query and migrate databases, and work with project settings.", category: "Developer", icon: siSupabase, url: "https://mcp.supabase.com/mcp" },
  { id: "neon", name: "Neon", description: "Manage Postgres databases", about: "Manage Neon Postgres projects and branches, and run SQL against them.", category: "Developer", icon: siNeon, url: "https://mcp.neon.tech/mcp" },
  { id: "prisma", name: "Prisma", description: "Manage Prisma Postgres", about: "Manage Prisma Postgres databases and run schema and migration tasks.", category: "Developer", icon: siPrisma, url: "https://mcp.prisma.io/mcp" },
  { id: "sentry", name: "Sentry", description: "Triage errors and issues", about: "Search and triage Sentry issues, errors and events across your projects.", category: "Developer", icon: siSentry, url: "https://mcp.sentry.dev/mcp" },

  { id: "zapier", name: "Zapier", description: "Run actions across 8,000 apps", about: "Run the Zapier actions you have enabled across thousands of connected apps.", category: "Productivity", icon: siZapier, url: "https://mcp.zapier.com/api/mcp/mcp" },
  { id: "intercom", name: "Intercom", description: "Search conversations and contacts", about: "Search Intercom conversations and contacts for support context.", category: "Productivity", icon: siIntercom, url: "https://mcp.intercom.com/mcp" },

  { id: "paypal", name: "PayPal", description: "Invoices, orders and payments", about: "Work with PayPal invoices, orders and payments.", category: "Finance", icon: siPaypal, url: "https://mcp.paypal.com/mcp" },

  { id: "mixpanel", name: "Mixpanel", description: "Query product analytics", about: "Query Mixpanel events, funnels and retention for your projects.", category: "Analytics", icon: siMixpanel, url: "https://mcp.mixpanel.com/mcp" },
];

/** The registered server that belongs to a directory entry, if any. */
export function serverFor(connector: Connector, servers: ServerState[]): ServerState | undefined {
  if (!connector.url) return undefined;
  const key = (url: string) => url.trim().replace(/\/+$/, "").toLowerCase();
  const want = key(connector.url);
  return servers.find((server) => key(server.url) === want);
}

/** "https://mcp.example.com/sse" -> "Example": a readable default for a custom connector's name. */
export function nameFromUrl(raw: string): string {
  let host: string;
  try { host = new URL(raw.trim()).hostname; } catch { return ""; }
  const labels = host.split(".").filter((label) => !["www", "mcp", "api"].includes(label));
  const label = (labels.length > 1 ? labels[labels.length - 2] : labels[0]) ?? host;
  return label.charAt(0).toUpperCase() + label.slice(1);
}
