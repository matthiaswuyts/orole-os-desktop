import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/backend", () => ({ inDesktop: false }));
vi.mock("@/mygo", async () => (await import("../builder/testing")).mygoMock);

import { bridge, resetBridge } from "../builder/testing";
import { StructuredPart } from "./StructuredPart";

beforeEach(resetBridge);
afterEach(cleanup);

describe("skill proposal card outside the desktop", () => {
  it("renders the draft with Add disabled and never calls the daemon", () => {
    render(<StructuredPart part={{ name: "skill", spec: JSON.stringify({ proposalId: "p1", botId: "b1", name: "trip-planner", description: "d", body: "b" }) }} />);
    expect(screen.getByText("trip-planner")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add skill" })).toBeDisabled();
    expect(bridge.chat.skillProposal).not.toHaveBeenCalled();
  });
});
