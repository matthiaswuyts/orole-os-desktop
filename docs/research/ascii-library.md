# ascii.rest library

Original source review: `C:/code/projects/_upstream/ascii` (HEAD `bfd6ad8cebc2486be75cc26b4d0fe5065d5c15c9`).
Orole now pins npm package `ascii.rest` 0.2.1 (`frontend/package.json`) for Tokyo rain and Lantern lake.
Licence: MIT, Copyright (c) 2026 bas3line. Dependency, not copied code; the MIT notice ships with the package.

## Piece contract (`src/types.ts`)

A piece module exports `meta` and a default function. `meta`: `name`, `category`, `note`, `cols`, `rows`, `fps`
(0 = still), optional `options`, `clock`, `palette` (up to 64 `#rrggbb`), `ground`, `cell` (1 or 2). The default
function takes options and returns `frame(t, env)`, which returns exactly `rows` lines of `cols` characters.
Coloured pieces (palette) draw on a `<canvas>`; the rest draw text into a `<pre>`.

## Catalogue

The original source checkout had 197 files (index/helpers included); README said 169/196 pieces depending on section. Categories:
scenes, shapes, space, physics, nature, creatures, objects, generative, effects, ui, data, type, logos, companies,
distros. Colour "scenes" in that checkout (13): alpine-dawn, aurora-fjord, deep-reef, desert-night, earthrise,
kyoto-dusk, marine-drive, misty-forest, night-coast, ocean-sunset, storm-plains, taj-dawn, varanasi-ghats.
Package 0.2.1 also includes [Tokyo rain](https://ascii.rest/tokyo-rain/) and
[Lantern lake](https://ascii.rest/lantern-lake/), both 200 × 100 square-cell colour scenes at 15 fps.

## Mounting and options (`src/mount.ts`)

`mount(el, piece, { fps, motion, ...pieceOptions })` returns a stop function. `fps` defaults to `meta.fps ?? 30`;
`fps: 0` draws once. A resize observer redraws canvases. Playback runs only while the element has been seen,
`document.hidden` is false, and `(motion || !prefers-reduced-motion)`: **with `prefers-reduced-motion: reduce` the
first frame is kept unless `motion: true` is passed** (the option exists for pages that already decided).

## How Orole uses it (`frontend/src/components/ascii/AsciiBackground.tsx`)

- `AsciiBand`: one `<canvas>` per window across the top of Home, cropped like `object-fit: cover` (scenes are 2:1),
  faded at the foot with a CSS mask; never one canvas per widget (`AGENTS.md`).
- Scene loaded lazily by name through `load[scene]()` from `ascii.rest`; the picker lists 13 scenes in `SCENES`
  (night-coast, aurora-fjord, alpine-dawn, deep-reef, desert-night, earthrise, kyoto-dusk, marine-drive,
  misty-forest, ocean-sunset, storm-plains, tokyo-rain, lantern-lake). All participate in random startup
  selection. taj-dawn and varanasi-ghats exist upstream but are not offered.
- Orole passes `motion: true` always and applies its **own** policy first, `decideMotion`: disabled, static-only,
  paused, reduced-motion (Settings > Accessibility or the OS preference), pause when unfocused, intensity (scales
  fps, 1..30), low-power (<= 2 cores or discharging < 20%: capped at 6 fps). When the policy says do not animate
  it mounts with `fps: 0`, i.e. a still first frame. Unit tests: `motion.test.ts`.
- Decorative only: the band is `aria-hidden` and `pointer-events-none`.
- Settings > Customization > Background accepts PNG, JPEG, and WebP images up to 10 MB. Images are
  center-cropped to 2:1 and sampled to 200 × 100 RGB cells; only those samples and the filename are saved
  through the existing daemon-backed settings API (about 80 KB, below its 256 KiB per-value limit).
  The original file and its path are not retained. Browser preview uses the existing local settings storage.
- `custom-background.ts` converts those samples into a 64-colour, ordered-dithered dot piece, mounted through
  the same `ascii.rest` renderer as the built-in scenes. The picker previews the converted result.
  Uploads are still images, share Home's crop/fade, opacity and intensity, and disable startup randomization.
  Replace image swaps the upload; Remove image returns to the saved built-in scene. Selecting a built-in
  scene or enabling random startup removes the upload. Invalid uploads leave the current background intact.

## Risks

Small, young, single-maintainer library; each scene is its own lazy chunk (the build emits one chunk per piece).
Pin the version and keep the motion policy in Orole's code.
