# NixOS vs Debian (slim/minimal) as the guest OS for local per-agent Linux micro VMs (as of Oct 2026)

Context: Windows 10/11 host, QEMU on Windows Hypervisor Platform (WHPX), no guest NIC. The guest runs an agent daemon, a terminal and headful Chromium. Research budget was limited to about 16 tool calls, so several hard numbers are marked as gaps and need local measurement.

## 1. Image and closure size (minimal, and with Chromium + Python + Node); reduction techniques

### Takeaway
Debian 13 official cloud images are a well-documented baseline: about 219 MB (tar.xz) or 326 MB (qcow2) for amd64 genericcloud, with a 3.0 GB sparse raw disk. Minimal NixOS has no official "slim" image. Community reports put a trimmed headless NixOS at about 200-400 MB compressed, while a naive switchable image is about 1.3 GB of store. NixOS can be cut down further with the perlless profile, by disabling docs, and with read-only squashfs/erofs store images. I found no published side-by-side numbers for either distro with Chromium + Python + Node added.

### Cited Findings
- Debian 13 (trixie) "latest" cloud images dated 2026-10-01, amd64: genericcloud is 219M tar.xz, 326M qcow2 and 3.0G raw; nocloud is 291M tar.xz, 388M qcow2 and 3.0G raw; generic is 307M tar.xz, 415M qcow2 and 3.0G raw — [cloud.debian.org trixie/latest index](https://cloud.debian.org/images/cloud/trixie/latest/)
- genericcloud is the smaller variant tuned for virtual environments; generic adds more drivers and firmware for hardware — [computingforgeeks (secondary)](https://computingforgeeks.com/run-debian-openstack-kvm/)
- NixOS Discourse, Jan 10 2026: a switchable/buildable image had a store of about 1.3 GB "even with many things disabled", a full image of 2.7 GB and a compressed image of about 340 MB. The largest store paths were linux 6.12.63 (165M), python3 3.12.12 (116M), perl 5.40.0 (63M), systemd 257.10 (43M), nix 2.28.5 (35M), grub 2.12 (31M) and glibc 2.40 (31M) — [Discourse: how minimal can a NixOS image get](https://discourse.nixos.org/t/how-minimal-can-a-nixos-image-get/45268)
- Same thread, May 2024 (older): a headless image with SSH can likely reach 200-400 MB. About 500 MB of the minimal ISO's compressed size is linux-firmware. The advice is to disable docs/man pages, trim `environment.systemPackages` and inspect the closure with `nix-tree` — [Discourse](https://discourse.nixos.org/t/how-minimal-can-a-nixos-image-get/45268)
- not-os (cleverca22), a NixOS-based generator, produces a 47 MB read-only squashfs for a runit-based (non-systemd) system — [Discourse](https://discourse.nixos.org/t/how-minimal-can-a-nixos-image-get/45268)
- A Nixcademy guide describes taking a NixOS appliance from about 1.5 GB to about 360 MB (seen only in a search snippet, not verified in full) — [hackers.pub post referencing it](https://hackers.pub/@jgalowicz@functional.cafe/019a1138-6514-775b-a209-368afb083bd6/quotes)
- Historical (2016): the minimal NixOS container closure went from about 424 MiB (16.03) to about 212 MiB (16.09) — [NixOS 16.09 release notes](https://spectrum-os.org/git/nixpkgs/tree/nixos/doc/manual/release-notes/rl-1609.section.md)
- The perlless profile builds a system without the perl interpreter. It fails the build if any store path references "perl" (via `system.forbiddenDependenciesRegexes`), sets `system.switch.enable = false` (no in-place `switch-to-configuration`), and disables `documentation.info`, `boot.enableContainers` and `environment.defaultPackages`. A later commit also disabled NixOS documentation in the profile because `nixos-help`/w3m pulled in perl — [nixpkgs profile commits (mirror)](https://git.alioth.systems/mirrors/nixpkgs/commit/bd4b5a5be605f6e5703d227b783fdbab18d4586b); [commit 91103e7d](https://code.ornl.gov/nix/nixpkgs/-/commit/91103e7d7c7efe349834c44abf5d6f3e7496e4d7)
- The option was renamed from `system.forbiddenDependenciesRegex` (string) to `system.forbiddenDependenciesRegexes` (list) — [nixpkgs commit](https://code.ornl.gov/nix/nixpkgs/-/commit/8814c364a386d7ff271b6a6058301bc89d49d199)
- microvm.nix builds the root as a read-only squashfs (smaller) or erofs (faster), either with a prepopulated `/nix/store` or by mounting the host store — [microvm.nix README](https://github.com/microvm-nix/microvm.nix)

### Inferences
- A bare kernel plus systemd NixOS guest with perlless, no docs, `nix.enable = false` and no firmware or grub (direct kernel boot in a VM) plausibly lands at about 150-300 MB uncompressed closure. Debian genericcloud expands to about 1 GB+ on disk (3.0G sparse raw). This is an estimate, not measured.
- With Chromium + Python + Node added, the payload dominates on both distros: likely +400-700 MB uncompressed. The base-OS difference then matters less than deduplication and sharing (see section 7).
- In a VM, firmware, grub and the bootloader can be dropped entirely on NixOS (microvm.nix boots the kernel and initrd directly). This removes about 500 MB of firmware and about 31 MB of grub compared with a generic image.

### Gaps
- `nix.enable = false` closure savings: no measured source found. Nix itself is about 35 MB per the Discourse store listing, plus its deps.
- No published closure size for `nixpkgs#chromium` / `playwright-driver.browsers` / nodejs / python3 found. Measure with `nix path-info -rSh` locally.
- No published Debian `debootstrap --variant=minbase` size or Debian+Chromium size for trixie found in this session.

## 2. Boot time: NixOS vs Debian

### Takeaway
I found no rigorous head-to-head benchmark. The only NixOS-specific microVM numbers are from a 2023 microvm.nix optimization commit under QEMU: a no-op VM went from about 10.2 s to about 4.8 s total. Most of the remaining time was serial-device waits, not Nix-specific work. NixOS-specific boot overhead comes from the activation script (historically Perl), which nixpkgs is replacing with systemd-initrd + `nixos-init` + userborn/sysusers.

### Cited Findings
- microvm.nix optimization commit (2023, older): a no-op VM's `systemd-analyze time` was about 10.2 s total (8.8 s userspace) before the change and about 4.8 s after, including 2.1 s in initrd. The remaining slow units were serial-port devices in initrd taking about 3 s each, which the author could not explain — [microvm.nix optimization.nix log (mirror)](https://code.querie.cc/mirrors/microvm.nix/log/nixos-modules/microvm/optimization.nix?showmsg=1)
- Firecracker discussions report a minimal Linux guest kernel coming up in tens of milliseconds. That is kernel/VMM time, not a full distro userspace — [firecracker issue #4429 mirror](https://git.tdem.in/firecracker-microvm/firecracker/issues/4429)
- Depot (Debian/Ubuntu-style microVMs): about 3.6 s `systemd-analyze` total, dominated by cloud-init services — [Depot blog: optimizing microVM boot times](https://depot.dev/blog/optimizing-microvm-boot-times)
- `nixos-init` aims to eventually replace `system.activationScripts`. It currently handles only the prepare-root step in systemd initrd, requires `services.userborn.enable` or `systemd.sysusers.enable`, and rejects `boot.postBootCommands`. Its rationale is parallel systemd startup for faster boot (present in the nixos-25.11 branch) — [nixpkgs nixos-init (mirror)](https://git.gay/transgirllucy/nixpkgs/src/branch/nixos-25.11/pkgs/by-name/ni/nixos-init)
- A Discourse report of slow activation (8+ min) actually attributed the time to the kernel phase in `systemd-analyze`, so the cause is unconfirmed — [Discourse: activation scripts take a long time](https://discourse.nixos.org/t/activation-scripts-takes-a-long-time/20632)

### Inferences
- In a microVM, Debian's main boot cost is cloud-init. The nocloud image or a custom debootstrap image without cloud-init avoids it. NixOS's main extra cost is stage-1 store mounting (squashfs/erofs or a 9p/virtiofs share) plus activation. With systemd initrd, nixos-init and userborn, the Perl activation path disappears (and perlless requires this).
- With both tuned, the expected userspace difference is under about 1 s. The VMM (QEMU+WHPX firmware/BIOS vs direct kernel boot) and Chromium startup will dominate the time to agent ready. This is unverified and should be measured.
- On a WHPX host, use `-kernel/-initrd` direct boot for either distro to skip firmware and grub.

### Gaps
- No 2025-2026 published measurement of NixOS vs Debian under QEMU/WHPX, cloud-hypervisor or Firecracker with an identical service set.
- No measurement of erofs vs squashfs store mount time.

## 3. microvm.nix: features, hypervisors, store sharing, maturity

### Takeaway
microvm.nix is the de facto NixOS microVM framework: about 3.0k stars, MIT licensed, 8 hypervisors, no tagged releases (consumed from the main branch via flakes). It is Linux-host-centric: its host module runs VMs as systemd services and starts virtiofsd. It is not a Windows/WHPX solution, but its guest modules can produce kernel, initrd and an erofs/squashfs store image you could launch from your own QEMU command line.

### Cited Findings
- Supported hypervisors are QEMU, cloud-hypervisor, Firecracker, crosvm, kvmtool, StratoVirt, alioth and vfkit (macOS). Limits: cloud-hypervisor has no 9p; Firecracker has no 9p or virtiofs; crosvm's 9p is broken; kvmtool has no virtiofs or control socket; StratoVirt and alioth have no 9p or control socket; vfkit has no 9p or tap. QEMU has no listed restrictions — [microvm.nix README](https://github.com/microvm-nix/microvm.nix)
- The root disk is read-only, either with a prepopulated `/nix/store` or by mounting the host's store with an optional writable overlay. The rootfs can be squashfs (smaller) or erofs (faster) — [README](https://github.com/microvm-nix/microvm.nix)
- Maturity: no tagged releases; it targets nixos-unstable and the current stable release; CI on Gradient; about 3.0k stars, 239 forks, 1,231 commits, 39 open issues. Graphics (Wayland forwarding with cloud-hypervisor) is experimental. Commercial support comes from Cyberus Technology and Numtide. The default is 512 MB RAM per VM, shrinkable via `microvm-balloon` — [README](https://github.com/microvm-nix/microvm.nix)
- Sharing the host `/nix/store` (`source = "/nix/store"`, mounted at `/nix/.ro-store`) "drastically" reduces the size and build time of the stage-1 image. 9p is the default; virtiofs needs a separate virtiofsd and "Expect virtiofs to yield better performance over 9p." `writableStoreOverlay` needs a block volume as its upper layer, because 9p/virtiofs can't be the upper layer, and the Nix DB forgets non-system paths after reboot — [microvm.nix shares docs](https://microvm-nix.github.io/microvm.nix/shares.html)

### Inferences
- For Orole on Windows, the pieces of microvm.nix worth reusing are the guest-side NixOS modules and image outputs (kernel, initrd, erofs store). The host-side runner assumes a Linux host (systemd, virtiofsd, tap).
- The "Used by" list was not visible on the page, so I could not name notable users.

### Gaps
- No public list of production users found.
- Whether microvm.nix's QEMU runner works with `-accel whpx` was not found. Treat it as unsupported.

## 4. Building NixOS images from a Windows host

### Takeaway
Nix does not run natively on Windows. Builds must happen in WSL2 (NixOS-WSL, or Nix installed in a WSL distro), in a Linux CI runner (GitHub Actions), or on a remote builder. Builds for standard packages are mostly cache downloads from cache.nixos.org, so a typical agent image build is network-bound (minutes), not compile-bound, unless you override derivations. No published timing numbers were found.

### Cited Findings
- NixOS-WSL can build its own tarball from a flake on any system with Nix. For WSL 2.4.4 or later it installs by double-clicking the `.wsl` file or with `wsl --install --from-file` — [NixOS-WSL README](https://github.com/htngr/NixOS-WSL/blob/main/README.md); [NixOS-WSL docs](https://docsearch.algolia.com/mcp/docs/repo/nix-community/nixos-wsl)
- Nix checks the public binary cache first and downloads a prebuilt binary if one exists. Packages not in the cache are compiled and can take hours — [rix binary-cache vignette](https://docs.ropensci.org/rix/articles/binary-cache.html)

### Inferences
- Recommended pipeline: build the guest image (kernel, initrd, erofs store) in GitHub Actions or WSL2 with a pinned flake lock, push custom paths to Cachix or Attic, and ship the artifact to the Windows app. Do not require Nix on the end user's machine.
- Reproducibility: pinned flake inputs give the same derivation graph (input-addressed). Bit-for-bit output reproducibility is not guaranteed for every package, but the image is fully specified by the lock file.
- Avoid perlless/override patterns that cause cache misses on huge packages such as Chromium. Overriding Chromium means a multi-hour rebuild (general knowledge, unverified here).

### Gaps
- No measured build time for a typical agent image (base + Chromium + Python + Node) found.
- Cachix/Attic pricing and limits were not researched in this session.

## 5. Running non-Nix binaries on NixOS (nix-ld, FHS envs, Playwright, pip, npm)

### Takeaway
This is NixOS's main practical cost for an AI-agent guest. Agents run `pip install`, `npm install` and `npx playwright install`, which download generic-Linux ELF binaries that expect `/lib64/ld-linux-x86-64.so.2` and an FHS layout. `programs.nix-ld` fixes most cases, but Playwright requires matching the npm version to the nixpkgs browser build or pointing it at Nix browsers. On Debian all of these work out of the box.

### Cited Findings
- nix-ld installs a shim at the standard loader path that execs the real loader from `NIX_LD` and maps `NIX_LD_LIBRARY_PATH` to `LD_LIBRARY_PATH`. Its target use case is third-party package managers (vscode, npm, pip) without patching. It is the `programs.nix-ld` option, in nixpkgs since 22.05. It only helps executables using the `/lib` or `/lib64` loader — [nix-ld GitHub](https://github.com/nix-community/nix-ld); [Thalheim blog 2022](https://blog.thalheim.io/2022/12/31/nix-ld-a-clean-solution-for-issues-with-pre-compiled-executables-on-nixos/)
- A user report: after enabling nix-ld, `npm install` worked "like any normal linux distribution" and an unpatched Blender 4.4.3 ran — [NixOS wiki: Nix-ld](https://wiki.nixos.org/wiki/Nix-ld)
- nix-ld-rs (a Rust rewrite) no longer leaks `LD_LIBRARY_PATH` into child processes, which causes fewer breakages. It was planned to merge into nix-ld — [Discourse: nix-ld-rs testers wanted](https://discourse.nixos.org/t/nix-ld-rs-testers-wanted/42145)
- Python from nixpkgs does not pick up `NIX_LD_LIBRARY_PATH`. pip native wheels need extra handling (for example the fix-python tool, which rewrites RPATH in a venv) — [Python and NixOS: a match made in hell](https://crescentro.se/posts/python-nixos/); [fix-python](https://github.com/GuillaumeDesforges/fix-python)
- buildFHSEnv drawbacks: setuid binaries cannot run inside it, and it needs a subshell — [nix-ld README](https://github.com/Mic92/nix-ld/blob/main/README.md)
- Playwright: `playwright install` fails on NixOS. Use `playwright-driver.browsers`, set `PLAYWRIGHT_BROWSERS_PATH` and `PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS=true`. The nixpkgs Playwright version must match the npm `@playwright/test` version (the example pins 1.52.0). Bun needs Playwright 1.58 or later. The Chromium path differs between `chrome-linux` and `chrome-linux64`. Wiki last edited 17 April 2026 — [NixOS wiki: Playwright](https://wiki.nixos.org/wiki/Playwright)
- Users report `playwright-driver.browsers` appearing to provide only Chromium (not Firefox or WebKit) in some versions, and "Executable doesn't exist" errors on version mismatch — [Discourse: running playwright tests](https://discourse.nixos.org/t/running-playwright-tests/25655); [Discourse: synchronize versions](https://discourse.nixos.org/t/synchronize-versions-of-playwright-driver-browsers-and-npm-package/66267)

### Inferences
- For agents that install arbitrary tooling, NixOS needs at least: `programs.nix-ld.enable` with a broad library set (glib, nss, libX11, mesa, and so on, so Playwright's downloaded Chromium works too), `envfs` for `/usr/bin/env`-style shebangs, and possibly an FHS env. Even then, edge cases remain (Python from nixpkgs plus pip wheels, setuid, version-pinned Playwright). On Debian these are non-issues.
- Hybrid option: run Debian as the guest and install Nix (multi-user or Determinate Nix) for declarative extras. This keeps FHS compatibility while gaining a shared, reproducible `/nix/store` for pinned tools.

### Gaps
- envfs and Determinate Nix were not fetched in this session. No 2026 survey of how painful this is found beyond the wiki and Discourse threads.

## 6. Debian reproducibility equivalents and the Nix-on-Debian hybrid

### Takeaway
Debian can get pinned, rebuildable images with mkosi/debos/debootstrap against snapshot.debian.org at a fixed timestamp. snapshot.debian.org has known throttling and availability problems, so a self-hosted mirror or cached artifact is safer. Updates are apt-based and in-place, with no built-in atomic rollback. That is mitigated in a VM by immutable images plus a per-VM overlay.

### Cited Findings
- mkosi generates bootable OS trees or images. It builds Debian with `--distribution debian --release ...` or `mkosi.conf`, and wraps apt, dnf, pacman and zypper — [Arch Wiki: mkosi](https://wiki.archlinux.org/title/Mkosi); [openSUSE package page](https://software.opensuse.org/package/mkosi?locale=de)
- snapshot.debian.org is a date and version archive of old Debian packages, but it has throttling and availability problems: it could not sustain rebuilding even one package, so the Qubes team built its own snapshot service — [Qubes forum](https://forum.qubes-os.org/t/reproducible-builds-for-debian-a-big-step-forward/6800)

### Inferences
- For a VM fleet, "image-based" Debian (build once, ship a read-only rootfs with a per-VM overlay, replace the image on update) gives most of the rollback benefit of NixOS generations. Rollback means keeping the previous image file.

### Gaps
- mkosi snapshot/mirror pinning options (for example `Mirror=`, `SourceDateEpoch`) were not verified in docs this session.
- debos and apko-for-Debian were not researched. apko is Alpine/Wolfi-focused (general knowledge).
- Determinate Nix on Debian was not researched this session.

## 7. Per-VM differentiation: shared read-only store, per-agent packages, rollback

### Takeaway
NixOS's strongest advantage here is architectural. Many VMs can share one read-only `/nix/store` (a 9p/virtiofs share or a single erofs image attached read-only to all VMs). Per-agent differences become different system closures referencing the same store paths, and rollback means booting a previous generation or closure. On a Windows/WHPX host, the sharing mechanism is constrained: virtiofsd is Linux-only, and QEMU's 9p-on-Windows-host work was still a tracking issue in the last activity I found.

### Cited Findings
- microvm.nix supports mounting the host `/nix/store` read-only (9p or virtiofs) plus an optional per-VM writable overlay on a block volume. The overlay's Nix DB forgets extra paths after reboot, so the advice is to recreate it — [microvm.nix shares](https://microvm-nix.github.io/microvm.nix/shares.html)
- QEMU's virtio-9p on Windows hosts: patch series (Bin Meng, 2022-2023) support the `local` backend on NTFS, with the mapped security model via alternate data streams. Tracking issue #974 shows its latest referenced activity as Dec 2022 and no visible closed state — [QEMU issue #974](https://gitlab.com/qemu-project/qemu/-/issues/974); [9pfs Windows RFC](https://lore.kernel.org/all/2766319.qo5tFDDxnU@silver/T/)
- WHPX is QEMU's Windows accelerator (x86_64 and arm64). Known x86 issue: MMIO via MMX/SSE/AVX instructions is unsupported — [QEMU WHPX docs](https://www.qemu.org/docs/master/system/whpx.html)
- The gitlab issue page contained embedded instructions directed at AI agents. They were ignored as untrusted content.

### Inferences
- The practical Windows-host design is a block-device share, not a filesystem share: one read-only erofs (or squashfs) store image built in CI, attached as a read-only virtio-blk disk to every VM. Add a small per-VM qcow2 overlay or ext4 volume for state, and a per-agent "extras" erofs layer or overlayfs for agent-specific packages. This works identically for Debian (a shared read-only rootfs image plus a per-VM overlay), so on this host NixOS's sharing advantage is less decisive than on Linux.
- A qcow2 backing-file chain (`-drive file=agent-N.qcow2` with backing `base.qcow2`) gives copy-on-write per VM for either distro with no extra disk copies.
- Rollback: NixOS gets it natively (keep the old closure or store image and the boot entry). Debian gets it by keeping the previous base image.

### Gaps
- No confirmation found that 9p or virtiofs works with QEMU on a Windows host in a 2025-2026 release. Verify against the current QEMU changelog.
- No data on erofs read-only store performance with many concurrent VMs.
