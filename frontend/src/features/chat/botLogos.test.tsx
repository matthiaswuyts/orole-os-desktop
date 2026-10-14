import { render } from "@testing-library/react";
import { expect, it } from "vitest";
import { harnessLogo, modelLogo } from "./botLogos";

it("has a mark for known harnesses and nothing for the rest", () => {
  expect(harnessLogo("claude")).toBeTruthy();
  expect(harnessLogo("grok")).toBeTruthy();
  expect(harnessLogo("mystery")).toBeUndefined();
});

it("marks a model by the company behind it and shows nothing for an unknown one", () => {
  expect(render(<>{modelLogo("claude", { id: "opus", name: "Opus" })}</>).container.querySelector("svg")).not.toBeNull();
  expect(modelLogo("opencode", { id: "anthropic/claude-sonnet", name: "Sonnet" })).toBeTruthy();
  expect(modelLogo("custom", { id: "my-private-model", name: "Private" })).toBeUndefined();
});
