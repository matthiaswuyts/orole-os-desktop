import type { PluginManifest } from "@/mygo";

export const MAX_MANIFEST_BYTES = 64 * 1024;

function isObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** Shape checks make local inspection safe; the daemon performs authoritative validation. */
export function parseManifest(text: string): PluginManifest {
  if (new TextEncoder().encode(text).byteLength > MAX_MANIFEST_BYTES) throw new Error("The manifest exceeds 64 KiB. Choose a smaller JSON file.");
  let value: unknown;
  try { value = JSON.parse(text); }
  catch { throw new Error("The manifest is not valid JSON. Correct it before importing."); }
  if (!isObject(value) || value.schemaVersion !== 1) throw new Error("Expected a plugin manifest object with schemaVersion 1.");
  for (const key of ["id", "name", "version", "description"]) {
    if (typeof value[key] !== "string") throw new Error(`Manifest ${key} must be text.`);
  }
  const lists = [
    { key: "capabilities", max: 16, fields: ["serverId", "tool"] },
    { key: "actions", max: 32, fields: ["id", "title", "description", "serverId", "tool"] },
    { key: "commands", max: 32, fields: ["id", "title", "description", "actionId"] },
    { key: "widgets", max: 16, fields: ["id", "title", "description", "actionId"] },
  ];
  for (const { key, max, fields } of lists) {
    const items = value[key] ?? [];
    if (!Array.isArray(items) || items.length > max) throw new Error(`${key} must be an array with at most ${max} entries.`);
    for (const item of items) {
      if (!isObject(item) || fields.some((field) => typeof item[field] !== "string")) throw new Error(`Each ${key} entry must include text fields: ${fields.join(", ")}.`);
      if (key === "actions" && !isObject(item.arguments)) throw new Error("Every action must have a JSON object for arguments. Never put credentials in arguments.");
    }
    value[key] = items;
  }
  return value as unknown as PluginManifest;
}

export function pluginWidgetType(pluginId: string, widgetId: string) {
  return `plugin:${pluginId}:widget:${widgetId}`;
}
