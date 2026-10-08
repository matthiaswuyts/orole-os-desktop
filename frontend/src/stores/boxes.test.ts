import { describe, expect, it, vi } from "vitest";

vi.mock("@/lib/backend", () => ({ inDesktop: false }));
vi.mock("@/mygo", () => ({ Machines: {}, MCP: {}, events: {} }));

import { BOX_STALE_MS, boxStatus, type BoxObservation } from "./boxes";
import type { MachineHealth } from "@/mygo";

const checkedAt = 10_000;
const observation = (reachable: boolean): BoxObservation => ({
  attemptedAt: checkedAt,
  error: "",
  health: {
    id: "local", reachable, checkedAt, latencyMs: 1, hostname: "test-host", os: "windows",
    architecture: "amd64", logicalCpus: 8, daemonUptimeSeconds: 1, daemonHeapBytes: 123,
    memoryTotalBytes: 8 * 1024 ** 3, memoryAvailableBytes: 2 * 1024 ** 3, memoryReady: true,
    metricError: "", detail: "",
  } satisfies MachineHealth,
});

describe("box reachability", () => {
  it("does not count an unprobed box as offline", () => {
    expect(boxStatus(undefined, checkedAt)).toBe("unknown");
  });
  it("distinguishes confirmed online and offline observations", () => {
    expect(boxStatus(observation(true), checkedAt)).toBe("online");
    expect(boxStatus(observation(false), checkedAt)).toBe("offline");
  });
  it("does not mistake failed authentication or policy for offline", () => {
    expect(boxStatus({ health: null, attemptedAt: checkedAt, error: "Identity changed" }, checkedAt)).toBe("error");
  });
  it("expires successful and unsuccessful observations", () => {
    expect(boxStatus(observation(true), checkedAt + BOX_STALE_MS + 1)).toBe("stale");
    expect(boxStatus(observation(false), checkedAt + BOX_STALE_MS + 1)).toBe("stale");
  });
  it("keeps connectivity separate from metric availability", () => {
    const sampled = observation(true);
    sampled.health!.memoryReady = false;
    sampled.health!.metricError = "CIM unavailable";
    expect(boxStatus(sampled, checkedAt)).toBe("online");
  });
});
