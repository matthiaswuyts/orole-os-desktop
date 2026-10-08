import { describe, expect, it } from "vitest";
import { ago, branchFor, slug } from "./projects";

describe("slug (must match internal/projects.Slug)", () => {
  it.each([["Fix Login!", "fix-login"], ["a/b_c.d", "a-b-c-d"], ["---", ""], ["UPPER", "upper"], ["  x  ", "x"]])("%s", (i, o) => expect(slug(i)).toBe(o));
  it("caps at 48 chars without trailing dash", () => expect(slug("a".repeat(47) + " b")).toBe("a".repeat(47)));
  it("branchFor is empty for unusable names", () => { expect(branchFor("!!")).toBe(""); expect(branchFor("Hello World")).toBe("orole/hello-world"); });
});

describe("ago", () => {
  const now = 1_000_000 * 1000;
  it("formats", () => {
    expect(ago(0, now)).toBe("never");
    expect(ago(1_000_000 - 30, now)).toBe("just now");
    expect(ago(1_000_000 - 300, now)).toBe("5 min ago");
    expect(ago(1_000_000 - 7200, now)).toBe("2 h ago");
    expect(ago(1_000_000 - 3 * 86400, now)).toBe("3 d ago");
  });
});
