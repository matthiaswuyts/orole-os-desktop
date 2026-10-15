#!/usr/bin/env python3
"""Collect the corresponding source of a QEMU for Windows zip into release assets and write SOURCES.md.

Input: the JSON of qemu_dll_sources.py, the QEMU source tarball already built by the workflow, and the unpacked zip.
Output (--out): one source archive per copyleft component (MSYS2 source packages as published on repo.msys2.org,
plus the hand-identified extras below), qemu-third-party-licenses.zip (the licence files the MSYS2 packages install,
the QEMU COPYING files and the SPDX texts of the copyleft licences involved) and SOURCES.md listing every bundled DLL,
its package, version, licence, whether the version was matched byte for byte, and the asset (with SHA-256) or upstream
URL of its source.
"""
import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import urllib.request
import zipfile

COPYLEFT = re.compile(r"(?<![A-Za-z])(A?GPL|LGPL|MPL|EPL|CDDL|EUPL|OSL)", re.I)
SPDX_TEXTS = ["GPL-2.0-only", "GPL-3.0-only", "LGPL-2.1-only", "LGPL-3.0-only", "AGPL-3.0-only", "MPL-2.0",
              "GCC-exception-3.1"]
SPDX_URL = "https://raw.githubusercontent.com/spdx/license-list-data/v3.27.0/text/{}.txt"

# DLLs that are not (or no longer) in an MSYS2 package that can be matched byte for byte.
EXTRAS = {
    "libssp-0.dll": {
        "component": "libssp (GCC 13.1.0, MSYS2 mingw-w64-gcc 13.1.0-3)",
        "version": "13.1.0-3",
        "licence": "GPL-3.0-or-later WITH GCC-exception-3.1",
        "assets": [
            ("gcc-13.1.0.tar.xz", "https://ftp.gnu.org/gnu/gcc/gcc-13.1.0/gcc-13.1.0.tar.xz"),
            ("mingw-w64-gcc-13.1.0-3.recipe.tar.gz", "git:msys2/MINGW-packages@21c70ad1096ea91eab83ba80c2fffb57c46e46ca:mingw-w64-gcc"),
        ],
        "upstream": "https://gcc.gnu.org/",
        "note": "Not byte-matched: the DLL's compiler string is `GCC: (Rev3, Built by MSYS2 project) 13.1.0`, i.e. "
                "MSYS2 mingw-w64-gcc 13.1.0-3 (gcc-libs, before MINGW-packages commit dfa6dd8 removed libssp-0.dll "
                "on 2023-05-06). That package and its source package are no longer on repo.msys2.org, so the source "
                "is the upstream GCC 13.1.0 tarball plus the MSYS2 build recipe (PKGBUILD and patches) at "
                "MINGW-packages commit 21c70ad (pkgver=13.1.0 pkgrel=3).",
    },
    "brlapi-0.8.dll": {
        "component": "BrlAPI client library (BRLTTY)",
        "version": "BrlAPI 0.8.x; source attached: BRLTTY 6.4 (BrlAPI 0.8.3)",
        "licence": "LGPL-2.1-or-later",
        "assets": [("brltty-6.4.tar.xz", "https://brltty.app/archive/brltty-6.4.tar.xz")],
        "upstream": "https://brltty.app/",
        "note": "Not byte-matched: not an MSYS2 package. The DLL was cross-built by the QEMU for Windows maintainer "
                "with Debian's mingw-w64 GCC 10 (PE timestamp 2022-04-20); his published "
                "https://qemu.weilnetz.de/brlapi/mingw64-x86_64-libbrlapi.tar (BRLTTY 6.2, BrlAPI 0.8.1, 2021-01-07) is "
                "an older build. The DLL exports brlapi_sync and brlapi_strerror_r, which first appear in BRLTTY 6.4, "
                "and it predates BRLTTY 6.5 (2022-06-09), so BRLTTY 6.4 is the closest determinable release; the exact "
                "revision is not recorded in the binary.",
    },
}


def fetch(url, dest):
    req = urllib.request.Request(url, headers={"User-Agent": "orole-qemu-sources"})
    for attempt in range(4):
        try:
            with urllib.request.urlopen(req, timeout=600) as r, open(dest + ".part", "wb") as f:
                while True:
                    b = r.read(1 << 20)
                    if not b:
                        break
                    f.write(b)
            os.replace(dest + ".part", dest)
            return True
        except urllib.error.HTTPError as e:
            if e.code == 404:
                return False
            if attempt == 3:
                raise
        except Exception:
            if attempt == 3:
                raise
    return False


def git_recipe(spec, dest, work):
    # git:<owner/repo>@<commit>:<dir>
    m = re.match(r"git:([^@]+)@([0-9a-f]{40}):(.+)$", spec)
    repo, commit, path = m.groups()
    d = os.path.join(work, "recipe-" + commit[:10])
    if not os.path.isdir(d):
        subprocess.run(["git", "init", "-q", d], check=True)
        subprocess.run(["git", "-C", d, "fetch", "-q", "--depth", "1", "--filter=blob:none",
                        f"https://github.com/{repo}.git", commit], check=True)
    with open(dest, "wb") as f:
        subprocess.run(["git", "-C", d, "archive", "--format=tar.gz",
                        f"--prefix={os.path.basename(dest).replace('.recipe.tar.gz', '')}/", "FETCH_HEAD", path],
                       check=True, stdout=f)


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for b in iter(lambda: f.read(1 << 20), b""):
            h.update(b)
    return h.hexdigest()


def human(n):
    return f"{n / 1048576:.1f} MiB" if n >= 1048576 else f"{n / 1024:.0f} KiB"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dlls", required=True, help="qemu_dll_sources.py JSON")
    ap.add_argument("--zipdir", required=True, help="unpacked qemu zip")
    ap.add_argument("--licenses", required=True, help="licence files extracted by qemu_dll_sources.py")
    ap.add_argument("--qemu-src", required=True)
    ap.add_argument("--qemu-commit", required=True)
    ap.add_argument("--qemu-version", required=True)
    ap.add_argument("--zip-name", required=True)
    ap.add_argument("--zip-sha256", required=True)
    ap.add_argument("--installer", required=True)
    ap.add_argument("--installer-sha512", required=True)
    ap.add_argument("--work", required=True)
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    os.makedirs(a.out, exist_ok=True)
    os.makedirs(a.work, exist_ok=True)
    data = json.load(open(a.dlls))

    assets = {}  # asset name -> sha256

    def add_asset(name, src_url):
        dest = os.path.join(a.out, name)
        if name not in assets:
            if src_url.startswith("git:"):
                git_recipe(src_url, dest, a.work)
            elif not fetch(src_url, dest):
                raise SystemExit(f"404: {src_url}")
            assets[name] = sha256(dest)
        return name

    rows = []  # per package
    for p in data["packages"]:
        lic = " AND ".join(p.get("licenses", [])) or "unknown"
        lic = lic.replace("spdx:", "")
        copyleft = bool(COPYLEFT.search(lic))
        asset = None
        src_url = None
        for cand in p["source_candidates"]:
            req = urllib.request.Request(cand, method="HEAD", headers={"User-Agent": "orole-qemu-sources"})
            try:
                urllib.request.urlopen(req, timeout=60)
                src_url = cand
                break
            except urllib.error.HTTPError:
                continue
        if copyleft:
            if not src_url:
                raise SystemExit(f"no source package for {p['name']} {p['version']}: {p['source_candidates']}")
            asset = add_asset(os.path.basename(src_url), src_url)
        rows.append({"component": p["name"].replace("mingw-w64-x86_64-", ""), "base": p["base"],
                     "version": p["version"], "licence": lic, "copyleft": copyleft, "dlls": p["dlls"],
                     "exact": p["exact"], "asset": asset, "src_url": src_url, "upstream": p.get("url", ""),
                     "note": "" if p["exact"] else "closest determinable version (not byte-identical)"})

    for dll in data["orphans"]:
        if dll not in EXTRAS:
            raise SystemExit(f"unidentified DLL without an EXTRAS entry: {dll}")
    for dll, e in EXTRAS.items():
        if dll not in data["orphans"]:
            continue
        names = [add_asset(n, u) for n, u in e["assets"]]
        rows.append({"component": e["component"], "base": "", "version": e["version"], "licence": e["licence"],
                     "copyleft": True, "dlls": [dll], "exact": False, "asset": ", ".join(names),
                     "src_url": ", ".join(u for _, u in e["assets"]), "upstream": e["upstream"], "note": e["note"]})

    # QEMU itself.
    qname = os.path.basename(a.qemu_src)
    assets[qname] = sha256(a.qemu_src)

    # Licence bundle.
    lic_zip = os.path.join(a.out, "qemu-third-party-licenses.zip")
    entries = []
    if True:
        for f in ("COPYING", "COPYING.LIB", "LICENSE"):
            if os.path.exists(os.path.join(a.zipdir, f)):
                entries.append((os.path.join(a.zipdir, f), f"qemu/{f}"))
        for root, _, files in os.walk(a.licenses):
            for f in files:
                full = os.path.join(root, f)
                entries.append((full, "msys2/" + os.path.relpath(full, a.licenses).replace(os.sep, "/")))
        for spdx in SPDX_TEXTS:
            dest = os.path.join(a.work, spdx + ".txt")
            if not fetch(SPDX_URL.format(spdx), dest):
                raise SystemExit(f"no SPDX text {spdx}")
            entries.append((dest, f"spdx/{spdx}.txt"))
    # Fixed timestamps and order so a re-run builds the same bytes.
    with zipfile.ZipFile(lic_zip, "w", zipfile.ZIP_DEFLATED) as z:
        for full, arc in sorted(entries, key=lambda e: e[1]):
            zi = zipfile.ZipInfo(arc, date_time=(1980, 1, 1, 0, 0, 0))
            zi.compress_type = zipfile.ZIP_DEFLATED
            zi.external_attr = 0o644 << 16
            with open(full, "rb") as fh:
                z.writestr(zi, fh.read())
    assets["qemu-third-party-licenses.zip"] = sha256(lic_zip)

    sizes = {n: os.path.getsize(os.path.join(a.out, n)) for n in assets if os.path.exists(os.path.join(a.out, n))}
    sizes[qname] = os.path.getsize(a.qemu_src)

    rel = "https://github.com/rayorole/orole-os-desktop/releases/download/qemu-v20260811/"
    L = []
    L.append("# Corresponding source for qemu-v20260811\n")
    L.append(f"`{a.zip_name}` (SHA-256 `{a.zip_sha256}`) holds unmodified files of the QEMU for Windows installer "
             f"`{a.installer}` (SHA-512 `{a.installer_sha512}`) from https://qemu.weilnetz.de/w64/. This page lists "
             "the source of everything in it. Every file named in the Asset column is attached to this release; "
             "the SHA-256 column is the hash of that asset.\n")
    L.append("## QEMU\n")
    L.append(f"`qemu-system-x86_64.exe` and `qemu-img.exe` report `{a.qemu_version}`. That is commit "
             f"`{a.qemu_commit}` of the installer maintainer's tree https://repo.or.cz/qemu/ar7.git (Stefan Weil's "
             "`master`, the merge of upstream tag `v11.1.0`; it is not on gitlab.com/qemu-project/qemu itself). "
             f"`{qname}` is that tree with all git submodules (the `roms/` firmware sources such as SeaBIOS, iPXE, "
             "qboot, OpenSBI, SLOF, OpenBIOS, u-boot-sam460ex, vbootrom, edk2 and its submodules) and the meson "
             "subprojects, assembled the way QEMU's `scripts/make-release` does. The firmware files in `share/` are "
             "the prebuilt blobs of that tree's `pc-bios/` directory. Licence: GPL-2.0-only (some parts "
             "GPL-2.0-or-later, LGPL-2.1-or-later, BSD and MIT; see `LICENSE` and `COPYING*` in the tarball).\n")
    L.append("| Asset | Size | SHA-256 |\n|---|---|---|")
    L.append(f"| [{qname}]({rel}{qname}) | {human(sizes[qname])} | `{assets[qname]}` |\n")
    L.append("## Bundled DLLs\n")
    L.append("The DLLs come from MSYS2's `mingw64` repository. Each was identified by downloading the MSYS2 binary "
             "packages published on or before the build date (2026-08-11) from https://repo.msys2.org/mingw/mingw64/ "
             "and comparing the DLL byte for byte; **exact** means the shipped DLL is identical to the one in that "
             "package version. For every copyleft licence (GPL, LGPL, AGPL, MPL; including dual licences) the MSYS2 "
             "source package of that exact version (`makepkg --allsource`: upstream source plus PKGBUILD and patches) "
             "is attached. Packages under permissive licences only are not attached; their source package URL is "
             "listed.\n")
    L.append("| Component (MSYS2 package) | Version | Licence | DLLs | Match | Source asset | SHA-256 |")
    L.append("|---|---|---|---|---|---|---|")
    notes = []
    for r in sorted(rows, key=lambda r: r["component"].lower()):
        match = "exact" if r["exact"] else "closest*"
        if r["note"] and not r["exact"]:
            notes.append(f"- **{r['component']}** ({', '.join(r['dlls'])}): {r['note']}")
        if r["asset"]:
            names = [n.strip() for n in r["asset"].split(",")]
            src = "<br>".join(f"[{n}]({rel}{n}) ({human(sizes[n])})" for n in names)
            sh = "<br>".join(f"`{assets[n]}`" for n in names)
        else:
            src = f"not attached (permissive): {r['src_url'] or r['upstream']}"
            sh = ""
        L.append(f"| {r['component']} | {r['version']} | {r['licence']} | {', '.join(r['dlls'])} | {match} | {src} | {sh} |")
    L.append("")
    if notes:
        L.append("### Not matched byte for byte\n")
        L.extend(notes)
        L.append("")
    L.append("## Licence texts\n")
    L.append(f"[qemu-third-party-licenses.zip]({rel}qemu-third-party-licenses.zip) "
             f"({human(sizes['qemu-third-party-licenses.zip'])}, SHA-256 `{assets['qemu-third-party-licenses.zip']}`) "
             "holds QEMU's `COPYING`/`COPYING.LIB`, the licence files each MSYS2 package installs "
             "(`share/licenses/<package>/`), and the SPDX texts of " + ", ".join(SPDX_TEXTS) + ".\n")
    L.append("## DLL hashes\n")
    L.append("| DLL | SHA-256 |\n|---|---|")
    for d, h in sorted(data["dll_sha256"].items(), key=lambda kv: kv[0].lower()):
        L.append(f"| {d} | `{h}` |")
    L.append("")
    L.append("Generated by `.github/workflows/qemu-sources.yml` (`.github/scripts/qemu_dll_sources.py`, "
             "`.github/scripts/qemu_sources_pack.py`) in https://github.com/rayorole/orole-os-desktop.")
    with open(os.path.join(a.out, "SOURCES.md"), "w", encoding="utf-8", newline="\n") as f:
        f.write("\n".join(L) + "\n")
    with open(os.path.join(a.work, "assets.json"), "w") as f:
        json.dump({"assets": assets, "sizes": sizes}, f, indent=1)
    for n in sorted(assets):
        print(f"{assets[n]}  {sizes.get(n, 0):>12}  {n}", file=sys.stderr)


if __name__ == "__main__":
    main()
