# Dashboard display standards

These standards apply to the `slopWatch` follow dashboard and its overlays.
They do not define layouts or controls for other interfaces.

## Selection

- Show the selected row with the dashboard's selected-row background colour.
- Do not use a text cursor such as `>`, `›`, or another marker to show
  selection.
- Apply the same selection treatment to the main table and selectable items in
  Settings, Weights, Columns, and Sort.
- Keep the selected row's text and spacing stable when the selection moves.

## Navigation hints

- Do not repeat standard navigation instructions inside every overlay.
- Keep the main screen footer as the standard place for dashboard navigation
  shortcuts.
- If an overlay must show a navigation hint, render it with the same key-hint
  treatment used by the main screen footer.
- Do not add plain instructional text that uses a different visual style.

## Main-screen shortcuts

- `o` opens Sort.
- `s` opens Settings.
- Columns belongs inside Settings rather than in the main-screen action list.
- The main footer must reflect the current shortcut assignments.

## Action layout

- Arrange screen functions from left to right in descending order of expected
  use on that screen.
- Keep screen functions left-aligned.
- Keep general functions, such as Find, Next, Settings, Help, and Quit,
  right-aligned.
- General functions must never overlap the screen-function group.
- When the available width is too small, preserve screen functions and
  truncate or omit general functions before allowing overlap.

## Settings

- Settings is the entry point for user-adjustable dashboard options.
- Settings contains Agents, Appearance, and Static Analysis.
- Agents contains Agent Setup (including Concurrency), Fix Settings, and Git Settings.
- Appearance contains Theme and Columns. Static Analysis contains Files and Weights.
- Settings items use the standard selected-row background treatment.
- Opening a leaf overlays its group and returns to that group and cursor on close.
- Files contains Honor gitignore, checked by default.

## Text wrapping

- Wrapped text must align continuation lines with the text they continue.
- A continuation line must not return to the left edge when the first line has
  a label or prefix.
- Use the shared generic wrapping utility for aligned wrapping. Do not create a
  one-off wrapper for each popup or text block.

## Scope

These rules cover the behaviours agreed for the dashboard during the current
UI work. They do not prescribe colours, dimensions, typography, or controls
that are not stated here.
