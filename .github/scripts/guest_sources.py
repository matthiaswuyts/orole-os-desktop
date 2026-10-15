#!/usr/bin/env python3
"""Collect the corresponding source of a NixOS guest image's runtime closure into tar.zst parts and write SOURCES.md.

Inputs (made by .github/workflows/guest-sources.yml):
  --closure   `nix-store -qR <toplevel>`: every store path on the image
  --graph     `nix derivation show -r <toplevel.drv>`: the derivations that built them
  --packages  nixpkgs' packages.json for the pinned revision (meta of every package Hydra evaluates)
For every runtime store path the script finds its derivation, the derivation's sources (src, srcs, patches and vendored
dependency sets) and the package's meta.license. Sources of copyleft (GPL, LGPL, AGPL, MPL, EPL, CDDL, EUPL, OSL,
CeCILL, Sleepycat) packages and of packages whose licence cannot be determined are realised (`nix-store -r`, from
cache.nixos.org or upstream) and packed into parts below --limit; the rest are listed with their upstream URL.
"""
import argparse
import base64
import collections
import hashlib
import json
import os
import re
import subprocess
import sys

COPYLEFT = re.compile(r"(?<![A-Za-z])(A?GPL|LGPL|MPL|EPL|CDDL|EUPL|OSL|CeCILL|Sleepycat|copyleft)", re.I)
SRC_ATTRS = ("src", "srcs", "patches", "cargoDeps", "goModules", "vendorDir", "npmDeps", "pnpmDeps",
             "yarnOfflineCache", "offlineCache")
STORE = "/nix/store/"

# Packages whose licence the name lookup in packages.json cannot find (an internal attribute such as the unwrapped
# browser) but nixpkgs states it; value: (licence, nixpkgs file that says so).
OVERRIDES = {
    "chromium-unwrapped": ("BSD-3-Clause", "pkgs/applications/networking/browsers/chromium/common.nix meta.license"),
}


def run(*cmd, **kw):
    return subprocess.run(cmd, check=True, text=True, capture_output=True, **kw).stdout


def base(p):
    return p[len(STORE):] if p.startswith(STORE) else p


def name_of(store_path):
    return base(store_path).split("-", 1)[1] if "-" in base(store_path) else base(store_path)


def lic_text(lic):
    if lic is None:
        return None
    items = lic if isinstance(lic, list) else [lic]
    out = []
    for l in items:
        if isinstance(l, dict):
            out.append(l.get("spdxId") or l.get("shortName") or l.get("fullName") or "?")
        else:
            out.append(str(l))
    return " / ".join(out)


def human(n):
    for unit, size in (("GiB", 1 << 30), ("MiB", 1 << 20), ("KiB", 1 << 10)):
        if n >= size:
            return f"{n / size:.1f} {unit}"
    return f"{n} B"


def sri_to_hex(sri):
    algo, b64 = sri.split("-", 1)
    return base64.b64decode(b64).hex()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--closure", required=True)
    ap.add_argument("--graph", required=True)
    ap.add_argument("--packages", required=True)
    ap.add_argument("--toplevel", required=True)
    ap.add_argument("--release", required=True)
    ap.add_argument("--nixpkgs-rev", required=True)
    ap.add_argument("--repo-commit", required=True)
    ap.add_argument("--extra", action="append", default=[], help="name=description of an extra asset already in --out")
    ap.add_argument("--work", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--limit", type=int, default=int(1.85 * (1 << 30)))
    ap.add_argument("--split", type=int, default=int(1.9 * (1 << 30)))
    a = ap.parse_args()
    os.makedirs(a.out, exist_ok=True)
    os.makedirs(a.work, exist_ok=True)

    closure = [l.strip() for l in open(a.closure) if l.strip()]
    g = json.load(open(a.graph))
    if isinstance(g, dict) and "derivations" in g:
        g = g["derivations"]
    drvs = {}
    out2drv = {}
    for k, d in g.items():
        kb = base(k)
        drvs[kb] = d
        for o, v in (d.get("outputs") or {}).items():
            p = v.get("path") if isinstance(v, dict) else None
            if p:
                out2drv[base(p)] = kb
    # Newer `nix derivation show` formats leave out the path of fixed-output (fetched) outputs: ask the store.
    pathless = [kb for kb, d in drvs.items()
                if any(not (isinstance(v, dict) and v.get("path")) for v in (d.get("outputs") or {}).values())]
    print(f"{len(drvs)} derivations, {len(pathless)} with output paths to look up", file=sys.stderr)
    for i in range(0, len(pathless), 500):
        for kb in pathless[i:i + 500]:
            for p in run("nix-store", "-q", "--outputs", STORE + kb).split():
                out2drv[base(p)] = kb

    pk = json.load(open(a.packages))
    pk = pk.get("packages", pk)
    by_name, by_pname = {}, {}
    for attr, p in pk.items():
        meta = p.get("meta") or {}
        entry = (attr, lic_text(meta.get("license")), meta.get("homepage"))
        by_name.setdefault(p.get("name"), entry)
        if p.get("pname"):
            by_pname.setdefault(p["pname"], entry)

    env_cache = {}

    def env(kd):
        """The derivation's environment; structured-attrs derivations keep theirs in __json."""
        if kd not in env_cache:
            e = dict(drvs[kd].get("env") or {})
            if "__json" in e:
                for k, v in json.loads(e.pop("__json")).items():
                    if isinstance(v, str):
                        e.setdefault(k, v)
                    elif isinstance(v, (int, float)) and not isinstance(v, bool):
                        e.setdefault(k, str(v))
                    elif isinstance(v, list) and all(isinstance(x, str) for x in v):
                        e.setdefault(k, " ".join(v))
            env_cache[kd] = e
        return env_cache[kd]

    def drv_name(kd):
        e = env(kd)
        return e.get("name") or drvs[kd].get("name") or kd[33:-4]

    def licence_for(kd):
        e = env(kd)
        n = drv_name(kd)
        pname = e.get("pname") or re.sub(r"-[0-9].*$", "", n)
        if pname in OVERRIDES:
            l, why = OVERRIDES[pname]
            return l, f"override ({why})", None
        for key, table in ((n, by_name), (pname, by_pname),
                           (re.sub(r"^python3\.\d+-", "", pname), by_pname),
                           (re.sub(r"-unwrapped$", "", pname), by_pname)):
            if key in table:
                attr, lic, home = table[key]
                return lic, attr, home
        return None, None, None

    def is_fod(kd):
        d = drvs[kd]
        outs = d.get("outputs") or {}
        o = outs.get("out") if isinstance(outs, dict) else None
        return bool((isinstance(o, dict) and (o.get("hash") or o.get("hashAlgo") or o.get("method")))
                    or env(kd).get("outputHash"))

    def fod_info(src_base):
        kd = out2drv.get(src_base)
        if not kd:
            return None, ""
        e = env(kd)
        url = e.get("urls") or e.get("url") or ""
        if e.get("rev"):
            url = f"{url} (rev {e['rev']})"
        return kd, url.strip()

    # Group runtime paths by derivation.
    groups = collections.OrderedDict()
    loose = []
    for p in closure:
        kd = out2drv.get(base(p))
        if kd:
            groups.setdefault(kd, []).append(p)
        else:
            loose.append(p)

    rows = []
    want = {}  # source store path -> fod drv (or None)
    for kd, paths in groups.items():
        e = env(kd)
        lic, attr, home = licence_for(kd)
        srcs = []
        for k in SRC_ATTRS:
            for tok in (e.get(k) or "").split():
                if not tok.startswith(STORE):
                    continue
                top = STORE + base(tok).split("/", 1)[0]
                producer = out2drv.get(base(top))
                # A file inside another package's build output (e.g. a config file NixOS takes from systemd's
                # output) is not a source: that package is listed on its own. Sources are fixed-output
                # (fetched) paths and plain files copied into the store.
                if producer and not is_fod(producer):
                    continue
                srcs.append(top)
        srcs = list(dict.fromkeys(srcs))
        copyleft = lic is None or bool(COPYLEFT.search(lic))
        src_rows = []
        for s in srcs:
            fod, url = fod_info(base(s))
            src_rows.append({"path": s, "fod": fod, "url": url})
            if copyleft:
                want[s] = fod
        rows.append({"drv": kd, "name": drv_name(kd), "version": e.get("version", ""), "licence": lic,
                     "attr": attr, "homepage": home, "paths": paths, "srcs": src_rows, "include": copyleft})

    kernel = [r for r in rows if re.match(r"linux-\d+\.\d+(\.\d+)?$", r["name"])]
    if not kernel or not all(r["srcs"] for r in kernel):
        raise SystemExit(f"the kernel derivation has no source: {[(r['name'], r['srcs']) for r in kernel]}")
    nosrc = sorted(r["name"] for r in rows if r["include"] and r["licence"] and not r["srcs"])
    print(f"copyleft without a source of their own ({len(nosrc)}): {', '.join(nosrc)}", file=sys.stderr)

    # Realise the wanted sources.
    targets = sorted({(STORE + f) if f else s for s, f in want.items()})
    print(f"realising {len(targets)} source derivations/paths", file=sys.stderr)
    failed = set()
    for i in range(0, len(targets), 200):
        chunk = targets[i:i + 200]
        r = subprocess.run(["nix-store", "-r", "--keep-going", *chunk], text=True, capture_output=True)
        if r.returncode:
            print(r.stderr[-4000:], file=sys.stderr)
    present = {s for s in want if os.path.exists(s)}
    failed = set(want) - present
    print(f"sources present: {len(present)}, not fetched: {len(failed)}", file=sys.stderr)
    for f in sorted(failed):
        print(f"  not fetched: {f}", file=sys.stderr)
    info = {}
    if present:
        pi = json.loads(run("nix", "path-info", "--json", *sorted(present)))
        if isinstance(pi, list):
            pi = {x["path"]: x for x in pi}
        for k, v in pi.items():
            info[STORE + base(k)] = v

    def src_hash(s):
        if os.path.isfile(s):
            h = hashlib.sha256()
            with open(s, "rb") as f:
                for b in iter(lambda: f.read(1 << 20), b""):
                    h.update(b)
            return h.hexdigest(), "file"
        return sri_to_hex(info[s]["narHash"]) if info[s]["narHash"].startswith("sha256-") else info[s]["narHash"], "NAR"

    # Pack into parts.
    items = sorted(present, key=lambda s: name_of(s))
    parts, cur, cur_size = [], [], 0
    big = []
    for s in items:
        size = info[s]["narSize"]
        if size > a.limit:
            big.append([s])
            continue
        if cur and cur_size + size > a.limit:
            parts.append(cur)
            cur, cur_size = [], 0
        cur.append(s)
        cur_size += size
    if cur:
        parts.append(cur)
    parts += big
    where = {}
    part_files = []
    for i, members in enumerate(parts, 1):
        fn = f"{a.release}-sources-part{i:02d}.tar.zst"
        dest = os.path.join(a.out, fn)
        listfile = os.path.join(a.work, f"part{i:02d}.list")
        with open(listfile, "w") as f:
            f.write("\n".join(base(s) for s in members) + "\n")
        subprocess.run(f"tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@1 -C /nix/store "
                       f"-T {listfile} -cf - | zstd -q -T0 -12 -o {dest}", shell=True, check=True)
        files = [fn]
        if os.path.getsize(dest) > a.split:
            subprocess.run(["split", "-b", str(a.split), "-d", "-a", "2", dest, dest + "."], check=True)
            os.remove(dest)
            files = sorted(x for x in os.listdir(a.out) if x.startswith(fn + "."))
        for s in members:
            where[s] = " + ".join(files)
        part_files += files

    # Manifest.
    sums = {}
    for fn in part_files + [x.split("=", 1)[0] for x in a.extra]:
        p = os.path.join(a.out, fn)
        h = hashlib.sha256()
        with open(p, "rb") as f:
            for b in iter(lambda: f.read(1 << 20), b""):
                h.update(b)
        sums[fn] = (h.hexdigest(), os.path.getsize(p))

    rel = f"https://github.com/rayorole/orole-os-desktop/releases/download/{a.release}/"
    L = [f"# Corresponding source for {a.release}\n",
         f"The image (`vmlinuz`, `initrd`, `cmdline`, `base.qcow2`) is NixOS built from `guest/flake.nix` of "
         f"https://github.com/rayorole/orole-os-desktop at commit `{a.repo_commit}` (tag `{a.release}`) with nixpkgs "
         f"revision `{a.nixpkgs_rev}` (https://github.com/NixOS/nixpkgs/tree/{a.nixpkgs_rev}). Its system is "
         f"`{a.toplevel}` (the `init=` of `cmdline`); the disk holds exactly that store path's runtime closure "
         f"({len(closure)} store paths), and the kernel and initrd are part of it.\n",
         "Every package's build recipe is in nixpkgs at that revision; with it and the flake files the image "
         "rebuilds bit-for-bit where nixpkgs is reproducible (`nix build ./guest#image`, see "
         "`docs/development/guest-image.md`).\n",
         "Sources of packages under a copyleft licence (GPL, LGPL, AGPL, MPL, EPL, CDDL, EUPL, OSL, CeCILL, "
         "Sleepycat; by nixpkgs `meta.license`, dual licences included) and of packages whose licence could not be "
         "determined are attached: each source store path (the upstream tarball or checkout nixpkgs fetched, "
         "and the patches it applies) is inside the part named in the table, at its `/nix/store` name. Sources of "
         "packages under permissive licences only are not attached; their upstream URL is listed. "
         "SHA-256 is the file hash for a source file and the NAR hash (as `nix path-info` reports it) for a "
         "source directory.\n",
         "## Assets\n", "| Asset | Size | SHA-256 | Contents |", "|---|---|---|---|"]
    for fn in part_files:
        h, n = sums[fn]
        L.append(f"| [{fn}]({rel}{fn}) | {human(n)} | `{h}` | source store paths (tar, zstd) |")
    for x in a.extra:
        fn, desc = x.split("=", 1)
        h, n = sums[fn]
        L.append(f"| [{fn}]({rel}{fn}) | {human(n)} | `{h}` | {desc} |")
    if any(len(where[s].split(" + ")) > 1 for s in where):
        L.append("\nParts named `*.tar.zst.NN` are one archive split at 1.9 GiB: `cat` them in order before "
                 "unpacking.")
    L.append("")
    gen = [r for r in rows if r["licence"] is None and not r["srcs"]]
    inc = [r for r in rows if r["include"] and r not in gen]
    exc = [r for r in rows if not r["include"]]
    L += ["## Attached (copyleft or licence unknown)\n",
          "| Package | Version | Licence | Runtime store paths | Source | SHA-256 | Part |", "|---|---|---|---|---|---|---|"]
    missing = []
    for r in sorted(inc, key=lambda r: r["name"].lower()):
        lic = r["licence"] or "unknown (not in packages.json)"
        rp = "<br>".join(f"`{base(p)}`" for p in r["paths"])
        if not r["srcs"]:
            L.append(f"| {r['name']} | {r['version']} | {lic} | {rp} | no source of its own (built from other "
                     f"derivations here and nixpkgs) | | |")
            continue
        for j, s in enumerate(r["srcs"]):
            head = (r["name"], r["version"], lic, rp) if j == 0 else ("", "", "", "")
            p = s["path"]
            if p in present:
                h, kind = src_hash(p)
                hs, part = f"`{h}` ({kind})", where[p]
            else:
                hs, part = "", "**not attached: could not be fetched**"
                missing.append((r["name"], p, s["url"]))
            L.append(f"| {' | '.join(head)} | `{base(p)}`<br>{s['url'] or 'local file in nixpkgs'} | {hs} | {part} |")
    L.append("")
    if missing:
        L += ["### Sources that could not be fetched\n"]
        L += [f"- {n}: `{p}` {u}" for n, p, u in missing]
        L.append("")
    L += ["## Not attached (permissive licences)\n", "| Package | Version | Licence | Upstream source |", "|---|---|---|---|"]
    for r in sorted(exc, key=lambda r: r["name"].lower()):
        urls = "<br>".join(s["url"] for s in r["srcs"] if s["url"]) or (r["homepage"] or "")
        if isinstance(urls, list):
            urls = " ".join(urls)
        L.append(f"| {r['name']} | {r['version']} | {r['licence']} | {urls} |")
    L.append("")
    if gen:
        L += ["## Generated by NixOS and nixpkgs\n",
              "Derivations that are not packages (configuration files, units, wrappers, scripts and the system "
              "itself) and have no upstream source: they are generated from the NixOS modules and nixpkgs functions "
              "in the nixpkgs tree above (MIT) and `guest/`.\n"]
        L += [f"- `{base(p)}`" for r in sorted(gen, key=lambda r: r["name"].lower()) for p in r["paths"]]
        L.append("")
    if loose:
        L += ["## Store paths without a derivation\n",
              "Files copied into the store verbatim (configuration and scripts from nixpkgs or `guest/`); their "
              "source is the file itself, in nixpkgs or this repository.\n"]
        L += [f"- `{base(p)}`" for p in loose]
        L.append("")
    L.append("Generated by `.github/workflows/guest-sources.yml` (`.github/scripts/guest_sources.py`).")
    with open(os.path.join(a.out, "SOURCES.md"), "w", encoding="utf-8", newline="\n") as f:
        f.write("\n".join(L) + "\n")
    print(f"included packages: {len(inc)}, omitted: {len(exc)}, missing sources: {len(missing)}, parts: {part_files}",
          file=sys.stderr)
    unknown = sorted({r["name"] for r in inc if r["licence"] is None and r["srcs"]})
    print("licence unknown (attached): " + ", ".join(unknown), file=sys.stderr)


if __name__ == "__main__":
    main()
