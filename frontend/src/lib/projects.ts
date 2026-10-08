/** Mirrors internal/projects.Slug so the UI can preview the branch Orole will create. */
export function slug(name: string): string {
  let out = "";
  let dash = true;
  for (const ch of name.trim().toLowerCase()) {
    if ((ch >= "a" && ch <= "z") || (ch >= "0" && ch <= "9")) { out += ch; dash = false; }
    else if (".-_ /".includes(ch) && !dash) { out += "-"; dash = true; }
  }
  out = out.replace(/^-+|-+$/g, "");
  if (out.length > 48) out = out.slice(0, 48).replace(/-+$/g, "");
  return out;
}

export const branchFor = (name: string): string => (slug(name) ? `orole/${slug(name)}` : "");

/** "3 min ago"-style label for a unix-seconds timestamp. */
export function ago(unix: number, now = Date.now()): string {
  if (!unix) return "never";
  const s = Math.max(0, Math.floor(now / 1000 - unix));
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return `${Math.floor(s / 86400)} d ago`;
}
