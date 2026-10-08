
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

### Oh My Pi logo

Source: [can1357/oh-my-pi](https://github.com/can1357/oh-my-pi/tree/cde91bb38674d365e8c3916d05d2292273bb8554),
commit `cde91bb38674d365e8c3916d05d2292273bb8554`.
The upstream [`assets/icon.svg`](https://github.com/can1357/oh-my-pi/blob/cde91bb38674d365e8c3916d05d2292273bb8554/assets/icon.svg)
(Git blob `f1ccf2a08a90ec4d7f6bb947dc62c5e995114422`) is bundled at
`frontend/src/assets/oh-my-pi.svg` and used by `features/agents/ProviderLogo.tsx`
for the `omp` provider across selectors and the sidebar.

Local changes: the three pale `#fafafa` foreground fills use `currentColor`
to remain visible in light and dark themes; a provenance comment was added.
Geometry, viewBox, orange accents, connector details, and original comments
are retained. The SVG contains only static rectangles and circles, with no
scripts, event handlers, external references, fonts, or runtime network requests.
The logo remains its owner's brand mark; no trademark rights are asserted.

License notice from the pinned upstream
[`LICENSE`](https://github.com/can1357/oh-my-pi/blob/cde91bb38674d365e8c3916d05d2292273bb8554/LICENSE):

```text
MIT License

Copyright (c) 2025 Mario Zechner
Copyright (c) 2025-2026 Can Bölük
Copyright (c) 2026 Stencil Labs, Inc.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### assistant-ui composer attachments

Source: [assistant-ui/assistant-ui](https://github.com/assistant-ui/assistant-ui/tree/9201447608271c64a8a9ca66c69e81d27a1d9fab),
commit `9201447608271c64a8a9ca66c69e81d27a1d9fab`,
`packages/ui/src/components/react/assistant-ui/elements/composer.tsx`.
The standalone `ComposerAttachments` and `ComposerAttachmentChip` subset is adapted
at `frontend/src/components/assistant-ui/elements/composer-attachments.tsx`, following
the [requested attachment element](https://www.assistant-ui.com/elements/composer-attachments).

Changes: coss buttons and semantic theme tokens replace catalog surface helpers;
pending/failed chips can be removed; indeterminate IPC uploads show a spinner rather
than fabricated percentage progress. No assistant-ui runtime, cloud storage or sample
files were introduced. The full MIT notice, copyright (c) 2026 AgentbaseAI Inc., is
retained in the source header.

### beui reasoning range slider

Source: [starc007/ui-components](https://github.com/starc007/ui-components/blob/cbc23255f962098a93a2c103d39e11f93b61d059/components/motion/range-slider.tsx),
commit `cbc23255f962098a93a2c103d39e11f93b61d059`, distributed through
[`range-slider.json`](https://beui.dev/r/range-slider.json).
Copied/adapted path: `frontend/src/components/ui/range-slider.tsx`.

Changes: a native range input supplies pointer/keyboard/accessibility behavior;
percentage positioning removes the shared gesture hook and ResizeObserver; the
rounded thumb and semantic color tokens fit the requested composer reference.
Tick geometry, spring-driven fill/handle and reduced-motion support are retained.
The full MIT notice, copyright (c) 2026 Saurabh Chauhan, is retained in the source.

### BeUI image-generation presentation

Source: [starc007/ui-components](https://github.com/starc007/ui-components/tree/cbc23255f962098a93a2c103d39e11f93b61d059),
commit `cbc23255f962098a93a2c103d39e11f93b61d059`,
[`components/agents/image-generation.tsx`](https://raw.githubusercontent.com/starc007/ui-components/cbc23255f962098a93a2c103d39e11f93b61d059/components/agents/image-generation.tsx).
Adapted path: `frontend/src/features/tasks/TaskImages.tsx`. The image card keeps the
stable status/media/footer composition; Orole connects it to actual provider events and
daemon-verified raster artifacts. Demo timers, dithered synthetic pixels, unsupported
resolution/refinement stages, and regeneration controls were not copied.

The upstream repository's pinned [`LICENSE`](https://raw.githubusercontent.com/starc007/ui-components/cbc23255f962098a93a2c103d39e11f93b61d059/LICENSE)
declares MIT, Copyright (c) 2026 Saurabh Chauhan. The full notice:

```text
MIT License

Copyright (c) 2026 Saurabh Chauhan

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Dependencies used without copying source
| Project | Version | Licence | Use |
|---|---|---|---|
| ascii.rest (https://github.com/bas3line/ascii @ bfd6ad8cebc2486be75cc26b4d0fe5065d5c15c9) | npm `^0.1.0` | MIT, (c) 2026 bas3line | animated scenes in `frontend/src/components/ascii/AsciiBackground.tsx` |
| MyGo (https://github.com/egoist/mygo) | v0.3.3 | see upstream `LICENSE` | desktop shell, bindings, packaging (`cmd/desktop`, `mygo.json`) |
| Microsoft/go-winio, UserExistsError/conpty, danieljoos/wincred, modernc.org/sqlite, google/uuid | see `go.mod` | upstream licences | named pipes, ConPTY, Credential Manager, SQLite, ids |
| @web-kits/audio (https://audio.raphaelsalaja.com/, https://github.com/raphaelsalaja/audio) | npm `0.2.0` | MIT, (c) 2026 Raphael Salaja | shared synthesized UI and in-app notification engine in `frontend/src/lib/audio.ts`; original cue definitions, no copied samples |
| @lobehub/icons-static-svg (https://github.com/lobehub/lobe-icons) | npm `1.95.1` | MIT (package declaration), LobeHub | Four unmodified package SVG imports (Claude, OpenAI, Gemini, OpenCode) in `features/agents/ProviderLogo.tsx`; no additional UI framework or remotely loaded logos. Brand marks remain their owners' trademarks. |

### Lobe Icons license notice

Source: https://raw.githubusercontent.com/lobehub/lobe-icons/master/LICENSE

```text
MIT License

Copyright (c) 2023 LobeHub

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Original work
`resources/icon.png` and `cmd/desktop/window-icon.png` are drawn programmatically by `scripts/genicon` (original mark, no upstream artwork).
