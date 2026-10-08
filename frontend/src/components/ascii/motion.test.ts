import { describe, expect, it } from "vitest";
import { decideMotion } from "./AsciiBackground";

const base = { enabled: true, staticOnly: false, paused: false, fps: 12, intensity: 1, pauseWhenUnfocused: true, focused: true, reduceMotion: false, lowPower: false };

describe("decideMotion", () => {
  it("animates at the configured fps scaled by intensity", () => {
    expect(decideMotion(base)).toEqual({ animate: true, fps: 12, reason: "ok" });
    expect(decideMotion({ ...base, intensity: 0.5 }).fps).toBe(6);
  });
  it("never animates when reduced motion is requested, even if everything else allows it", () => {
    expect(decideMotion({ ...base, reduceMotion: true })).toMatchObject({ animate: false, fps: 0, reason: "reduced-motion" });
  });
  it("throttles to a still frame when unfocused, unless the user opted out", () => {
    expect(decideMotion({ ...base, focused: false }).reason).toBe("unfocused");
    expect(decideMotion({ ...base, focused: false, pauseWhenUnfocused: false }).animate).toBe(true);
  });
  it("caps fps on low-power devices and stills at zero intensity", () => {
    expect(decideMotion({ ...base, fps: 30, lowPower: true }).fps).toBe(6);
    expect(decideMotion({ ...base, intensity: 0.01 })).toMatchObject({ animate: false, reason: "intensity" });
  });
  it("gives precedence to hard stops: disabled > static > paused", () => {
    expect(decideMotion({ ...base, enabled: false, staticOnly: true }).reason).toBe("disabled");
    expect(decideMotion({ ...base, staticOnly: true, paused: true }).reason).toBe("static");
  });
});
