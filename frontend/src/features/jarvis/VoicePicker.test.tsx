import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import type { TTSModel, Voice } from "@/mygo";
import { LanguagePicker, ModelPicker, VoicePicker } from "./VoicePicker";
import { voiceGradient } from "./voices";

afterEach(cleanup);

const voices: Voice[] = [
  { id: "v1", name: "Rachel", description: "Calm · female · American" },
  { id: "v2", name: "George", description: "Warm · male · British" },
];
const models: TTSModel[] = [
  { id: "flash", name: "Flash v2.5", description: "Fastest and cheapest.", recommended: true },
  { id: "turbo", name: "Turbo v2.5 (older)", description: "Slower.", recommended: false },
];
const noop = () => undefined;

it("gives the same id the same gradient and different ids different ones", () => {
  expect(voiceGradient("v1")).toBe(voiceGradient("v1"));
  expect(voiceGradient("v1")).not.toBe(voiceGradient("v2"));
});

it("shows name and description for each voice option, with the trigger showing the chosen name", () => {
  render(<VoicePicker id="v" value="v2" voices={voices} defaultId="" onChange={noop} />);
  expect(screen.getByRole("combobox")).toHaveTextContent("George");
  fireEvent.click(screen.getByRole("combobox"));
  expect(screen.getByRole("option", { name: /Rachel.*Calm · female · American/ })).toBeInTheDocument();
  expect(screen.getByRole("option", { name: /George.*Warm · male · British/ })).toBeInTheDocument();
});

it("names the global default in the first voice option", () => {
  render(<VoicePicker id="v" value="" voices={voices} defaultId="v1" onChange={noop} />);
  expect(screen.getByRole("combobox")).toHaveTextContent("Default (Rachel)");
});

it("says plain Default when no default voice is known", () => {
  render(<VoicePicker id="v" value="" voices={voices} defaultId="" onChange={noop} />);
  expect(screen.getByRole("combobox")).toHaveTextContent(/^Default$/);
});

it("hides avatars from assistive tech and paints each from its id", () => {
  const { container } = render(<VoicePicker id="v" value="v1" voices={voices} defaultId="" onChange={noop} />);
  const avatar = container.querySelector('[data-slot="voice-avatar"]') as HTMLElement;
  expect(avatar).toHaveAttribute("aria-hidden", "true");
  expect(avatar.style.backgroundImage).not.toBe("");
});

it("lists curated models with descriptions and a Recommended badge", () => {
  render(<ModelPicker id="m" value="turbo" models={models} defaultId="flash" onChange={noop} />);
  fireEvent.click(screen.getByRole("combobox"));
  expect(screen.getByRole("option", { name: /Flash v2.5.*Recommended.*Fastest and cheapest/ })).toBeInTheDocument();
  expect(screen.getByRole("option", { name: /Turbo v2.5 \(older\).*Slower/ })).toBeInTheDocument();
});

it("keeps an unknown stored model selected, labelled as not recommended", () => {
  render(<ModelPicker id="m" value="eleven_multilingual_v2" models={models} defaultId="" onChange={noop} />);
  expect(screen.getByRole("combobox")).toHaveTextContent("eleven_multilingual_v2 (not recommended)");
});

it("labels the default model option with the default's name", () => {
  render(<ModelPicker id="m" value="" models={models} defaultId="flash" onChange={noop} />);
  expect(screen.getByRole("combobox")).toHaveTextContent("Default (Flash v2.5)");
});

it("reports the chosen value", async () => {
  const picked: string[] = [];
  render(<VoicePicker id="v" value="" voices={voices} defaultId="" onChange={(v) => picked.push(v)} />);
  fireEvent.click(screen.getByRole("combobox"));
  const option = await screen.findByRole("option", { name: /George/ });
  fireEvent.mouseMove(option);
  fireEvent.mouseUp(option);
  fireEvent.click(option);
  await waitFor(() => expect(picked).toEqual(["v2"]));
});

it("offers Dutch, English and Auto as call languages, with what Auto does", () => {
  render(<LanguagePicker id="l" value="nl" onChange={noop} />);
  expect(screen.getByRole("combobox")).toHaveTextContent("Nederlands");
  fireEvent.click(screen.getByRole("combobox"));
  expect(screen.getByRole("option", { name: /Nederlands/ })).toBeInTheDocument();
  expect(screen.getByRole("option", { name: /English/ })).toBeInTheDocument();
  expect(screen.getByRole("option", { name: /Auto.*Starts in Dutch and switches if you speak another language/ })).toBeInTheDocument();
});
