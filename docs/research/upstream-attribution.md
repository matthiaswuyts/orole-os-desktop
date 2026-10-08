
## Copied files (register)
Source: https://github.com/cosscom/shipyard @ c809c5eccfc31a386be6bb82b8b0839eaf7ec89b, MIT, Copyright (c) 2026 Sean Brydon.
| Upstream path | Orole path | Modification |
|---|---|---|
| app/src/components/ui/*.tsx | frontend/src/components/ui/*.tsx | import stubs for lib/focus-home and lib/platform; tooltip root uses coss/Base UI directly instead of Shipyard's global keyboard-focus timer |
| app/src/lib/utils.ts | frontend/src/lib/utils.ts | none |
| app/src/index.css (tokens, density, base layer) | frontend/src/index.css | removed plugin @source, Tauri/sidebar-resize/worktree-tone rules; Orole additions appended |
| app/src/themes/builtin.ts (colours) | frontend/src/themes/builtin.ts | renamed Orole Midnight/Obsidian/Light; expanded to a shared UI/terminal registry with Solarized and Catppuccin palettes |

Theme palette references: [Solarized](https://ethanschoonover.com/solarized/) and
[Catppuccin palette v1.8.0](https://github.com/catppuccin/palette/tree/v1.8.0).
Orole's semantic-token mappings are original; no third-party theme renderer is embedded.
Keyboard and tooltip compositions follow the [coss Kbd](https://coss.com/ui/docs/components/kbd)
and [coss Tooltip](https://coss.com/ui/docs/components/tooltip) contracts using the
already attributed local primitives. The custom `components/tip.tsx` layer was removed.

### Dither Kit charts

Only the official `area-chart` registry component and its shared chart core were
installed with `@dither-kit/cli` 0.1.1. No Dither Kit buttons, avatars, or gradients
are used. Source: [Boring-Software-Inc/dither-kit](https://github.com/Boring-Software-Inc/dither-kit/commit/1e7faee9aa252e499651e6736ed65f7a07d9a6bd),
commit `1e7faee9aa252e499651e6736ed65f7a07d9a6bd`, author ripgrim / Boring-Software-Inc.
The `area-chart` and `core` registry versions are 0.1.0; installed files and source
hashes are recorded in `frontend/dither-kit.json`.

Copied paths: `frontend/src/components/dither-kit/*`, from the
[`area-chart`](https://tripwire.sh/r/area-chart.json) and
[`core`](https://tripwire.sh/r/core.json) registry payloads. Local changes:
`cartesian-canvas.tsx` paints once per data/context change when animation is
disabled, without an idle animation loop; `area.tsx` uses Vite's
`import.meta.env.DEV` guard. Other transformations are the official installer's
normal source transforms.

Upstream package metadata declares MIT, but the inspected repository contains no
standalone LICENSE text. This records the declaration, not a claim that an
upstream license notice was supplied. Retain supplied source notices.

## Dependencies used without copying source
| Project | Version | Licence | Use |
|---|---|---|---|
| ascii.rest (https://github.com/bas3line/ascii @ bfd6ad8cebc2486be75cc26b4d0fe5065d5c15c9) | npm `^0.1.0` | MIT, (c) 2026 bas3line | animated scenes in `frontend/src/components/ascii/AsciiBackground.tsx` |
| MyGo (https://github.com/egoist/mygo) | v0.3.3 | see upstream `LICENSE` | desktop shell, bindings, packaging (`cmd/desktop`, `mygo.json`) |
| Microsoft/go-winio, UserExistsError/conpty, danieljoos/wincred, modernc.org/sqlite, google/uuid | see `go.mod` | upstream licences | named pipes, ConPTY, Credential Manager, SQLite, ids |
| @web-kits/audio (https://audio.raphaelsalaja.com/, https://github.com/raphaelsalaja/audio) | npm `0.2.0` | MIT, (c) 2026 Raphael Salaja | shared synthesized UI and in-app notification engine in `frontend/src/lib/audio.ts`; original cue definitions, no copied samples |

## Original work
`resources/icon.png` and `cmd/desktop/window-icon.png` are drawn programmatically by `scripts/genicon` (original mark, no upstream artwork).
