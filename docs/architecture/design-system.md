# Orole OS design system

Acceptance reference for GitHub #7 and #26. This specifies the existing component and token contract, not a second UI framework.

## Direction and hierarchy

A compact personal desktop: quiet neutral surfaces, restrained status color, small readable typography, and a single atmospheric ASCII band on Home. The shell owns navigation and the route heading; feature views retain controls, breadcrumbs, counts and meaningful detail titles without duplicating the route title. Native caption controls retain MyGo's reserved inset.

Home combines the task composer, running-agent view and persistent widget grid. The sidebar owns workspace/project navigation; the footer owns daemon, integration, task and machine status. Notifications use a non-modal viewport-corner popover rather than a full-height sheet. Dialogs are for bounded decisions and forms, not ordinary navigation.

## One token layer

`frontend/src/index.css` imports Tailwind v4 and the bundled Inter and JetBrains Mono variable fonts. Its `@theme inline` maps semantic CSS variables to utilities. `frontend/src/themes/themes.css` supplies shared theme styles; `themes/builtin.ts` is the built-in UI/terminal palette registry. `hooks/useApplyTheme.ts` applies palette variables, `color-scheme`, the dark class and `data-density` to the document root and clears old palette overrides on changes.

| Purpose | Tokens / contract |
|---|---|
| Canvas and content | `background`, `foreground` |
| Cards and overlays | `card`, `card-foreground`, `popover`, `popover-foreground` |
| Emphasis and selection | `primary`, `primary-foreground`, `accent`, `accent-foreground` |
| Secondary content | `muted`, `muted-foreground`, `secondary`, `secondary-foreground` |
| Boundaries and focus | `border`, `input`, `ring`, `radius` and derived radii |
| Status | `success`, `warning`, `destructive`, `info` and corresponding foregrounds |
| Navigation | `sidebar`, `sidebar-foreground`, `sidebar-accent`, `sidebar-border`, `sidebar-ring` |
| Density | `row-h`, `row-pad`, `row-gap`, `side-row`; compact is the default |
| Typography | Inter for interface text; JetBrains Mono for code, terminal and technical metadata |

Built-ins: Orole Midnight, Orole Obsidian, Orole Light, Solarized Dark/Light, and Catppuccin Latte/Frappé/Macchiato/Mocha. Theme IDs, labels and terminal colors come from the same registry. Custom themes extend Midnight with the whitelist in `themes/custom.ts`: background, foreground, primary, primary-foreground, card, sidebar, success, warning, destructive, border and muted-foreground. Inputs must be a JSON object with recognized keys and validated color strings, not arbitrary CSS.

## Components and interaction

Use the existing coss/Base UI primitives in `components/ui`; do not add a competing menu, dialog, tooltip or field implementation. Preserve upstream notices and record copied changes in [upstream attribution](../research/upstream-attribution.md).

- Buttons use the shared variants and sizes. Icon-only actions have accessible names.
- Menus select commands/options; nested agent controls reuse menu/submenu primitives. The reasoning slider exposes only the selected harness's supported levels.
- Tooltips use `Tooltip`, `TooltipTrigger`, `TooltipPopup`. Keyboard hints use `Kbd` and `KbdGroup`, not textual approximations of keycaps.
- Dialog fields have associated labels and descriptions. Approval dialogs display the Go-owned action, actor and resource; frontend state never grants authority.
- CodeMirror owns text editing; xterm.js owns terminal rendering. Both consume the shared appearance settings rather than independently selected palettes.
- Charts use the integrated Dither Kit chart components and real daemon samples. Unknown samples remain gaps; empty data is not synthesized.

## Component contracts

These names identify product roles; not every role is a separately exported generic
component. Reuse the concrete implementations below rather than creating aliases
solely to match the handoff's names.

| Role | Existing implementation and caller contract |
|---|---|
| AppShell / TitleBar / Sidebar | `components/shell/AppShell.tsx` accepts the active view as children; shell stores own navigation and sidebar visibility. Native window chrome is not a React imitation. |
| Panel / Widget | `components/widgets/Widget.tsx`: `title`, optional `icon`, `count`, `children`, `onRemove`, and `status` (`ready`, `loading`, `error`, `offline`, `unconfigured`). `message` and `{label,onClick}` action explain non-ready states. The header is the grid drag handle; the body scrolls independently. Feature panels otherwise compose semantic layout elements and shared surfaces; there is no second generic Panel API. |
| DataTable | `components/ui/table.tsx` exports semantic Table/Header/Body/Row/Head/Cell composition. `Table` accepts native table props and `variant: default | card`; its wrapper provides horizontal scrolling. Feature stores own sorting/filtering/selection, not the table primitive. |
| TreeView | `features/files/DirectoryTree.tsx` takes roots, explicit provider, selected path, navigate callback, revision, removable and onRemove. Expansion loads provider directories, ignores stale responses and does not descend through links. Arrow keys/Home/End navigate; missing roots remain visible with their problem. |
| SplitPane / terminal tabs | `features/terminals/TerminalsView.tsx` owns a list of daemon terminal IDs, active pane and up to three side-by-side TerminalPane children. Switching tabs changes the selected pane; closing a terminal explicitly calls Go. This is a concrete terminal split layout, not a general draggable-divider API. |
| Dialogs | `components/ui/dialog.tsx` supplies Base UI open/onOpenChange composition, popup, title, description and close controls. Use confirmation-specific composition for destructive actions and await Go results before reporting success. |
| ContextMenu | `components/ui/context-menu.tsx`: Root/Trigger/Popup and item primitives. Popup accepts position/anchor and portal options while preserving Base UI keyboard/focus behavior. Context actions must still call authorized services. |
| Empty / Loading / Error | Shared Empty/Skeleton/Alert primitives and `WidgetState`; feature-level `NeedsDesktop`, `NotConnected`, `NotImplemented` in `features/shared/States.tsx` describe distinct capability states. Error states preserve actionable retry/setup information. |
| CommandPalette | Existing command registry supplies stable IDs, labels, categories and run callbacks; provider results remain asynchronous data with explicit provider boundaries. Plugin contributions use the same registry. |

## Layout and state ownership

`WidgetGrid` uses a 12-column react-grid-layout grid, header drag handles, resize controls and per-workspace persisted positions. Interactive widget children are excluded from drag activation. Unknown plugin widgets preserve layout and show an explicit unavailable state with removal. The daemon owns saved workspace data; the browser preview must not pretend it can persist daemon data.

Every data surface distinguishes loading, empty, error, offline, setup-required and unsupported states. Retain visible retry/setup actions where applicable. Long histories/lists scroll or virtualize; keep primary controls usable at narrow widths. Do not hide authorization errors or replace unavailable integrations with sample results.

## Motion and accessibility

`components/ascii/motion.ts` centralizes background policy: pause/static settings, OS/application reduced motion, focus/visibility, intensity and low-power limits. Render one decorative, non-interactive canvas, never one per widget. Motion must not convey essential information. Maintain keyboard access, visible focus, labeled inputs and readable semantic foreground/background pairs; verify actual surfaces rather than inferring accessibility from primitive selection.

## Verification

Existing evidence and component ownership are recorded in [component structure](component-structure.md). `bun run --cwd frontend check` covers type checking, lint and unit behavior; `bun run --cwd frontend test:e2e` is the browser suite. Installed WebView2 acceptance is separate from preview-mode checks. Unverified platform/accessibility cases must stay explicitly unverified; this specification does not certify a complete WCAG audit.
