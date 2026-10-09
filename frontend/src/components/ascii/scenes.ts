import type { PieceName } from "ascii.rest";

/** Colour scenes shared by Home, startup selection, and the settings picker. */
export const SCENES: { id: PieceName; label: string }[] = [
  { id: "night-coast" as PieceName, label: "Night coast" },
  { id: "aurora-fjord" as PieceName, label: "Aurora fjord" },
  { id: "alpine-dawn" as PieceName, label: "Alpine dawn" },
  { id: "deep-reef" as PieceName, label: "Deep reef" },
  { id: "desert-night" as PieceName, label: "Desert night" },
  { id: "earthrise" as PieceName, label: "Earthrise" },
  { id: "kyoto-dusk" as PieceName, label: "Kyoto dusk" },
  { id: "marine-drive" as PieceName, label: "Marine drive" },
  { id: "misty-forest" as PieceName, label: "Misty forest" },
  { id: "ocean-sunset" as PieceName, label: "Ocean sunset" },
  { id: "storm-plains" as PieceName, label: "Storm plains" },
  { id: "tokyo-rain", label: "Tokyo rain" },
  { id: "lantern-lake", label: "Lantern lake" },
];
