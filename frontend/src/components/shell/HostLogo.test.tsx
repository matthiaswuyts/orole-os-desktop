import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ArchChip, OsLogo, hostArch, hostOS } from "./HostLogo";

afterEach(cleanup);

describe("host logos", () => {
  it("reduces what a machine reports to an OS and an architecture", () => {
    expect(["windows", "Windows 11", "darwin", "macOS 15", "linux/amd64", "Ubuntu 24.04", "plan9"].map(hostOS)).toEqual(["windows", "windows", "macos", "macos", "linux", "linux", "unknown"]);
    expect(hostArch("arm64")).toEqual({ label: "ARM64", arm: true });
    expect(hostArch("aarch64")?.arm).toBe(true);
    expect(hostArch("amd64")).toEqual({ label: "x64", arm: false });
    expect(hostArch("x86_64")?.label).toBe("x64");
    expect(hostArch("")).toBeNull();
  });

  it("draws a mark for a known OS and nothing for an unknown one or an unreported architecture", () => {
    expect(render(<OsLogo os="linux" />).container.querySelector("svg")).not.toBeNull();
    cleanup();
    expect(render(<OsLogo os="plan9" />).container.firstChild).toBeNull();
    cleanup();
    expect(render(<ArchChip architecture="" />).container.firstChild).toBeNull();
    cleanup();
    const arm = render(<ArchChip architecture="arm64" />).container;
    expect(arm.textContent).toBe("ARM64");
    expect(arm.querySelector("svg")).not.toBeNull();
  });
});
