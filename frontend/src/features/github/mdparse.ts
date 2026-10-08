/**
 * Small, safe GitHub-flavoured markdown parser for untrusted issue/PR/comment text.
 *
 * It produces a plain data tree (never HTML strings), so the renderer can only emit React
 * elements. Raw HTML is not interpreted: tags show up as literal text. Only http(s)/mailto links
 * survive, images are never fetched (they become a link), and HTML comments are dropped.
 */

export type Inline =
  | { t: "text"; v: string }
  | { t: "code"; v: string }
  | { t: "strong" | "em" | "del"; c: Inline[] }
  | { t: "link"; href: string; c: Inline[] }
  | { t: "br" };

export type Block =
  | { t: "heading"; level: 1 | 2 | 3 | 4 | 5 | 6; c: Inline[] }
  | { t: "p"; c: Inline[] }
  | { t: "code"; lang: string; v: string }
  | { t: "quote"; c: Block[] }
  | { t: "list"; ordered: boolean; start: number; items: { checked: boolean | null; c: Block[] }[] }
  | { t: "hr" };

const MAX_DEPTH = 6;

/** Returns a normalised URL if it is safe to link to, else null. */
export function safeUrl(raw: string): string | null {
  const s = raw.trim();
  if (!s || /[\u0000-\u001f\u007f\s]/.test(s)) return null;
  try {
    const u = new URL(s);
    return u.protocol === "http:" || u.protocol === "https:" || u.protocol === "mailto:" ? u.href : null;
  } catch {
    return null;
  }
}

export function parseInline(src: string, depth = 0): Inline[] {
  const out: Inline[] = [];
  let buf = "";
  const flush = () => { if (buf) { out.push({ t: "text", v: buf }); buf = ""; } };
  let i = 0;
  while (i < src.length) {
    const ch = src[i]!;
    if (ch === "\\" && i + 1 < src.length && /[\\`*_{}[\]()#+\-.!~<>|]/.test(src[i + 1]!)) { buf += src[i + 1]; i += 2; continue; }
    if (ch === "\n") {
      const hard = / {2,}$/.test(buf);
      buf = buf.replace(/ +$/, "");
      flush();
      if (hard) out.push({ t: "br" });
      else buf += " ";
      i++; continue;
    }
    if (ch === "`") {
      let n = 1;
      while (src[i + n] === "`") n++;
      const fence = "`".repeat(n);
      const end = src.indexOf(fence, i + n);
      if (end > 0) { flush(); out.push({ t: "code", v: src.slice(i + n, end).replace(/^ (.*) $/s, "$1") }); i = end + n; continue; }
      buf += fence; i += n; continue;
    }
    if (depth < MAX_DEPTH) {
      // images become plain links: nothing is loaded from the network.
      if ((ch === "!" && src[i + 1] === "[") || ch === "[") {
        const img = ch === "!";
        const start = i + (img ? 1 : 0);
        const close = findClose(src, start);
        if (close > 0 && src[close + 1] === "(") {
          const pend = src.indexOf(")", close + 2);
          if (pend > 0) {
            const target = src.slice(close + 2, pend).trim().split(/\s+/)[0] ?? "";
            const href = safeUrl(target);
            const label = src.slice(start + 1, close);
            if (href) {
              flush();
              const c = parseInline(img ? `image: ${label || href}` : label || href, depth + 1);
              out.push({ t: "link", href, c });
              i = pend + 1; continue;
            }
          }
        }
      }
      if (ch === "<") {
        const m = /^<((?:https?:\/\/|mailto:)[^\s<>]+)>/.exec(src.slice(i));
        const href = m && safeUrl(m[1]!);
        if (m && href) { flush(); out.push({ t: "link", href, c: [{ t: "text", v: m[1]! }] }); i += m[0].length; continue; }
      }
      const delim = ch === "*" || ch === "_" || ch === "~" ? ch : "";
      if (delim) {
        const n = src.startsWith(delim.repeat(2), i) ? 2 : 1;
        if (delim !== "~" || n === 2) {
          const tok = delim.repeat(n);
          const canOpen = src[i + n] !== undefined && !/\s/.test(src[i + n]!) && (delim !== "_" || !/\w/.test(src[i - 1] ?? " "));
          const end = canOpen ? findDelim(src, tok, i + n) : -1;
          if (end > 0) {
            flush();
            const c = parseInline(src.slice(i + n, end), depth + 1);
            out.push({ t: delim === "~" ? "del" : n === 2 ? "strong" : "em", c });
            i = end + n; continue;
          }
        }
      }
      if (ch === "h" && (src.startsWith("http://", i) || src.startsWith("https://", i)) && !/[\w]/.test(src[i - 1] ?? " ")) {
        const m = /^https?:\/\/[^\s<>]+/.exec(src.slice(i));
        if (m) {
          const trimmed = m[0].replace(/[.,;:!?)\]'"]+$/, "");
          const href = safeUrl(trimmed);
          if (href) { flush(); out.push({ t: "link", href, c: [{ t: "text", v: trimmed }] }); i += trimmed.length; continue; }
        }
      }
    }
    buf += ch; i++;
  }
  flush();
  return out;
}

function findClose(s: string, open: number): number {
  let depth = 0;
  for (let i = open; i < s.length; i++) {
    if (s[i] === "\\") { i++; continue; }
    if (s[i] === "[") depth++;
    else if (s[i] === "]" && --depth === 0) return i;
    else if (s[i] === "\n" && s[i + 1] === "\n") return -1;
  }
  return -1;
}

function findDelim(s: string, tok: string, from: number): number {
  for (let i = from; i < s.length; i++) {
    if (s[i] === "\\") { i++; continue; }
    if (s[i] === "`") { const e = s.indexOf("`", i + 1); if (e > 0) { i = e; continue; } }
    if (s.startsWith(tok, i) && i > from && !/\s/.test(s[i - 1]!) && s[i + tok.length] !== tok[0]) return i;
  }
  return -1;
}

const FENCE = /^ {0,3}(`{3,}|~{3,})\s*([\w+#.-]*)[^`]*$/;
const LIST_ITEM = /^( *)([-*+]|\d{1,9}[.)])\s+(.*)$/;

export function parseMarkdown(src: string, depth = 0): Block[] {
  const text = src.replace(/\r\n?/g, "\n").replace(/<!--[\s\S]*?-->/g, "");
  return blocks(text.split("\n"), depth);
}

function blocks(lines: string[], depth: number): Block[] {
  const out: Block[] = [];
  let para: string[] = [];
  const flushPara = () => { if (para.length) { out.push({ t: "p", c: parseInline(para.join("\n").trim()) }); para = []; } };
  let i = 0;
  while (i < lines.length) {
    const line = lines[i]!;
    if (!line.trim()) { flushPara(); i++; continue; }
    const f = FENCE.exec(line);
    if (f) {
      flushPara();
      const fence = f[1]!;
      const body: string[] = [];
      i++;
      while (i < lines.length && !new RegExp(`^ {0,3}${fence[0] === "`" ? "`" : "~"}{${fence.length},}\\s*$`).test(lines[i]!)) body.push(lines[i++]!);
      i++;
      out.push({ t: "code", lang: f[2] ?? "", v: body.join("\n") });
      continue;
    }
    const h = /^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/.exec(line);
    if (h) { flushPara(); out.push({ t: "heading", level: h[1]!.length as 1, c: parseInline(h[2]!) }); i++; continue; }
    if (/^ {0,3}([-*_])( *\1){2,}\s*$/.test(line)) { flushPara(); out.push({ t: "hr" }); i++; continue; }
    if (/^ {0,3}>/.test(line) && depth < MAX_DEPTH) {
      flushPara();
      const q: string[] = [];
      while (i < lines.length && /^ {0,3}>/.test(lines[i]!)) q.push(lines[i++]!.replace(/^ {0,3}> ?/, ""));
      out.push({ t: "quote", c: blocks(q, depth + 1) });
      continue;
    }
    const li = LIST_ITEM.exec(line);
    if (li && depth < MAX_DEPTH) {
      flushPara();
      const base = li[1]!.length;
      const ordered = /\d/.test(li[2]![0]!);
      const items: { checked: boolean | null; c: Block[] }[] = [];
      while (i < lines.length) {
        const m = LIST_ITEM.exec(lines[i]!);
        if (!m || m[1]!.length !== base || /\d/.test(m[2]![0]!) !== ordered) break;
        let first = m[3]!;
        let checked: boolean | null = null;
        const task = /^\[([ xX])\]\s+(.*)$/.exec(first);
        if (task) { checked = task[1] !== " "; first = task[2]!; }
        const body = [first];
        i++;
        while (i < lines.length) {
          const l = lines[i]!;
          if (!l.trim()) { // blank line continues the item only if followed by indented text
            if (i + 1 < lines.length && /^ {2,}\S/.test(lines[i + 1]!) && (lines[i + 1]!.search(/\S/) > base)) { body.push(""); i++; continue; }
            break;
          }
          const ind = l.search(/\S/);
          if (ind > base) { body.push(l.slice(Math.min(ind, base + 2))); i++; continue; }
          break;
        }
        items.push({ checked, c: blocks(body, depth + 1) });
      }
      out.push({ t: "list", ordered, start: ordered ? Number.parseInt(li[2]!, 10) : 1, items });
      continue;
    }
    para.push(line);
    i++;
  }
  flushPara();
  return out;
}
