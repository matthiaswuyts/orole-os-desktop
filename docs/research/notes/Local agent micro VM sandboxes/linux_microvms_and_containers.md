# Linux microVM and container sandboxes for per-agent isolation, from a Windows desktop host (state as of 15 Oct 2026)

Scope: Firecracker, Cloud Hypervisor, QEMU `microvm`, libkrun (microsandbox, krunkit/Podman, krunvm), gVisor, Kata Containers, Docker/Podman containers, Docker Sandboxes (`sbx`). The focus is a per-agent sandbox with a filesystem, a shell or terminal, and a headless or headful browser, run locally on a Windows 10/11 desktop. Dates are given where sources state them. Older material is labelled.

---

## Q1. Firecracker: host requirements, jailer, snapshot/restore and fork, images, device limits, E2B usage, running Chromium in the guest

### Takeaway
Firecracker is the reference minimal VMM. It needs a Linux host with KVM: there is no Windows or WHP backend, so on Windows it runs only inside WSL2 or another Linux VM with nested virtualization. Its published numbers are 125 ms or less to guest `/sbin/init` and 5 MiB or less of VMM overhead. Snapshot/restore has been GA since v1.13 and is how E2B-style platforms reach sub-second starts. The device model is still minimal: up to v1.17 the changelog has no virtio-fs and no GPU entries. PCI and memory hotplug are new and opt-in, and the first escape-potential CVE (CVE-2026-5747, April 2026) was in the new PCI transport.

### Cited Findings
**Host requirements and platform**
- Firecracker is Linux-only. Docker's May 2026 comparison post lists it as "Linux only" ([Docker blog, 7 May 2026](https://www.docker.com/blog/comparing-sandboxing-approaches-ai-agents/)). Docker reportedly wrote its own VMM for Sandboxes because Firecracker targets only Linux deployment environments, while Docker needed macOS and Windows. This is secondary, contributed content ([Cloud Native Now](https://cloudnativenow.com/contributed-content/the-questions-every-team-asks-about-docker-sandboxes/)).
- Firecracker maintainers consider nested virtualization out of the project's scope. It works when the outer layer exposes KVM ([firecracker issue #1721](https://github.com/firecracker-microvm/firecracker/issues/1721)).

**Published performance specification** ([SPECIFICATION.md](https://github.com/firecracker-microvm/firecracker/blob/main/SPECIFICATION.md))
- VMM startup: up to 8 CPU-ms before the API socket is available, with wall clock typically about 12 ms (range 6–60 ms).
- Boot: 125 ms or less from the InstanceStart API call to the start of the guest's `/sbin/init`. This is measured with the serial console disabled and a minimal kernel and rootfs.
- Memory overhead: 5 MiB or less of VMM threads for a guest with 1 vCPU and 128 MiB, excluding MMDS. Many vsock connections can push it above 5 MiB.
- Compute runs at more than 95% of bare metal. Network reaches up to 14.5 Gbps at no more than 80% of one host core, or 25 Gbps at 100%, with 0.06 ms of added latency. Storage reaches up to 1 GiB/s at no more than 70% of one core. Several of these are marked "integration test pending". Reference hosts are m5d.metal and m6g.metal.
- The NSDI 2020 paper claims up to 150 microVMs launched per second per host, and less than 5 MB overhead with a minimal guest kernel config ([NSDI'20 paper, MIT 6.566 reading copy](https://css.csail.mit.edu/6.566/2023/readings/firecracker.pdf)).

**Jailer** ([docs/jailer.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/jailer.md))
- The jailer validates its inputs, closes inherited file descriptors, and clears the environment. It creates a chroot at `/srv/jailer/<exec>/<id>/root` and copies the Firecracker binary into it so memory isn't shared with other Firecracker processes.
- It applies `setrlimit` (default no-file 2048) and sets up cgroups (v1 by default, v2 via `--cgroup-version=2`).
- It calls `unshare` into a new mount namespace, then `pivot_root` and `chroot`. It runs `mknod` for `/dev/kvm` and `/dev/net/tun` inside the jail, optionally joins a netns (`--netns`), optionally creates a new PID namespace (`--new-pid-ns`), drops to the target uid/gid, and then execs Firecracker.
- It requires a statically linked (musl) Firecracker of the same version. It normally runs as root. Jail creation slows as the mount count rises.
- The arXiv study measured E2B's Firecracker seccomp allowlist at 55 syscalls, the smallest of the engines it tested ([Andronchik & Lokhmakov, arXiv:2606.08433, June 2026](https://arxiv.org/html/2606.08433v1)).

**Snapshot/restore and fork-from-snapshot** ([snapshot-support.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/snapshot-support.md))
- There are two snapshot types: full (device state plus all guest memory) and diff (only dirtied memory, as a sparse file). Diff snapshots are still a developer preview, and snapshotting as a whole went GA in v1.13 ([CHANGELOG](https://github.com/firecracker-microvm/firecracker/blob/main/CHANGELOG.md)).
- On restore, guest memory is mapped privately from the snapshot file. Pages load on demand and writes go to a CoW anonymous mapping, with either the `File` backend (kernel page faults) or the `Uffd` backend (userspace page-fault handler). The memory file must stay immutable while any restored VM runs, which is what allows many clones to share one memory file.
- No official restore-latency figure is published; it depends on memory size, vCPUs and devices. Restore latency is high on cgroups v1, so v2 is recommended.
- Restore caveats:
  - Open vsock connections are closed on resume, though listening sockets survive. Network connection state is not guaranteed.
  - Cross-host restore requires identical hardware and software.
  - The guest wall clock must be corrected after restore.
  - Snapshots taken very early in boot may crash on resume.
- Clone uniqueness: resuming the same snapshot in multiple VMs duplicates random numbers, IDs and tokens, and Firecracker calls this insecure unless the operator de-duplicates. VMGenID makes Linux 5.18+ reseed its kernel PRNG, but userspace state stays duplicated. The VMClock `vm_generation_counter` lets userspace detect a restore but needs Linux 7.0 or patched kernels. Firecracker trusts snapshot files, so authentication and encryption are the operator's job.
- Third-party restore measurements (not official):
  - About 28 ms in a homelab build ([dev.to](https://dev.to/adwitiya/how-i-built-sandboxes-that-boot-in-28ms-using-firecracker-snapshots-i0k)).
  - 5–30 ms per sandbox against 125–200 ms cold, in a vendor blog ([Spheron](https://www.spheron.network/blog/ai-agent-code-execution-sandbox-e2b-daytona-firecracker/)).
  - Hosted E2B cold-start figures conflict: about 150 ms, versus about 80 ms best case and about 410 ms p50 (secondary sources gathered in the same Spheron/aggregator results). None are measured with Chromium.

**Device model and features by release (2025–2026)** ([CHANGELOG](https://github.com/firecracker-microvm/firecracker/blob/main/CHANGELOG.md))
- v1.13: optional PCI support (`--enable-pci`), snapshots GA, aarch64 PVTime.
- v1.14: memory hotplug via virtio-mem; balloon free-page reporting and hinting (developer preview).
- v1.15: VMClock device.
- v1.16: developer-preview hotplug of PCI virtio-block, pmem and net.
- v1.17 (latest numbered release): `huge_pages` on snapshot load, `sync_snapshot_files`, Graviton5.
- No virtiofs or GPU entries appear in the changelog. Host↔guest file exchange therefore goes over block devices, pmem or vsock.

**How E2B uses it**
- E2B's engineering blog describes running Firecracker with a read-only rootfs plus an in-guest OverlayFS (`overlay-init`). The upper layer is tmpfs by default, or a block device if writes must survive VM termination. Without this, even small Alpine rootfs images cost "a few hundred megabytes" of copying per instance ([E2B blog](https://www.e2b.dev/blog/scaling-firecracker-using-overlayfs-to-save-disk-space)).
- E2B templates snapshot a running process. A process started in the template build (for example a browser) is already running when a sandbox is created from the template ([E2B docs: start & ready commands](https://e2b.dev/docs/template/start-ready-command); [How it works](https://e2b.dev/docs/template/how-it-works)).
- E2B's "desktop" template for computer use is Ubuntu 22.04 with XFCE, Xvfb, x11vnc and noVNC on port 6080, plus xdotool and scrot. The example task uses Firefox ([E2B desktop template](https://e2b.dev/docs/template/examples/desktop); [computer-use docs](https://www.e2b.dev/docs/use-cases/computer-use)).
- Other users per Fly.io: AWS Lambda, Vercel Sandbox (one microVM per sandbox), and Fly Machines/Sprites ([Fly.io, 9 Sep 2026](https://fly.io/learn/firecracker-vs-gvisor/)).

### Inferences
- Headful Chromium in Firecracker is practical only with software rendering (SwiftShader/llvmpipe on Xvfb), because there is no virtio-gpu. The E2B desktop stack (Xvfb, XFCE, VNC) is the proven pattern. Headless Chromium with `--disable-gpu` works the same way as in any GPU-less Linux server.
- Snapshotting a VM with Chromium already launched is the main way to cut its startup cost: E2B's "start command in template" model is exactly this.
- For a Windows desktop product, Firecracker means shipping or requiring a Linux VM (WSL2 with nested KVM, or a dedicated Hyper-V VM) and running a fleet of nested microVMs inside it. That adds the nested-virtualization tax and Windows-11-only nesting (see Q6).

### Gaps
- No published, reproducible measurement of Chromium cold start or snapshot-restore time inside Firecracker was found. The Spheron/aggregator numbers are unverified.
- The RAM a Chromium-bearing guest needs (guest kernel, Xvfb and Chromium working set) was not found in a Firecracker-specific benchmark.
- GPU passthrough: the changelog is silent. Whether the new PCI transport is intended to enable VFIO/GPU was not confirmed.

---

## Q2. Cloud Hypervisor (and QEMU `microvm`): features vs Firecracker, MSHV, Windows guests, Windows host?

### Takeaway
Cloud Hypervisor (CH) is the "bigger Firecracker". It is a Rust VMM on KVM or MSHV with virtio-fs, vhost-user, VFIO passthrough, CPU, memory and device hotplug, snapshot/restore and live migration, and it can boot Windows guests. It does **not** run on a Windows host. MSHV support means CH runs in a *Linux root partition* on the Microsoft Hypervisor (an Azure-style deployment), not on Windows 10/11 Hyper-V. QEMU's `microvm` machine type is a Firecracker-like minimal x86 machine, but its docs only describe KVM use.

### Cited Findings
- CH "runs on top of the KVM hypervisor and the Microsoft Hypervisor (MSHV)". Guests are 64-bit Linux and Windows 10 / Windows Server 2019 ([cloud-hypervisor README](https://github.com/cloud-hypervisor/cloud-hypervisor)).
- Hotplug covers CPUs, memory resize, VFIO passthrough devices, and virtio-net/block/pmem/fs/vsock. vhost-user device offload is supported. Snapshot/restore and live migration exist but are not supported across CH versions. The recommended host kernel is 5.13, with CI on 5.15 ([README](https://github.com/cloud-hypervisor/cloud-hypervisor)).
- MSHV support:
  - It targets Linux running as the root partition on the Microsoft Hypervisor. It requires building CH with the `mshv` feature and can boot guests from IGVM files ([CH docs/mshv.md, v42 mirror](https://git.phyllo.me/External/cloud-hypervisor/src/tag/v42.0/docs/mshv.md)).
  - The `/dev/mshv` root-partition kernel driver (v6, March 2025) was applied to hyperv-next ([LKML](https://lkml.iu.edu/hypermail/linux/kernel/2503.1/09685.html); [LWN](https://lwn.net/Articles/1012202)).
  - No source describes a CH configuration hosted on Windows.
- Security:
  - CVE-2026-45782, a virtio-block use-after-free rated 8.9, is described as the first escape-class CH CVE. CVE-2026-27211 is a host-information leak.
  - CH has an in-tree cargo-fuzz harness.
  - The arrakis sandbox product had pinned CH v44.0 for more than 471 days, leaving CVE-2026-45782 unpatched for at least 7 days.
  - Among the tested engines, arrakis (CH) had the largest kernel-primitive reachability (12 of 14) and 170 guest-visible `/dev` entries.
  - Source for all of the above: [arXiv:2606.08433](https://arxiv.org/html/2606.08433v1).
- Kata 4.0's runtime-rs supports QEMU, Cloud Hypervisor and Dragonball ([Kata 4.0 overview, 22 Jul 2026](https://katacontainers.io/blog/kata-containers-4-0-0-release-overview/)). Northflank markets Kata with CH as one of its runtimes ([Northflank](https://northflank.com/blog/kata-containers-vs-firecracker-vs-gvisor)).
- QEMU `microvm` ([QEMU docs](https://www.qemu.org/docs/master/system/i386/microvm.html)):
  - It is "inspired by Firecracker": no PCI, no ACPI, and up to eight virtio-mmio devices. It boots with qboot (or SeaBIOS) and needs a host-side kernel, since firmware can't boot from virtio-mmio block.
  - It has no hotplug and no cross-version live migration, and is intended for short-lived guests and as a benchmarking baseline.
  - The docs mention only KVM (kvmclock "if using KVM") and say nothing about WHPX, HVF or TCG.

### Inferences
- On Windows, CH is usable only inside WSL2 or a Linux VM with nested KVM, in the same position as Firecracker. Its extras (virtio-fs, hotplug, VFIO) matter mainly on bare-metal Linux.
- CH is the better fit than Firecracker when a per-agent VM needs virtio-fs workspace sharing or memory hotplug and ballooning. Firecracker remains the leaner, smaller-surface option.
- Upstream QEMU has a WHPX accelerator, so QEMU is the only VMM in this set with an upstream Windows-host path, but `microvm` + WHPX is undocumented. A QEMU WHPX boot bug ("Unexpected VP exit code 4") is open on GitLab ([QEMU issue 4385](https://gitlab.com/qemu-project/qemu/-/work_items/4385)), so expect rough edges.

### Gaps
- No CH boot-time or memory-overhead numbers were found in primary docs. The README claims only "low latency, low memory footprint".
- CH licence text and latest release number were not confirmed on the fetched page.
- Whether QEMU `microvm` works under WHPX on Windows was not verified.

---

## Q3. libkrun family: libkrun, microsandbox, krunvm, krunkit/Podman; architecture, platforms (Windows/WHP?), startup, maturity, licensing

### Takeaway
libkrun is a VMM shipped as a dynamic library with a C API. It offers TSI (socket-level networking over vsock, no virtual NIC) or virtio-net with passt/gvproxy, plus virtio-fs, vsock, virtio-gpu (venus) and a balloon. Upstream supports Linux KVM and macOS HVF (ARM64), and the README is Apache-2.0. **Windows status as of October 2026:** microsandbox ships a *preview* Windows host built on "libkrun and libkrunfw Windows ports" that use WHP (June 2026). Upstream libkrun's README still lists only KVM and HVF. microsandbox is the most Windows-relevant microVM runtime in this set, but it is beta, and libkrun puts guest and VMM in the same security context.

### Cited Findings
**libkrun upstream** ([containers/libkrun README](https://github.com/containers/libkrun))
- It is a dynamic library with a minimal set of emulated devices, a C API (`include/libkrun.h`), and is written in Rust.
- Platforms: Linux/KVM and macOS 14+ ARM64/HVF. Windows/WHP is not mentioned.
- Networking options are mutually exclusive:
  - TSI (Transparent Socket Impersonation over virtio-vsock) needs a custom guest kernel, has no raw sockets, and supports only AF_INET, AF_INET6 and AF_UNIX.
  - virtio-net goes through a passt or gvproxy userspace proxy.
- Devices: console, block, fs (virtio-fs), gpu (venus and native-context), net, vsock, balloon (free-page reporting only), rng.
- Variants: libkrun-sev (SEV/SEV-ES/SEV-SNP) and libkrun-tdx. The licence is Apache-2.0.
- `main` is libkrun 2.0 in development and breaks 1.x API/ABI. Production users should use the newest `stable-*` branch.
- Security caveat: the guest and VMM "pertain to the same security context", so the VMM must be isolated with host OS features such as namespaces.
  - virtio-fs gives no protection against guests reaching other host directories the VMM can access.
  - Guests can exhaust host filesystem resources.
  - With TSI, network restrictions must be applied to the VMM process.
- Related projects: crun (krun handler), krunkit (GPU-enabled VMs on macOS), muvm.

**microsandbox**
- It is a local-first microVM runtime and library built on libkrun and smoltcp. It runs standard OCI images and claims average boot under 100 ms, with a footnote that this was measured on an M1 Mac ([GitHub README](https://github.com/superradcompany/microsandbox)).
- It is Apache-2.0, labelled "beta software", has about 8.7k stars, and offers TS, Rust, Python, Go and Ruby SDKs ([README](https://github.com/superradcompany/microsandbox)).
- Features:
  - Snapshot and restore of sandbox state, and **fork of a live sandbox**. `--cow-mem` shares unchanged memory from a full snapshot.
  - Host and port allowlists.
  - Secrets that "never enter the VM".
  - Guides for Playwright headless browser jobs and Browser Use agents.
  - Source: [README](https://github.com/superradcompany/microsandbox).
- Windows:
  - Preview status. It requires Windows 10+ on x64 or ARM64 with the **Windows Hypervisor Platform** optional feature (`HypervisorPlatform`), which is distinct from the `VirtualMachinePlatform` feature that WSL2 and Docker Desktop enable. `msb doctor --fix` enables it.
  - The runtime ships `libkrunfw.dll`. Published ports may trigger Windows Firewall prompts, so they should bind to 127.0.0.1 ([microsandbox Windows docs](https://docs.microsandbox.dev/troubleshooting/windows)).
  - The 26 June 2026 changelog says msb now runs "as a first-class local host on Windows" on "the libkrun and libkrunfw Windows ports". It has a Windows-specific passthrough filesystem backend, and "Unix-only" features return explicit unsupported errors ([changelog 2026-06-26](https://docs.microsandbox.dev/changelog/2026-06-26)).
  - PyPI marks Windows x86_64 and ARM64 as Preview ([PyPI](https://pypi.org/project/microsandbox/0.6.17/)).
  - The README lists "Windows with WHP enabled" without a preview label, which conflicts with the docs and PyPI ([README](https://github.com/superradcompany/microsandbox)).
  - The earlier blocker was getting libkrun and libkrunfw to work on Windows (libkrun issue #147), per the old planning issue ([microsandbox #47](https://github.com/superradcompany/microsandbox/issues/47)).
- Other Windows work outside upstream libkrun:
  - The `a3s-libkrun-sys` crate claims a Windows WHPX backend, but its docs.rs build failed ([docs.rs](https://docs.rs/a3s-libkrun-sys)).
  - The phooq/libkrunfw fork documents a MinGW build of `libkrunfw.dll` (`make OS=Windows`), with the embedded kernel limited to 8 CPUs ([fork](https://ithub.global.ssl.fastly.net/phooq/libkrunfw)).
- Security measurements for microsandbox (libkrun):
  - 0 of 28 host-information-leak probes succeeded, and 116 guest-visible `/dev` entries were found.
  - 11 of 14 kernel-LPE primitives were reachable. libkrun is about 72k Rust LOC versus about 85k for Firecracker.
  - No libkrun CVEs were recorded from May 2024 to May 2026, which the authors call "the absence of a finding", not proof.
  - Release cadence is 1–3 months.
  - Source: [arXiv:2606.08433](https://arxiv.org/html/2606.08433v1).

**krunkit / Podman machine**
- Podman machine providers: Linux uses QEMU; macOS uses libkrun (the default in Podman Desktop on Apple Silicon, via krunkit) or applehv; **Windows uses WSL (default) or Hyper-V** ([podman-machine docs](https://docs.podman.io/en/latest/markdown/podman-machine.1.html); [Podman Desktop docs](https://podman-desktop.io/docs/podman/creating-a-podman-machine)).
- Podman 5.2 (2024, older) added full libkrun support on macOS, including GPU mounting ([AlternativeTo summary of release](https://alternativeto.net/news/2024/8/podman-5-2-0-released-full-libkrun-support-on-macos-enhanced-quadlet-features-and-more)).
- The Hyper-V provider arrived in Podman 4.8, and the 5.1 installer can configure it ([podman blog tag](https://blog.podman.io/tag/podman-machine)).
- Podman 6 reportedly keeps WSL as the Windows default ([RuntimeWire, secondary](https://runtimewire.com/article/podman-6-machine-provider-usability-brent-baude)).

### Inferences
- From Orole's Go daemon, a microsandbox-style approach (libkrun on WHP) is the only option found where a Linux microVM per agent runs *directly* on Windows without WSL2 nesting, apart from Docker's closed `sbx`. It is preview and beta, though, and the Windows ports appear to be maintained by microsandbox rather than merged upstream. Pinning and forking risk is real.
- libkrun's "same security context" model means the host-side VMM process must itself be sandboxed (restricted token, job object, AppContainer on Windows). On Linux the README calls for namespaces.
- TSI's no-NIC design simplifies per-agent egress policy, because the VMM proxies sockets. On Windows it is unclear whether TSI or smoltcp-based networking is used (see Gaps).
- krunkit and libkrun-in-Podman are macOS-only today. On Windows, Podman uses WSL2 or Hyper-V VMs with ordinary containers inside.

### Gaps
- No Windows/WHP boot-time or memory-overhead numbers for microsandbox were found. The <100 ms figure is M1 Mac only.
- Which features are "Unix-only" on the Windows build (TSI, virtio-fs, snapshots/fork, GPU) is not listed in the docs.
- No upstream libkrun PR merging a WHP backend was found. Upstream status is unconfirmed.
- krunvm's maintenance status in 2026 was not researched; no source was found.

---

## Q4. gVisor and Kata Containers: isolation vs microVMs, overhead, Docker/containerd integration, Windows-host story

### Takeaway
gVisor is a user-space kernel (the Sentry) that intercepts syscalls. It has the smallest measured kernel attack surface, starts at process speed and needs no KVM: the `systrap` platform works inside VMs, including WSL2 without nested virtualization. Its costs are syscall- and I/O-heavy overhead and Linux-only operation. Kata runs each pod or container inside a lightweight VM (QEMU, Cloud Hypervisor or Dragonball; Kata 4.0 GA in July 2026 made the Rust runtime the default). Kata needs KVM, integrates via containerd shim-v2, and has no Windows-host story beyond a nested Linux VM.

### Cited Findings
**gVisor**
- Platforms ([gVisor platform guide](https://gvisor.dev/docs/architecture_guide/platforms)):
  - **systrap** uses seccomp `SECCOMP_RET_TRAP`. It has been the default since mid-2023 and is "a better choice when running inside a VM, or on a machine without virtualization support".
  - **KVM** "runs best on bare-metal".
  - **ptrace** is deprecated and slated for removal.
  - The guide notes that systrap often beats KVM under nested virtualization.
- Costs come from Sentry memory and extra layers on syscalls. Raw memory access has no extra cost once mapped ([gVisor performance guide](https://gvisor.dev/docs/architecture_guide/performance/)).
- File access goes through a "gofer" proxy, and Directfs (2023) reduced that cost ([Google OSS blog](https://opensource.googleblog.com/2023/06/optimizing-gvisor-filesystems-with-directfs.html?m=0)).
- Third-party, unverified overhead figures: syscall-heavy workloads such as Redis and write-heavy databases run 50% to more than 100% slower, and simple syscalls about 2.2x slower than runc ([rywalker.com summary](https://rywalker.com/research/gvisor)). Treat these as indicative only.
- Users per Fly.io: Google Cloud Run, GKE Sandbox, App Engine, OpenAI (higher-risk tasks incl. code execution), Anthropic (code execution in claude.ai) and Modal. The threat model is defence against host-kernel bugs, with no claim against hardware side channels. Fly.io recommends gVisor for Kubernetes `runtimeClassName`, for cloud VMs without usable KVM, and for CPU-bound, short-lived work ([Fly.io, Sep 2026](https://fly.io/learn/firecracker-vs-gvisor/)).
- Measured surface ([arXiv:2606.08433](https://arxiv.org/html/2606.08433v1)):
  - Seccomp ceiling of 84 host syscalls.
  - The **lowest** kernel-primitive reachability of the tested engines (5 of 14).
  - Only 15 guest-visible `/dev` entries, and 2 of 28 host-leak probes.
  - 3 CVEs in May 2024–May 2026 with **no escape-class entries**, but fixes ship silently 165–458 days before CVE publication.
- An academic VEE 2020 study (older) found that both Firecracker and gVisor execute substantially more host kernel code than native Linux ([Anjali et al., NSF PAR](https://par.nsf.gov/biblio/10176031)).
- Docker's comparison post calls gVisor "Linux only" with a small community ([Docker blog, May 2026](https://www.docker.com/blog/comparing-sandboxing-approaches-ai-agents/)).

**Kata Containers**
- Kata 4.0.0 was announced on 22 Jul 2026 ([Kata 4.0 overview](https://katacontainers.io/blog/kata-containers-4-0-0-release-overview/)):
  - The Rust runtime-rs is the default, and the Go runtime is deprecated. It gets fixes only and will be removed no earlier than 5.0 (forecast for Q1 2028).
  - runtime-rs supports QEMU, Cloud Hypervisor and Dragonball (Alibaba/Ant), with Dragonball as the default built-in VMM.
  - Docker integration is "only tested with QEMU upstream"; Kubernetes and nerdctl are also supported.
  - NVIDIA GPU passthrough works with QEMU. Architectures are x86_64, aarch64 and s390x.
  - Firecracker is absent from the 4.0 runtime-rs list. The Kata 4.0 preview had planned to include it ([4.0 preview](https://katacontainers.io/blog/release-4-0-0-preview/)).
- The Kata hypervisors doc lists CH, Firecracker and Dragonball as Rust Type-2 (KVM) VMMs ([hypervisors.md](https://github.com/kata-containers/kata-containers/blob/2fc7f75724ac9e18e60f63dcc9aa395dc51c184d/docs/hypervisors.md)).
- Trade press describes the Rust rewrite as readying Kata for agent sandboxing ([Cloud Native Now](https://cloudnativenow.com/features/rust-rewrite-readies-kata-containers-for-agent-sandboxing/)).

### Inferences
- **Windows host:** neither gVisor nor Kata runs natively on Windows. gVisor (`runsc` with systrap) is the one Linux sandbox here that runs inside WSL2 *without* nested virtualization, so it also works on Windows 10, where nested KVM in WSL is unreliable. Kata needs `/dev/kvm` inside WSL2, so Windows 11 with nested virtualization and a performance tax.
- For a desktop agent sandbox, Kata adds Kubernetes/containerd machinery that a single-user desktop daemon doesn't need. Its value is OCI compatibility, which microsandbox and Docker `sbx` also provide.
- gVisor's weak spot for browser agents is syscall- and file-heavy work: Chromium's multiprocess IPC, package installs and git. Expect higher CPU for Chromium under gVisor than native (unbenchmarked here; Chromium compatibility under gVisor was not verified in fetched sources).

### Gaps
- No primary 2026 gVisor overhead benchmark for Chromium or Playwright workloads was found.
- The reason Firecracker was dropped from Kata 4.0's runtime-rs list is not explained in the sources.
- No Kata boot-time or memory numbers were published for 4.0.

---

## Q5. Plain containers and Docker Sandboxes on Windows: where they really run, isolation vs a VM, licensing, `sbx`

### Takeaway
On Windows, Docker Desktop, Podman (WSL or Hyper-V provider) and Rancher Desktop all run Linux containers inside a WSL2 or Hyper-V Linux VM. Agents inside that VM share one Linux kernel, so a container escape reaches every other agent and the VM's shared state (though not Windows directly). Docker's own 2026 positioning concedes containers are "weak (shared kernel)" for agents. Docker Sandboxes (`sbx`) is Docker's answer: a standalone CLI that runs **one microVM per agent** with a private Docker daemon. On Windows it uses the **Windows Hypervisor Platform** (Windows 11, x64) through a Docker-built cross-platform VMM, so no WSL2 is needed. Docker Desktop itself needs a paid subscription for companies above 250 employees or $10M revenue.

### Cited Findings
- Docker's May 2026 comparison table rates containers "Weak (shared kernel)" and microVMs "Strong (dedicated kernel)". Docker-in-Docker requires `--privileged`, which "dramatically weakens the isolation". The table also lists per-agent costs: Lima VM about 4 GB RAM and 30–60 s boot, systemd-nspawn about 10 MB and under 1 s ([Docker blog, 7 May 2026](https://www.docker.com/blog/comparing-sandboxing-approaches-ai-agents/)). This is a vendor post with no benchmarks.
- Docker Sandboxes on Windows:
  - "now, even on Windows", installed with `winget install Docker.sbx` (macOS: `brew install docker/tap/sbx`).
  - "fully standalone": Docker Desktop is not required.
  - Agents supported: Claude Code, Copilot CLI, OpenCode, Gemini CLI, Codex, Docker Agent, Kiro and others.
  - Source: [Docker blog, 31 Mar 2026](https://www.docker.com/blog/docker-sandboxes-run-agents-in-yolo-mode-safely/).
  - Secondary sources date the microVM launch to January 2026 and claim macOS and Windows availability then. That conflicts with the March "now even on Windows" phrasing ([Docker blog Jan 2026, via search](https://www.docker.com/blog/docker-sandboxes-run-claude-code-and-other-coding-agents-unsupervised-but-safely/); [letsdatascience](https://letsdatascience.com/news/docker-launches-sandbox-microvms-for-ai-agents-80fdf456)).
- Windows requirements for `sbx`: Windows 11, 64-bit Intel or AMD, and **Windows Hypervisor Platform** (`Enable-WindowsOptionalFeature -Online -FeatureName HypervisorPlatform -All`). No Docker Desktop or Engine is required ([Docker docs: install sandboxes](https://docs.docker.com/ai/sandboxes/install/)).
  - A community post also enabled WSL2 or Hyper-V, which is anecdotal ([DEV, Ajeet Raina](https://dev.to/ajeetraina/running-docker-sandboxes-on-windows-platform-1ikd)).
  - An earlier docs state listed Sandboxes as Experimental, requiring Docker Desktop 4.58+. That is superseded by the standalone `sbx`.
- Architecture ("Why MicroVMs", 16 Apr 2026): one microVM and its own kernel per sandbox, a private Docker daemon inside the VM (full build/run/compose without mounting the host socket), and "no path back to the host" ([Docker blog](https://www.docker.com/blog/why-microvms-the-architecture-behind-docker-sandboxes/)).
- Docker built its own VMM rather than use Firecracker so it could run on macOS and Windows natively via each platform's hypervisor. This is secondary, contributed content ([Cloud Native Now](https://cloudnativenow.com/contributed-content/the-questions-every-team-asks-about-docker-sandboxes/); [WindowsForum](https://windowsforum.com/news/docker-cloud-sandboxes-launch-hosted-microvms-for-ai-coding-agents.445887/)).
- Architecture docs ([docs.docker.com/ai/sandboxes/architecture](https://docs.docker.com/ai/sandboxes/architecture)):
  - Directly mounted workspaces use filesystem passthrough with **virtiofs** caching on by default on all OSes (`DOCKER_SANDBOXES_ENABLE_VIRTIOFS_CACHE=0` disables it). Mountless and cloned workspaces keep files inside the VM.
  - All outbound TCP goes through a host-side proxy that enforces policy: a forward proxy for HTTP(S), transparent forwarding for other TCP.
  - Credentials are injected by the host proxy, so the agent sees only a sentinel value.
  - A host-side MCP gateway enforces MCP policy.
  - Sandboxes can be stopped and restarted without recreating the VM. Snapshots are not described.
- Docker positions each sandbox as its own network (sandboxes can't reach each other or the host) with allow/deny policies ([Docker blog, May 2026](https://www.docker.com/blog/comparing-sandboxing-approaches-ai-agents/)).
- Cloud Sandboxes (24 Sep 2026) are hosted microVMs from about $0.07/hour, with `sbx move <project> --to cloud`. This is secondary and not local ([Forkast](https://forkast.news/docker-launched-cloud-sandboxes-containers-werent-designed-for-ai-agent-isolation/); [Docker blog](https://www.docker.com/blog/introducing-cloud-sandboxes-start-on-your-laptop-finish-in-the-cloud/)).
- **Docker Desktop licensing:** commercial use at organizations with more than 250 employees **or** more than $10M annual revenue requires a paid Pro, Team or Business subscription. Subsidiaries of larger companies and contractors count toward the total ([Docker pricing FAQ](https://docker.io/pricing/faq/); [legacy pricing](https://www.docker.com/legacy-pricing)). The March 2026 blog page lists Personal at $0, Pro $9–11, Team $15–16 and Business $24 per user per month ([Docker blog](https://www.docker.com/blog/docker-sandboxes-run-agents-in-yolo-mode-safely/)).
- Podman on Windows uses a WSL (default) or Hyper-V machine ([podman-machine docs](https://docs.podman.io/en/latest/markdown/podman-machine.1.html)).

### Inferences
- For Orole, depending on Docker Desktop brings licensing exposure for business users and a shared-kernel agent boundary. `sbx` gives a microVM boundary with good egress and credential design, but it is a closed Docker product whose licence and terms for embedding were not found. It also requires Windows 11, so Windows 10 users are excluded.
- Docker's `sbx` design is a useful reference architecture regardless:
  - VM per agent.
  - A host-side egress proxy with policy.
  - A credential-swapping proxy so secrets never enter the guest.
  - virtiofs workspace passthrough.
  - WHP directly instead of WSL2.

### Gaps
- `sbx` licensing and pricing terms for local use, and whether it can be embedded or driven by a third-party app, were not found.
- Docker has not published cold-start or memory-per-sandbox numbers for `sbx` ("seconds" only).
- Rancher Desktop's 2026 Windows backend and licence were not researched in this pass.
- Whether `sbx` sandboxes include or support a headful browser is not stated; only terminal inspection is mentioned.

---

## Q6. Running these inside WSL2 with nested virtualization: feasibility and gotchas

### Takeaway
On Windows 11, WSL2 enables nested virtualization by default (`nestedVirtualization=true`, Windows-11-only), so `/dev/kvm` is normally available. Firecracker, Cloud Hypervisor, Kata and QEMU/KVM then work inside WSL2 at a nested-virtualization performance cost. On Windows 10 the in-box WSL is reported not to expose virtualization extensions at all. gVisor's systrap needs no nested KVM. WHP-native runtimes (microsandbox, Docker `sbx`) avoid nesting entirely.

### Cited Findings
- `.wslconfig` settings ([Microsoft Learn, updated 16 Sep 2026](https://learn.microsoft.com/en-us/windows/wsl/wsl-config)):
  - `nestedVirtualization` defaults to `true` and is "Only available on Windows 11".
  - `networkingMode` can be `nat` (default), `mirrored`, `consomme` or `none`. `mirrored` and several DNS and firewall settings require Windows 11 22H2+.
  - There is an experimental `virtiofs` setting for Windows shares.
  - `memory` defaults to 50% of host RAM.
  - `autoMemoryReclaim` defaults to `dropCache`.
  - `vmIdleTimeout` defaults to 60 s.
  - `kernel` and `kernelModules` allow a custom kernel.
- Windows 10: a WSL issue reports `/dev/kvm` absent and no `vmx` regardless of `.wslconfig` on builds 19044/19045. The lifted WSL service writes `ExposeVirtualizationExtensions = false` ([microsoft/WSL #40735](https://github.com/microsoft/WSL/issues/40735)). Whether this was fixed was not confirmed.
- All outer layers must expose virtualization extensions before KVM, Firecracker or QEMU can run in a guest. Test read/write access on `/dev/kvm` first ([Crabbox nested execution notes](https://crabbox.sh/features/nested-execution.html)).
- Firecracker needs read/write on `/dev/kvm` (via `setfacl` or the `kvm` group). Nested support is a KVM matter, and Firecracker considers it out of scope ([firecracker #1721](https://github.com/firecracker-microvm/firecracker/issues/1721); [WSL KVM guide, older 2023](https://news.mynavi.jp/article/20231121-2823959/); [boxofcables, older](https://boxofcables.dev/accelerated-kvm-guests-on-wsl-2/)).
- gVisor recommends systrap over KVM inside VMs "due to the overhead of nested virtualization" ([gVisor platforms](https://gvisor.dev/docs/architecture_guide/platforms)).
- WHP (`HypervisorPlatform`) is a separate Windows feature from `VirtualMachinePlatform` (used by WSL2 and Docker Desktop). microsandbox and Docker `sbx` need WHP ([microsandbox Windows docs](https://docs.microsandbox.dev/troubleshooting/windows); [Docker sandboxes install](https://docs.docker.com/ai/sandboxes/install/)).

### Inferences
Gotchas for a WSL2-hosted microVM fleet:
- The WSL VM's memory cap (50% default) bounds total agent density.
- `autoMemoryReclaim` and `vmIdleTimeout` can interfere with long-lived daemons. The idle timeout shuts the VM down, which would kill every agent VM inside.
- Nested EPT adds latency to page faults, which hurts snapshot restores that rely on on-demand paging (UFFD or File-backed).
- Networking is double-NAT unless mirrored mode is used, and mirrored needs 22H2+.
- Windows 10 support is effectively lost for KVM-based options.
- The user's dev box (from the session environment) reports Windows 11 build 22000 (21H2). Nesting is available there, but `mirrored` networking (22H2+) is not.
- Orole's architecture rule that terminals and agents live in the daemon fits best with a WHP-native VMM inside the daemon process tree. A WSL2-nested VMM would put agent lifetime under WSL's lifecycle (idle shutdown, `wsl --shutdown` by the user).

### Gaps
- No quantitative benchmark of Firecracker or CH boot and snapshot-restore *inside WSL2* vs bare metal was found.
- The current status of WSL #40735 (Windows 10 nested KVM) is unconfirmed.

---

## Q7. Image strategy: smallest practical guest with shell, Python/Node and Chromium; reproducible builds; CoW per agent; snapshot fast-start

### Takeaway
A maintained Alpine plus Chromium image is about 270 MB compressed, and older stripped images reach 120–150 MB. Playwright with its bundled browsers effectively needs glibc (Debian slim), and those images run 350 MB to over 1 GB compressed, up to 2.5 GB on Ubuntu. Per-agent copies should never be full copies. The patterns in use are:
- an in-guest overlayfs over a read-only rootfs (E2B);
- block-level CoW over a shared read-only base, using dm-snapshot or a custom sparse overlay (Tensorlake, wrenn);
- qcow2 backing files for QEMU-family VMMs;
- memory snapshots mapped CoW (Firecracker private mmap, microsandbox `--cow-mem`) for fast start.

### Cited Findings
- Image sizes are Docker Hub compressed sizes ([Docker Hub listings](https://hub.docker.com/r/zenika/alpine-chrome/tags)):
  - zenika/alpine-chrome:latest is about 267 MB and actively pushed; the `with-playwright` tag is about 380 MB but over a year old.
  - vanrijn/alpine-chrome is about 319 MB.
  - Stale minimal images are about 128–149 MB: chinello/alpine-chrome, hiono/headless-chrome-alpine, and bjoernkw/alpine-node-chromium (Node plus Chromium).
  - Playwright Python images range from about 350 MB to 1.21 GB.
- A Japanese write-up on slimming Playwright images reports 2.5 GB on Ubuntu. The author chose Debian slim over Alpine because Alpine "lacks functionality" for Playwright ([zenn.dev](https://zenn.dev/dozo/articles/80e1ede2d1be05)).
- E2B found that copying even a small Alpine rootfs per instance costs "a few hundred megabytes". They use a read-only rootfs plus `overlay-init` with a tmpfs upper layer, or a block device upper layer to persist ([E2B blog](https://www.e2b.dev/blog/scaling-firecracker-using-overlayfs-to-save-disk-space)).
- Block-level options:
  - A device-mapper snapshot gives writable block CoW over a read-only base with only loop devices and `dmsetup` ([Julia Evans, 2021, older](https://jvns.ca/blog/2021/01/27/day-47--using-device-mapper-to-manage-firecracker-images)).
  - Tensorlake uses a read-only shared base, a sparse per-VM live overlay and frozen overlay layers, so snapshots cost O(changed bytes) ([Tensorlake](https://www.tensorlake.ai/blog/firecracker-disk-snapshots-o-changed-bytes)).
  - The wrenn sandbox project replaced reflink rootfs copies with dm-snapshots: one shared read-only loop device per template and one sparse CoW file per sandbox. Firecracker must be destroyed before removing the dm device ([wrenn commit](https://git.omukk.dev/wrenn/sandbox/src/commit/63e9132d3822a5a7d6acdfa1d21f0c45c21d9793)).
  - firecracker-containerd has an image-builder for rootfs images ([firecracker-containerd image-builder](https://github.com/MecSimCalc/firecracker-containerd/blob/main/tools/image-builder/README.md)).
- Memory-side fast start:
  - Firecracker maps snapshot memory privately (CoW). The file must stay immutable, so many VMs can share one snapshot, subject to the uniqueness caveats in Q1 ([snapshot-support.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/snapshot-support.md)).
  - microsandbox supports snapshot, restore and live fork, with `--cow-mem` sharing unchanged memory ([README](https://github.com/superradcompany/microsandbox)).
  - E2B templates capture a running process at build time ([E2B docs](https://e2b.dev/docs/template/how-it-works)).
- microsandbox and Docker `sbx` consume standard OCI images directly, which avoids a separate rootfs build pipeline ([microsandbox README](https://github.com/superradcompany/microsandbox); [Docker blog](https://www.docker.com/blog/why-microvms-the-architecture-behind-docker-sandboxes/)).

### Inferences
- Recommended guest: Debian-slim (glibc) with Python and Node, using the distro or Playwright Chromium for compatibility with Playwright/CDP tooling. An Alpine plus system-`chromium` image is the size-optimised alternative if the agent drives Chromium over raw CDP. Expect roughly 300–500 MB compressed for shell, Python, Node and Chromium on Debian slim. This is an estimate from the listings above, not measured.
- On a Windows host:
  - Reflinks need ReFS (Dev Drive) or a Linux filesystem inside WSL. NTFS has no reflink.
  - qcow2 backing files (QEMU/WHPX), VHDX differencing disks (Hyper-V) or the runtime's own OCI layer store (microsandbox, `sbx`) are the practical CoW mechanisms.
  - This is general knowledge, not verified in fetched sources.
- Snapshot-based fast start (boot once, launch Chromium, snapshot, then clone per agent) is the biggest latency lever. It requires per-clone reseeding of RNG and identity state, a new machine-id and fresh TLS/session tokens, per Firecracker's uniqueness warnings.

### Gaps
- Reproducible image building (apko/melange, mkosi, Nix, BuildKit with SOURCE_DATE_EPOCH) was not researched in this pass. No sources were gathered.
- No uncompressed on-disk or in-RAM size measurements were found for a Debian-slim + Python + Node + Chromium guest.
- No benchmark compares reflink vs dm-thin vs qcow2 for per-agent clone time.

---

## Q8. Security track record and the strongest practical isolation for untrusted AI-generated code

### Takeaway
Industry practice and the one neutral 2026 study agree:
- Plain containers (runc) are the weakest boundary, with a steady stream of escapes: Leaky Vessels in 2024 and three new runc escapes in November 2025.
- MicroVMs (Firecracker, Cloud Hypervisor, libkrun) and gVisor are the accepted options for untrusted code.
- MicroVMs gained their first escape-class CVEs in 2026 (Firecracker CVE-2026-5747 in the opt-in PCI transport; CH CVE-2026-45782). gVisor has the smallest measured kernel reachability and no escape-class CVEs in the window, but fixes silently.
- Operator patch and pin policy matters as much as engine choice.
- The common recommendation for multi-tenant, genuinely untrusted agent code is a hardware-virtualized microVM plus a strict egress allowlist, with gVisor as the alternative where KVM isn't available.

### Cited Findings
- The arXiv study (Andronchik & Lokhmakov, dated 24 May 2026, submitted 7 Jun 2026) compared arrakis (CH), E2B (Firecracker), microsandbox (libkrun), gVisor (runsc) and Daytona (runc) ([arXiv:2606.08433](https://arxiv.org/html/2606.08433v1)).
  - Kernel-LPE primitive reachability (lower is better): gVisor 5, E2B 7, microsandbox 11, Daytona 11, arrakis 12.
  - Guest-visible `/dev` entries: gVisor 15, microsandbox 116, E2B 128, arrakis 170, Daytona 268.
  - Host-leak probes (of 28): E2B 0, microsandbox 0, arrakis 1, gVisor 2, Daytona 10.
  - CVEs from May 2024 to May 2026:
    - runc: 4 (CVE-2025-52565, CVE-2025-52881, CVE-2025-31133, CVE-2024-45310).
    - gVisor: 3, none escape-class.
    - Firecracker: 2 (CVE-2026-5747, escape-potential, CVSS4 8.7; CVE-2026-1386, jailer symlink host-write, 6.0).
    - Cloud Hypervisor: 2 (CVE-2026-45782, virtio-block UAF, 8.9, the first CH escape-class; CVE-2026-27211, host leak).
    - libkrun: 0, which is not proof of soundness.
  - Patch cadence: runc, Firecracker and CH disclose on a coordinated basis. gVisor fixes ship 165–458 days before CVE publication. The downstream pin policy dominates: the E2B self-hosted default was unchanged for 399 days, leaving CVE-2026-5747 unpatched for at least 44 days.
  - "No overall ranking is proposed." The strongest combination, a microVM with a continuous public fuzzer, is "unoccupied" among the tested products.
- **CVE-2026-5747** (Firecracker) ([AWS bulletin 2026-015](https://aws.amazon.com/security/security-bulletins/2026-015-aws); [NVD](https://nvd.nist.gov/vuln/detail/CVE-2026-5747); [OSV](https://osv.dev/vulnerability/CVE-2026-5747)):
  - It is an out-of-bounds write in the virtio **PCI** transport. Root in the guest modifies queue registers after the device is activated.
  - The main impact is VMM crash. Host code execution needs extra preconditions.
  - Affected versions are 1.13.0–1.14.3 and 1.15.0; fixed in 1.14.4 and 1.15.1.
  - The default MMIO transport is unaffected.
  - Published 7 Apr 2026 and credited to Anthropic.
- runc history:
  - CVE-2024-21626 "Leaky Vessels" (Jan/Feb 2024, older) was an fd leak giving the container process a host-filesystem working directory, which leads to full escape. It was fixed in runc 1.1.12 ([Tenable](https://tenable.com/cve/CVE-2024-21626); [Red Hat RHSB-2024-001](https://access.redhat.com/fr/security/vulnerabilities/RHSB-2024-001)).
  - CVE-2025-31133 (6 Nov 2025) was a masked-path or `/dev/null` bind-mount race leading to escape. It was fixed in 1.2.8, 1.3.3 and 1.4.0-rc.3, bundled with CVE-2025-52565 and CVE-2025-52881 ([OSV](https://api.osv.dev/v1/vulns/CVE-2025-31133); [TuxCare errata](https://errata.tuxcare.com/els_os/almalinux9.2esu/CLSA-2025-1763648873.html)).
- Fly.io (vendor of Firecracker products, Sep 2026):
  - Plain containers are acceptable only for your own code with scoped credentials and a locked-down network.
  - Firecracker is best for "genuinely untrusted, syscall-heavy agent loops" with snapshot warm pools.
  - gVisor is best on Kubernetes or when KVM isn't usable.
  - "An egress allowlist is what stops exfiltration" either way.
  - Source: [Fly.io](https://fly.io/learn/firecracker-vs-gvisor/).
- Northflank (vendor) recommends microVM isolation for multi-tenant untrusted or user-generated code, and calls gVisor sufficient for internal automation where you control the code ([Northflank](https://northflank.com/blog/firecracker-vs-gvisor)).
- libkrun's README warns that guest and VMM share a security context, so the VMM must itself be confined ([libkrun README](https://github.com/containers/libkrun)).

### Inferences
**Synthesis matrix for a Windows 10/11 desktop host (as of Oct 2026)**

| Option | Runs on Windows host how? | Isolation | Boot / restore | Per-VM overhead | Snapshot / fork | FS share | Net model | GPU | Maturity |
|---|---|---|---|---|---|---|---|---|---|
| Firecracker | Only in WSL2/Linux VM with nested KVM (Win 11) | HW VM + jailer + seccomp (55 syscalls in E2B) | ≤125 ms to init; restore ms-range (unofficial 5–30 ms) | ≤5 MiB VMM | Full GA, diff preview; CoW mmap clones (uniqueness caveats) | No virtiofs; block/pmem/vsock | tap + host NAT | No | Very mature (Lambda, E2B, Fly) |
| Cloud Hypervisor | Only in WSL2/Linux VM (MSHV ≠ Windows host) | HW VM | No primary numbers | "Low" (no figure) | Yes (same version only) | virtiofs, hotplug | tap, vhost-user | VFIO passthrough | Mature (used by Kata, arrakis, Northflank) |
| QEMU `microvm` | KVM documented; WHPX untested for microvm | HW VM, larger QEMU codebase | Fast (no figure) | Higher than FC | QEMU savevm (not researched) | virtio-mmio only | any QEMU netdev | No | Mature QEMU, niche machine |
| libkrun / microsandbox | **Native via WHP (preview, June 2026)**; Linux KVM, macOS HVF | HW VM but VMM shares security context; must confine VMM | <100 ms (M1 Mac claim) | Not published | Snapshot, restore, live fork, `--cow-mem` | virtiofs (Linux/mac); Windows passthrough backend | TSI or passt/gvproxy; allowlists | venus (Linux/mac) | Beta |
| Docker Sandboxes (`sbx`) | **Native via WHP, Win 11 x64**, standalone | HW microVM + private dockerd per agent | "Seconds" | Not published | Stop/restart; no snapshot documented | virtiofs passthrough | Host proxy with policy + credential injection | Not stated | GA product, closed VMM |
| gVisor | Only in WSL2/Linux VM; systrap needs **no** nested virt (works on Win 10) | User-space kernel; smallest kernel reachability | Process start | Sentry memory (no figure) | Checkpoint/restore exists (not researched) | gofer/directfs | netstack | Limited (not researched) | Mature (Google, Modal, OpenAI, Anthropic) |
| Kata 4.0 | Only in WSL2/Linux VM with nested KVM | HW VM (QEMU/CH/Dragonball) | No figures | No figures | Not researched | virtiofs | CNI | NVIDIA via QEMU | Mature, K8s-oriented |
| Docker/Podman containers | Inside WSL2/Hyper-V Linux VM | Shared kernel (weak) | ms | MBs | CRIU (not researched) | bind mounts | bridge | WSL GPU-PV | Mature; Docker Desktop licence ≥250 staff/$10M |

- **Strongest practical isolation that is local and Windows-first:** a hardware-virtualized microVM per agent with the VMM running directly on WHP, no WSL nesting, is the industry-aligned choice. It should come with a host-side egress proxy and allowlist, credentials injected outside the guest, and the VMM process itself confined. Today only Docker `sbx` (closed, Windows 11) and microsandbox/libkrun (open, Apache-2.0, preview/beta) deliver this.
- Firecracker, CH and Kata are stronger-proven but force a WSL2-nested design on Windows.
- gVisor in WSL2 is the fallback that also works on Windows 10.
- If Orole uses the new Firecracker PCI transport or Cloud Hypervisor, it must track upstream releases closely: the 2026 CVEs show pin lag is the dominant real-world risk.

### Gaps
- Most numbers for Docker `sbx`, CH, Kata 4.0 and libkrun on Windows are unpublished. Local benchmarks on the target hardware are needed.
- The 2026 arXiv study's later sections (fuzzing detail, stackability) were only partially read. Its Part 2 (product-level) was not found.
- No 2026 escape CVEs for Docker's custom `sbx` VMM or for the microsandbox Windows port were found. The absence of findings is not evidence of soundness.
