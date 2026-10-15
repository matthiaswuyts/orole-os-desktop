#!/usr/bin/env python3
"""Find the MSYS2 packages that the DLLs of a QEMU for Windows build came from.

Stefan Weil's installers (qemu.weilnetz.de) bundle DLLs from MSYS2's mingw64 repository. For every DLL in --dir this
script looks up the owning package in the current mingw64 files database, then downloads that package's versions
published on or before --build-date (newest first) from repo.msys2.org and compares the DLL byte for byte. The first
identical one is the exact package; when none matches, the newest version on or before the build date is recorded as
the closest determinable one. The result (JSON on --out) has per package: name, base, version, licences, the DLLs it
supplied, whether every DLL matched exactly, the source package URL and the package's licence files (copied to
--licenses/<package>/).

Usage: qemu_dll_sources.py --dir <unpacked zip> --build-date 2026-08-11 --cache <dir> --licenses <dir> --out <json>
Needs Python 3.9+ and the `zstandard` module (pip install zstandard).
"""
import argparse
import concurrent.futures
import datetime as dt
import hashlib
import io
import json
import os
import re
import sys
import tarfile
import urllib.request

import zstandard

REPO = "https://repo.msys2.org/mingw/mingw64/"
SOURCES = "https://repo.msys2.org/mingw/sources/"
MAX_TRIES = 12

# DLLs whose package the current files database no longer lists (renamed soname); package name by hand.
FALLBACK_OWNERS = {
    "libgcc_s_seh-1.dll": ["mingw-w64-x86_64-gcc-libs"],
    "libstdc++-6.dll": ["mingw-w64-x86_64-gcc-libs"],
    "libssp-0.dll": ["mingw-w64-x86_64-gcc-libs"],
    "libjsoncpp-26.dll": ["mingw-w64-x86_64-jsoncpp"],
    "libslirp-0.dll": ["mingw-w64-x86_64-libslirp"],
    "libspice-server-1.dll": ["mingw-w64-x86_64-spice"],
    "libusbredirparser-1.dll": ["mingw-w64-x86_64-usbredir"],
}


def fetch(url, dest=None):
    req = urllib.request.Request(url, headers={"User-Agent": "orole-qemu-sources"})
    for attempt in range(4):
        try:
            with urllib.request.urlopen(req, timeout=300) as r:
                data = r.read()
            break
        except Exception:
            if attempt == 3:
                raise
    if dest:
        tmp = dest + ".part"
        with open(tmp, "wb") as f:
            f.write(data)
        os.replace(tmp, dest)
    return data


def zst_tar(data):
    raw = zstandard.ZstdDecompressor().stream_reader(io.BytesIO(data)).read()
    return tarfile.open(fileobj=io.BytesIO(raw), mode="r:")


def files_db():
    """dll basename -> set of package names, from mingw64.files."""
    owners = {}
    tf = zst_tar(fetch(REPO + "mingw64.files.tar.zst"))
    descs = {}
    for m in tf.getmembers():
        if not m.isfile():
            continue
        d, base = m.name.rsplit("/", 1)
        if base == "desc":
            txt = tf.extractfile(m).read().decode()
            name = re.search(r"%NAME%\n(.+)\n", txt).group(1)
            descs[d] = name
    for m in tf.getmembers():
        if m.isfile() and m.name.endswith("/files"):
            d = m.name.rsplit("/", 1)[0]
            for line in tf.extractfile(m).read().decode().splitlines():
                if line.startswith("mingw64/bin/") and line.lower().endswith(".dll"):
                    owners.setdefault(line[len("mingw64/bin/"):].lower(), set()).add(descs[d])
    return owners


def listing():
    """package name -> [(version, date, filename)] from the repository's directory index."""
    html = fetch(REPO).decode()
    pkgs = {}
    for fn, date in re.findall(r'href="([^"]+?-any\.pkg\.tar\.(?:zst|xz))".*?(\d\d-\w\w\w-\d{4} \d\d:\d\d)', html):
        fn = urllib.request.unquote(fn)
        m = re.match(r"(.+)-([^-]+-[^-]+)-any\.pkg\.tar\.(?:zst|xz)$", fn)
        if not m:
            continue
        when = dt.datetime.strptime(date, "%d-%b-%Y %H:%M")
        pkgs.setdefault(m.group(1), []).append((m.group(2), when, fn))
    return pkgs


def pkg_members(path):
    with open(path, "rb") as f:
        data = f.read()
    if path.endswith(".zst"):
        return zst_tar(data)
    return tarfile.open(fileobj=io.BytesIO(data), mode="r:xz")


def get_pkg(cache, fn):
    dest = os.path.join(cache, fn)
    if not os.path.exists(dest):
        fetch(REPO + urllib.request.quote(fn), dest)
    return dest


def sha256(b):
    return hashlib.sha256(b).hexdigest()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", required=True)
    ap.add_argument("--build-date", required=True)
    ap.add_argument("--cache", required=True)
    ap.add_argument("--licenses", required=True)
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    os.makedirs(a.cache, exist_ok=True)
    cutoff = dt.datetime.fromisoformat(a.build_date) + dt.timedelta(days=1)

    dlls = sorted(f for f in os.listdir(a.dir) if f.lower().endswith(".dll"))
    want = {d: sha256(open(os.path.join(a.dir, d), "rb").read()) for d in dlls}
    owners = files_db()
    index = listing()

    # Candidate packages per DLL: owners in today's files database plus the hand-written fallbacks (DLLs that moved
    # between packages or whose package left the database).
    cands = {}
    for d in dlls:
        names = set(owners.get(d.lower(), set())) | set(FALLBACK_OWNERS.get(d, []))
        cands[d] = sorted(names)
    pkgs = sorted({n for ns in cands.values() for n in ns})

    def scan(name, limit):
        """[(version, date, file, {dll: sha256})] for the newest `limit` versions on or before the cutoff."""
        dl = [d for d in dlls if name in cands[d]]
        vers = sorted((v for v in index.get(name, []) if v[1] < cutoff), key=lambda v: v[1], reverse=True)
        rows = []
        for ver, when, fn in vers[:limit]:
            tf = pkg_members(get_pkg(a.cache, fn))
            got = {}
            for m in tf.getmembers():
                if m.isfile() and m.name.startswith("mingw64/bin/") and m.name[12:] in dl:
                    got[m.name[12:]] = sha256(tf.extractfile(m).read())
            rows.append((ver, when, fn, got))
            if dl and all(got.get(d) == want[d] for d in dl):
                break
        return name, rows

    scans = {}
    with concurrent.futures.ThreadPoolExecutor(8) as ex:
        for name, rows in ex.map(lambda n: scan(n, MAX_TRIES), pkgs):
            scans[name] = rows

    def exact(d):
        for n in cands[d]:
            for ver, when, fn, got in scans.get(n, []):
                if got.get(d) == want[d]:
                    return n, ver, when, fn
        return None

    # Second pass: whole history for the packages of DLLs still unmatched.
    deep = sorted({n for d in dlls if not exact(d) for n in cands[d]})
    with concurrent.futures.ThreadPoolExecutor(8) as ex:
        for name, rows in ex.map(lambda n: scan(n, 10**6), deep):
            scans[name] = rows

    # Assign every DLL to one (package, version): the exact match, else the newest version on or before the cutoff
    # of its first candidate that ships a file of that name.
    chosen, orphans = {}, []
    for d in dlls:
        hit = exact(d)
        if hit:
            n, ver, when, fn = hit
            key = (n, ver)
            chosen.setdefault(key, {"name": n, "version": ver, "file": fn, "dlls": [], "exact_dlls": []})
            chosen[key]["dlls"].append(d)
            chosen[key]["exact_dlls"].append(d)
            continue
        near = None
        for n in cands[d]:
            for ver, when, fn, got in scans.get(n, []):
                if d in got:
                    near = (n, ver, fn)
                    break
            if near:
                break
        if not near:
            orphans.append(d)
            continue
        n, ver, fn = near
        chosen.setdefault((n, ver), {"name": n, "version": ver, "file": fn, "dlls": [], "exact_dlls": []})
        chosen[(n, ver)]["dlls"].append(d)
    results = {}
    for (n, ver), r in sorted(chosen.items()):
        r["exact"] = len(r["exact_dlls"]) == len(r["dlls"])
        results[f"{n}={ver}"] = r
        print(f"{n} {ver}: exact={r['exact']} {r['dlls']}", file=sys.stderr)

    out = {"build_date": a.build_date, "orphans": orphans, "dll_sha256": want, "packages": []}
    for key, r in sorted(results.items()):
        name = r["name"]
        entry = dict(r)
        if True:
            tf = pkg_members(os.path.join(a.cache, r["file"]))
            info = tf.extractfile("./.PKGINFO" if "./.PKGINFO" in tf.getnames() else ".PKGINFO").read().decode()
            kv = {}
            for line in info.splitlines():
                if " = " in line:
                    k, v = line.split(" = ", 1)
                    kv.setdefault(k, []).append(v)
            entry["base"] = kv.get("pkgbase", [name])[0]
            entry["licenses"] = kv.get("license", [])
            entry["url"] = kv.get("url", [""])[0]
            pkgver = kv["pkgver"][0]
            entry["source_candidates"] = [f"{SOURCES}{entry['base']}-{pkgver}.src.tar.{ext}" for ext in ("zst", "gz", "xz")]
            lic_dir = os.path.join(a.licenses, f"{name}-{r['version']}")
            files = []
            for m in tf.getmembers():
                if m.isfile() and m.name.startswith("mingw64/share/licenses/"):
                    rel = m.name[len("mingw64/share/licenses/"):]
                    dest = os.path.join(lic_dir, rel)
                    os.makedirs(os.path.dirname(dest), exist_ok=True)
                    with open(dest, "wb") as f:
                        f.write(tf.extractfile(m).read())
                    files.append(rel)
            entry["license_files"] = files
        out["packages"].append(entry)
    with open(a.out, "w") as f:
        json.dump(out, f, indent=1)
    print(f"orphans: {orphans}", file=sys.stderr)


if __name__ == "__main__":
    main()
