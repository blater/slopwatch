"""Interactive, event-driven follow mode for Slopslap reports."""

from __future__ import annotations

import asyncio
import time
from collections import deque
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Callable, Iterable, Mapping

from rich.color import Color
from rich.style import Style
from rich.text import Text
from textual import constants as textual_constants
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical, VerticalScroll
from textual.screen import ModalScreen
from textual.widgets import Button, DataTable, SelectionList, Static
from watchfiles import awatch

from slopslap_runtime.errors import ValidationError as RuntimeValidationError

from .orchestrator import _IGNORED_DIRECTORIES, _is_test_source
from .registry import standard_provider_registry


# Textual otherwise waits 100 ms before deciding whether an ESC byte starts an
# escape sequence. Arrow keys use those sequences, making navigation feel delayed.
textual_constants.ESCAPE_DELAY = 0.005


@dataclass(frozen=True)
class RefreshResult:
  """A full report or a targeted set of rows to merge into the snapshot."""

  document: dict[str, Any]
  replace_paths: frozenset[str]
  full: bool = False


ReportBuilder = Callable[[set[str] | None], RefreshResult]
_BASE_BACKGROUND = (7, 16, 25)
_CHANGE_COLOURS = {
    "better": (48, 220, 157),
    "worse": (255, 82, 105),
    "same": (72, 181, 235),
    "new": (72, 181, 235),
}


@dataclass(frozen=True)
class HistoryPoint:
  at: float
  rank: int
  score: float


@dataclass
class FollowRow:
  document: dict[str, Any]
  history: deque[HistoryPoint] = field(default_factory=deque)
  delta: float | None = None
  changed_at: float | None = None
  change_kind: str = "same"
  change_origin: str = "edit"

  @property
  def path(self) -> str:
    return str(self.document["path"])

  @property
  def rank(self) -> int:
    return int(self.document["rank"])

  @property
  def score(self) -> float:
    return float(self.document["score"])


class FollowState:
  """History and edit-memory independent of the terminal rendering layer."""

  def __init__(self, trend_window: float) -> None:
    if trend_window <= 0:
      raise ValueError("trend_window must be positive")
    self.trend_window = trend_window
    self.rows: dict[str, FollowRow] = {}
    self.order: list[str] = []
    self.summary: Mapping[str, Any] = {}

  def apply(self, document: Mapping[str, Any], changed: Iterable[str], *,
            now: float | None = None) -> None:
    current_time = time.monotonic() if now is None else now
    changed_paths = set(changed)
    previous = self.rows
    updated: dict[str, FollowRow] = {}
    order: list[str] = []
    cutoff = current_time - self.trend_window

    for file_document in document.get("files", ()):
      item = dict(file_document)
      path = str(item["path"])
      prior = previous.get(path)
      if prior is None:
        row = FollowRow(item)
        if previous and path in changed_paths:
          row.changed_at = current_time
          row.change_kind = "new"
      else:
        row = prior
        old_score = row.score
        row.document = item
        score_delta = row.score - old_score
        directly_edited = path in changed_paths
        indirectly_changed = score_delta != 0
        if directly_edited or indirectly_changed:
          row.delta = score_delta
          row.changed_at = current_time
          row.change_kind = (
              "better" if score_delta < 0 else "worse" if score_delta > 0 else "same"
          )
          row.change_origin = "edit" if directly_edited else "affected"

      row.history.append(HistoryPoint(current_time, row.rank, row.score))
      while len(row.history) > 1 and row.history[1].at < cutoff:
        row.history.popleft()
      updated[path] = row
      order.append(path)

    self.rows = updated
    self.order = order
    self.summary = dict(document.get("summary", {}))

  def apply_refresh(self, refresh: RefreshResult, changed: Iterable[str], *,
                    now: float | None = None) -> None:
    if refresh.full:
      self.apply(refresh.document, changed, now=now)
      return
    files = {path: dict(row.document) for path, row in self.rows.items()}
    for path in refresh.replace_paths:
      files.pop(path, None)
    for item in refresh.document.get("files", ()):
      files[str(item["path"])] = dict(item)
    ranked = sorted(
        files.values(),
        key=lambda item: (
            -float(item["score"]), str(item["path"]), str(item.get("language", "")),
        ),
    )
    for rank, item in enumerate(ranked, 1):
      item["rank"] = rank
    summary = dict(self.summary)
    summary["files_analyzed"] = len(ranked)
    summary["complete_files"] = sum(bool(item.get("complete")) for item in ranked)
    if "pass_score" in summary:
      summary["failed_files"] = sum(not bool(item.get("passed")) for item in ranked)
      summary["passed"] = summary["failed_files"] == 0
    self.apply({"files": ranked, "summary": summary}, changed, now=now)

  def rank_shift(self, row: FollowRow, *, now: float | None = None) -> int:
    current_time = time.monotonic() if now is None else now
    recent = [
        point for point in row.history
        if point.at >= current_time - self.trend_window
    ]
    if len(recent) < 2:
      return 0
    return recent[0].rank - row.rank

  def mark_changed(self, paths: Iterable[str], *, now: float | None = None) -> None:
    """Show an edit immediately while its targeted measurement is in flight."""
    current_time = time.monotonic() if now is None else now
    for path in paths:
      row = self.rows.get(path)
      if row is None:
        continue
      row.delta = None
      row.changed_at = current_time
      row.change_kind = "same"
      row.change_origin = "edit"

  def age(self, row: FollowRow, *, now: float | None = None) -> float | None:
    if row.changed_at is None:
      return None
    current_time = time.monotonic() if now is None else now
    return max(0.0, current_time - row.changed_at)

  def background(self, row: FollowRow, *, now: float | None = None
                 ) -> tuple[int, int, int] | None:
    age = self.age(row, now=now)
    if age is None or age >= self.trend_window:
      return None
    flash_period = min(5.0, self.trend_window / 3)
    if age <= flash_period:
      strength = 0.34 - (0.18 * age / max(flash_period, 0.001))
    else:
      remaining = (self.trend_window - age) / max(
          self.trend_window - flash_period, 0.001,
      )
      strength = 0.16 * max(0.0, remaining)
    source = _CHANGE_COLOURS[row.change_kind]
    return tuple(
        round(base + (accent - base) * strength)
        for base, accent in zip(_BASE_BACKGROUND, source)
    )


def _resolved_targets(workspace: Path, targets: Iterable[Path]) -> tuple[Path, ...]:
  supplied = tuple(targets)
  result = []
  for raw in supplied or (workspace,):
    target = raw if raw.is_absolute() else workspace / raw
    target = target.resolve(strict=True)
    try:
      target.relative_to(workspace)
    except ValueError as error:
      raise ValueError(f"follow target escapes workspace: {target}") from error
    result.append(target)
  return tuple(dict.fromkeys(result))


def watch_roots(workspace: Path, targets: Iterable[Path]) -> tuple[Path, ...]:
  """Return a minimal set of directories for native recursive watching."""
  scopes = _resolved_targets(workspace.resolve(), targets)
  candidates = sorted(
      {scope if scope.is_dir() else scope.parent for scope in scopes},
      key=lambda path: (len(path.parts), str(path)),
  )
  roots: list[Path] = []
  for candidate in candidates:
    if not any(candidate == root or candidate.is_relative_to(root) for root in roots):
      roots.append(candidate)
  return tuple(roots)


@dataclass(frozen=True)
class MeasurementPlan:
  """The smallest safe analyzer inventory for a set of changed sources."""

  targets: tuple[Path, ...]
  replace_paths: frozenset[str]
  languages: tuple[str, ...]


def _source_language(workspace: Path, candidate: Path, *, include_tests: bool,
                     languages: frozenset[str] | None = None) -> str | None:
  try:
    relative = candidate.resolve(strict=False).relative_to(workspace)
  except ValueError:
    return None
  if any(part in _IGNORED_DIRECTORIES for part in relative.parts[:-1]):
    return None
  registry = standard_provider_registry()
  try:
    language = registry.language_for_path(relative.as_posix())
  except RuntimeValidationError:
    return None
  if languages is not None and language not in languages:
    return None
  if not include_tests and _is_test_source(relative, language):
    return None
  return language


def _rust_scope(workspace: Path, changed: Path) -> Path:
  current = changed.parent
  while current != workspace and current.parent != current:
    if (current / "Cargo.toml").is_file():
      return current
    current = current.parent
  return workspace


def measurement_plan(workspace: Path, changed: Iterable[str], *,
                     include_tests: bool,
                     languages: Iterable[str] | None = None,
                     scopes: Iterable[Path] = ()) -> MeasurementPlan:
  """Expand edits only where a metric genuinely has cross-file facts.

  Go types and methods can span files in one package, so that package is the
  measurement unit. Rust impl blocks can span a crate, so the nearest crate is
  the measurement unit. Java and TypeScript retain exact-file targets; the
  TypeScript compiler resolves imports for those roots itself.
  """
  workspace = workspace.resolve()
  selected = None if languages is None else frozenset(languages)
  requested_scopes = _resolved_targets(workspace, scopes)
  targets: dict[str, Path] = {}
  replacements: set[str] = set()
  measured_languages: set[str] = set()

  def include(candidate: Path, expected_language: str) -> None:
    if not candidate.is_file():
      return
    resolved = candidate.resolve()
    if not any(
        resolved == scope or (scope.is_dir() and resolved.is_relative_to(scope))
        for scope in requested_scopes
    ):
      return
    language = _source_language(
        workspace, candidate, include_tests=include_tests, languages=selected,
    )
    if language != expected_language:
      return
    relative = resolved.relative_to(workspace).as_posix()
    targets[relative] = resolved
    replacements.add(relative)
    measured_languages.add(language)

  for relative_path in sorted(set(changed)):
    candidate = (workspace / relative_path).resolve(strict=False)
    language = _source_language(
        workspace, candidate, include_tests=include_tests, languages=selected,
    )
    if language is None:
      continue
    replacements.add(relative_path)
    if language == "go":
      for sibling in candidate.parent.glob("*.go"):
        include(sibling, language)
    elif language == "rust":
      for sibling in _rust_scope(workspace, candidate).rglob("*.rs"):
        include(sibling, language)
    else:
      include(candidate, language)

  return MeasurementPlan(
      tuple(targets[path] for path in sorted(targets)),
      frozenset(replacements), tuple(sorted(measured_languages)),
  )


class SourceWatchFilter:
  """Limit events to eligible sources inside the exact requested scopes."""

  def __init__(self, workspace: Path, targets: Iterable[Path], *,
               include_tests: bool,
               languages: Iterable[str] | None = None) -> None:
    self.workspace = workspace.resolve()
    self.scopes = _resolved_targets(self.workspace, targets)
    self.include_tests = include_tests
    self.languages = None if languages is None else frozenset(languages)
    self.registry = standard_provider_registry()

  def __call__(self, change: object, path: str) -> bool:
    del change
    candidate = Path(path).resolve(strict=False)
    if not any(
        candidate == scope or (scope.is_dir() and candidate.is_relative_to(scope))
        for scope in self.scopes
    ):
      return False
    try:
      relative = candidate.relative_to(self.workspace)
    except ValueError:
      return False
    if any(part in _IGNORED_DIRECTORIES for part in relative.parts[:-1]):
      return False
    try:
      language = self.registry.language_for_path(relative.as_posix())
    except RuntimeValidationError:
      return False
    if self.languages is not None and language not in self.languages:
      return False
    return self.include_tests or not _is_test_source(relative, language)


@dataclass(frozen=True)
class ColumnSpec:
  key: str
  heading: str
  width: int | None


_COLUMNS = (
    ColumnSpec("score", "SCORE", 9),
    ColumnSpec("cog", "COG", 8),
    ColumnSpec("npath", "NPATH", 9),
    ColumnSpec("cyclo", "CYCLO", 8),
    ColumnSpec("deep", "SHALLOW", 7),
    ColumnSpec("god", "GOD", 8),
    ColumnSpec("path", "", None),
)
_COLUMN_SPECS = {column.key: column for column in _COLUMNS}
_FIXED_COLUMNS = frozenset({"score", "path"})


def _table_cell(key: str, value: Text) -> Text:
  """Fit a cell while reserving exactly one trailing column separator."""
  width = _COLUMN_SPECS[key].width
  if width is None:
    return value
  separator_width = 2 if key in {"score", "god"} else 1
  result = value.copy()
  result.truncate(width - separator_width, overflow="ellipsis")
  result.align(
      "right" if key in {"score", "god"} else "left",
      width - separator_width,
  )
  result.append(" " * separator_width)
  return result


def _display_number(value: Any) -> str:
  if isinstance(value, int):
    return str(value)
  if isinstance(value, float):
    return f"{value:.1f}".rstrip("0").rstrip(".")
  return str(value)


def _display_decimal(value: Any) -> str:
  if isinstance(value, (int, float)):
    return f"{float(value):.1f}"
  return str(value)


def _display_decimal_within(value: Any, width: int) -> str:
  result = _display_decimal(value)
  if len(result) <= width or not isinstance(value, (int, float)):
    return result[:width]
  mantissa, exponent = f"{float(value):.1e}".split("e")
  result = f"{mantissa}e{int(exponent)}"
  if len(result) <= width:
    return result
  return result[:width]


def _component_values(item: Mapping[str, Any], component_id: str) -> list[Any] | None:
  component = item.get("components", {}).get(component_id)
  if component is None:
    return None
  return [subject["value"] for subject in component["subjects"]]


def _maximum(item: Mapping[str, Any], component_id: str) -> str:
  values = _component_values(item, component_id)
  if values is None:
    return "-"
  if not values:
    return "0"
  return _display_number(max(values))


def _cyclomatic_maximum(item: Mapping[str, Any]) -> str:
  method_values = _component_values(item, "cyclomatic_method_complexity")
  if method_values is None:
    return "-"
  return _display_number(max(method_values, default=0))


def _depth(item: Mapping[str, Any]) -> str:
  values = _component_values(item, "module_shallowness")
  return "-" if values is None else _display_number(max(values, default=0))


def _god(item: Mapping[str, Any]) -> str:
  component = item.get("components", {}).get("god_class")
  return "-" if component is None else _display_decimal(component["contribution"])


def _metric_summary(item: Mapping[str, Any]) -> list[tuple[str, str]]:
  cognitive = _component_values(item, "cognitive_complexity")
  npath = _component_values(item, "npath_complexity")
  cyclo_types = _component_values(item, "cyclomatic_class_complexity")
  cyclo_methods = _component_values(item, "cyclomatic_method_complexity")
  shallow = _component_values(item, "module_shallowness")
  god = item.get("components", {}).get("god_class")

  def maximum_and_count(values: list[Any] | None) -> str:
    if values is None:
      return "unavailable"
    return f"maximum {_display_number(max(values, default=0))} across {len(values)} routines"

  method_maximum = "unavailable" if cyclo_methods is None else _display_number(
      max(cyclo_methods, default=0),
  )
  type_total = "unavailable" if cyclo_types is None else _display_number(
      max(cyclo_types, default=0),
  )
  return [
      ("Cognitive complexity", maximum_and_count(cognitive)),
      ("NPath complexity", maximum_and_count(npath)),
      ("Cyclomatic complexity",
       f"method maximum {method_maximum}; largest class/type total {type_total}"),
      ("Module shallowness", "unavailable" if shallow is None else f"{max(shallow, default=0)}/100 penalty"),
      ("God class score", "unavailable" if god is None else _display_decimal(
          god.get("contribution", 0),
      )),
  ]


def _age_label(age: float | None) -> str:
  if age is None:
    return ""
  if age < 60:
    return f"{max(1, int(age))}s"
  return f"{int(age // 60)}m"


def _metric(value: str, level: str = "normal") -> Text:
  if value.strip() == "-":
    return Text(value, style="#526b80")
  colours = {"normal": "#58e7ad", "warm": "#f0c765", "hot": "#ff8291"}
  return Text(value, style=colours[level])


def _rank_indicator(row: FollowRow, state: FollowState, now: float) -> Text:
  shift = state.rank_shift(row, now=now)
  result = Text()
  if shift > 0:
    result.append("↑", style="#ff6174")
  elif shift < 0:
    result.append("↓", style="#58e7ad")
  return result


def _score(row: FollowRow, state: FollowState, now: float) -> Text:
  score = row.score
  style = "#58e7ad" if score < 50 else "#f5c451" if score < 100 else "#ff6174"
  indicator = _rank_indicator(row, state, now)
  number = _display_decimal_within(
      row.document["score"], 7 - len(indicator.plain),
  )
  result = Text(number, style=style)
  if indicator.plain:
    result.append_text(indicator)
  return result


def _path_text(path: str) -> Text:
  parent, separator, name = path.rpartition("/")
  result = Text()
  if separator:
    result.append(parent + "/", style="#607b91")
  result.append(name or path, style="#d5e2eb")
  return result


def _sparkline(values: Iterable[float]) -> str:
  samples = list(values)[-18:]
  if not samples:
    return ""
  low, high = min(samples), max(samples)
  blocks = "▁▂▃▄▅▆▇█"
  if high == low:
    return blocks[3] * len(samples)
  return "".join(blocks[round((value - low) * 7 / (high - low))] for value in samples)


class ScoreTable(DataTable[Any]):
  """DataTable with whole-row edit-memory shading."""

  follow_state: FollowState | None = None

  def _get_row_style(self, row_index: int, base_style: Style) -> Style:
    style = super()._get_row_style(row_index, base_style)
    if self.follow_state is None or not self.is_valid_row_index(row_index):
      return style
    key = self.ordered_rows[row_index].key.value
    row = None if key is None else self.follow_state.rows.get(key)
    colour = None if row is None else self.follow_state.background(row)
    if colour is None:
      return style
    return style + Style(bgcolor=Color.from_rgb(*colour))


class ColumnsScreen(ModalScreen[set[str] | None]):
  """Small keyboard-friendly chooser for optional report columns."""

  CSS = """
  ColumnsScreen { align: center middle; background: #02070db0; }
  #columns-dialog { width: 48; height: auto; max-height: 90%; padding: 1 2;
                    background: #0a1723; border: round #31506a; }
  #columns-title { height: 2; color: #68f6c8; text-style: bold; }
  #columns-list { height: 18; background: #08131e; }
  #columns-actions { height: 3; align-horizontal: right; }
  #columns-actions Button { margin-left: 1; }
  """

  BINDINGS = [Binding("escape", "cancel", "Cancel")]

  def __init__(self, visible: set[str]) -> None:
    super().__init__()
    self.selected_columns = visible

  def compose(self) -> ComposeResult:
    choices = [
        (column.heading, column.key, column.key in self.selected_columns)
        for column in _COLUMNS if column.key not in _FIXED_COLUMNS
    ]
    with Vertical(id="columns-dialog"):
      yield Static("VISIBLE COLUMNS", id="columns-title")
      yield SelectionList(*choices, id="columns-list")
      with Horizontal(id="columns-actions"):
        yield Button("Cancel", id="columns-cancel")
        yield Button("Apply", id="columns-apply", variant="primary")

  def on_button_pressed(self, event: Button.Pressed) -> None:
    if event.button.id == "columns-apply":
      selected = set(self.query_one("#columns-list", SelectionList).selected)
      self.dismiss(selected | set(_FIXED_COLUMNS))
    else:
      self.dismiss(None)

  def action_cancel(self) -> None:
    self.dismiss(None)


_SORT_FIELDS = (
    ("score", "Score"), ("cog", "COG"),
    ("npath", "NPath"), ("cyclo", "Cyclomatic"), ("deep", "Shallowness"),
    ("god", "God"), ("filename", "Filename"),
)


class SortScreen(ModalScreen[tuple[str, bool] | None]):
  """Choose one overview field and its direction."""

  CSS = """
  SortScreen { align: center middle; background: #02070db0; }
  #sort-dialog { width: 48; height: auto; padding: 1 2;
                 background: #0a1723; border: round #31506a; }
  #sort-title { height: 2; color: #68f6c8; text-style: bold; }
  #sort-options { height: 10; color: #d5e2eb; }
  #sort-help { height: 2; color: #668298; }
  """

  BINDINGS = [
      Binding("escape", "cancel", "Cancel"),
      Binding("q", "cancel", "Cancel"),
      Binding("up,k", "previous", "Previous"),
      Binding("down,j", "next", "Next"),
      Binding("left,h", "ascending", "Ascending"),
      Binding("right,l", "descending", "Descending"),
      Binding("space", "toggle_direction", "Toggle direction"),
      Binding("enter", "apply", "Apply"),
  ]

  def __init__(self, key: str, reverse: bool) -> None:
    super().__init__()
    self.cursor = next(
        (index for index, item in enumerate(_SORT_FIELDS) if item[0] == key), 0,
    )
    self.reverse = reverse

  def compose(self) -> ComposeResult:
    with Vertical(id="sort-dialog"):
      yield Static("SORT RESULTS", id="sort-title")
      yield Static(id="sort-options")
      yield Static("←/→ direction   Enter apply   Esc cancel", id="sort-help")

  def on_mount(self) -> None:
    self._render_options()

  def _render_options(self) -> None:
    content = Text()
    for index, (_, label) in enumerate(_SORT_FIELDS):
      selected = index == self.cursor
      content.append("› " if selected else "  ", style="#6fb9e8" if selected else "")
      content.append(f"{label:<12}")
      if selected:
        content.append("descending" if self.reverse else "ascending", style="#58e7ad")
      content.append("\n")
    self.query_one("#sort-options", Static).update(content)

  def action_previous(self) -> None:
    self.cursor = max(0, self.cursor - 1)
    self._render_options()

  def action_next(self) -> None:
    self.cursor = min(len(_SORT_FIELDS) - 1, self.cursor + 1)
    self._render_options()

  def action_ascending(self) -> None:
    self.reverse = False
    self._render_options()

  def action_descending(self) -> None:
    self.reverse = True
    self._render_options()

  def action_toggle_direction(self) -> None:
    self.reverse = not self.reverse
    self._render_options()

  def action_apply(self) -> None:
    self.dismiss((_SORT_FIELDS[self.cursor][0], self.reverse))

  def action_cancel(self) -> None:
    self.dismiss(None)


def _full_details(row: FollowRow, state: FollowState) -> Text:
  age = state.age(row)
  shift = state.rank_shift(row)
  content = Text()
  content.append(row.path, style="bold #d8e7f0")
  content.append(f"\n{row.document.get('language', 'unknown')}  ·  rank {row.rank}",
                 style="#7893a7")
  content.append("  ·  score ")
  score_style = "#58e7ad" if row.score < 50 else "#f5c451" if row.score < 100 else "#ff6174"
  content.append(_display_decimal(row.document["score"]), style=score_style)
  if shift:
    direction = "up" if shift > 0 else "down"
    content.append(f"  ·  moved {direction} {abs(shift)}", style=(
        "#ff6174" if shift > 0 else "#58e7ad"
    ))
  if row.delta is not None:
    content.append(f"  ·  Δ {row.delta:+.3g}", style=(
        "#58e7ad" if row.delta < 0 else "#ff6174" if row.delta > 0 else "#6fb9e8"
    ))
  if age is not None and age < state.trend_window:
    content.append(f"  ·  {row.change_origin} {_age_label(age)} ago", style="#79a3ba")
  content.append("\n\nSCORE HISTORY  ", style="bold #68f6c8")
  content.append(_sparkline(point.score for point in row.history) or "—", style="#4cdfaa")
  content.append("\n\nMETRIC SUMMARY\n", style="bold #68f6c8")
  for label, description in _metric_summary(row.document):
    content.append(f"\n{label:<24}", style="bold #b9cad8")
    content.append(description, style="#91aabd")
  content.append("\n\nCOMPONENTS\n", style="bold #68f6c8")

  components = row.document.get("components", {})
  if not components:
    content.append("No component measurements.\n", style="#668298")
  for component_id, component in sorted(components.items()):
    content.append(f"\n{component_id}", style="bold #b9cad8")
    contribution = component.get("contribution", 0)
    contribution_text = (
        _display_decimal(contribution) if component_id == "god_class"
        else _display_number(contribution)
    )
    content.append(
        f"  contribution {contribution_text}"
        f"  ·  observations {component.get('observations', 0)}\n",
        style="#8ca6b9",
    )
    subjects = component.get("subjects", ())
    for subject in subjects:
      content.append("  • ", style="#526b80")
      content.append(str(subject.get("subject", "")), style="#91aabd")
      content.append(
          f" = {_display_number(subject.get('value'))}"
          f"  (+{_display_number(subject.get('contribution', 0))})\n",
          style="#7893a7",
      )
    for waiver in component.get("waivers", ()):
      content.append(
          f"  waiver {waiver.get('waiver_id', '')}: {waiver.get('status', '')}"
          f" — {waiver.get('reason', '')}\n",
          style="#f0c765",
      )
  return content


class HelpScreen(ModalScreen[None]):
  """Compact column glossary shown without leaving the ranking view."""

  CSS = """
  HelpScreen { align: center middle; background: #02070dcc; }
  #help-dialog { width: 92%; height: 14; padding: 1 2;
                 background: #0d1d29; border: round #31506a; }
  """

  BINDINGS = [
      Binding("escape", "close", "Close", priority=True),
      Binding("q", "close", "Close", priority=True),
      Binding("h", "close", "Close", priority=True),
  ]

  def compose(self) -> ComposeResult:
    with Vertical(id="help-dialog"):
      yield Static("HELP", classes="title")
      yield Static(
          "SCORE   weighted total of enabled metrics; COG, NPath, Cyclo, "
          "SHALLOW and God contributions\n"
          "COG     cognitive effort from nested decisions\n"
          "NPATH   estimated distinct execution routes through a routine\n"
          "CYCLO   independent control-flow paths\n"
          "SHALLOW Ousterhout-inspired 0–100 module shallowness penalty; lower is better\n"
          "GOD     large, externally coupled, low-cohesion type contribution\n"
          "PATH    source file; press Esc, q or h to close",
      )

  def action_close(self) -> None:
    self.dismiss(None)


class DetailsScreen(ModalScreen[None]):
  """Scrollable full analysis for the selected file."""

  CSS = """
  DetailsScreen { align: center middle; background: #02070dcc; }
  #details-dialog { width: 92%; height: 88%; background: #091723;
                    border: round #31506a; }
  #details-title { height: 3; padding: 1 2; background: #0b1e2d;
                   color: #68f6c8; text-style: bold; }
  #details-scroll { height: 1fr; padding: 0 2 1 2;
                    scrollbar-color: #31536b; scrollbar-background: #0a1723; }
  """

  BINDINGS = [
      Binding("escape", "close", "Close", priority=True),
      Binding("q", "close", "Close", priority=True),
  ]

  def __init__(self, content: Text) -> None:
    super().__init__()
    self.details = content

  def compose(self) -> ComposeResult:
    with Vertical(id="details-dialog"):
      yield Static("FULL ANALYSIS                                      ESC / Q  CLOSE",
                   id="details-title")
      with VerticalScroll(id="details-scroll"):
        yield Static(self.details)

  def action_close(self) -> None:
    self.dismiss(None)


class FollowApp(App[int]):
  """Polished live ranking dashboard backed by native filesystem events."""

  CSS = """
  Screen { background: #071019; color: #c8d7e2; }
  #follow-top { height: 1; padding: 0 1; color: #9fb4c6;
                background: #0a1622; }
  #results { height: 1fr; background: #071019; color: #c5d4df;
             overflow-x: hidden; scrollbar-size-horizontal: 0;
             scrollbar-color: #31536b; scrollbar-background: #0a1723; }
  #results > .datatable--header { background: #0b1e2d; color: #6f8ca2;
                                  text-style: bold; }
  #results > .datatable--cursor { background: #183b52; text-style: none; }
  #results > .datatable--hover { background: #102638; }
  #follow-keys { height: 1; padding: 0 1; color: #668298;
                 background: #061019; }
  """

  BINDINGS = [
      Binding("ctrl+c", "quit_follow", "Quit", priority=True, show=False),
      Binding("ctrl+f", "page_forward", "Page forward", show=False),
      Binding("ctrl+b", "page_back", "Page back", show=False),
      Binding("q", "quit_follow", "Quit"),
      Binding("c", "columns", "Columns"),
      Binding("s", "sort", "Sort"),
      Binding("r", "rescan", "Rescan"),
      Binding("h", "help", "Help"),
  ]

  def __init__(self, initial_document: dict[str, Any], analyzer: ReportBuilder, *,
               workspace: Path, targets: Iterable[Path], include_tests: bool,
               trend_window: float, compact: bool = False, limit: int = 0,
               languages: Iterable[str] | None = None,
               watch: bool = True) -> None:
    super().__init__()
    self.workspace = workspace.resolve()
    self.targets = tuple(targets)
    self.analyzer = analyzer
    self.state = FollowState(trend_window)
    self.state.apply(initial_document, (), now=time.monotonic())
    self.visible_columns = (
        set(_FIXED_COLUMNS) if compact else {column.key for column in _COLUMNS}
    )
    self.include_tests = include_tests
    self.languages = None if languages is None else tuple(languages)
    self.limit = limit
    self.watch_enabled = watch
    self.roots = watch_roots(self.workspace, self.targets)
    self.pending_changes: set[str] = set()
    self.scanning = False
    self.rescan_requested = False
    self.last_error: str | None = None
    self.sort_key = "score"
    self.sort_reverse = True
    self.selected_path: str | None = self.state.order[0] if self.state.order else None
    self.rendered_scores: dict[str, str] = {}

  def compose(self) -> ComposeResult:
    yield Static(id="follow-top")
    yield ScoreTable(
        id="results", cursor_type="row", zebra_stripes=False,
        cell_padding=0,
        cursor_foreground_priority="renderable",
        cursor_background_priority="css",
    )
    yield Static(id="follow-keys")

  def on_mount(self) -> None:
    table = self.query_one("#results", ScoreTable)
    table.follow_state = self.state
    self._rebuild_table()
    self._update_chrome()
    self.set_interval(1, self._refresh_dynamic)
    if self.watch_enabled:
      self.run_worker(self._watch_sources(), name="source watcher", exit_on_error=False)

  async def _watch_sources(self) -> None:
    source_filter = SourceWatchFilter(
        self.workspace, self.targets, include_tests=self.include_tests,
        languages=self.languages,
    )
    try:
      async for changes in awatch(
          *self.roots, watch_filter=source_filter, debounce=300, step=50,
          rust_timeout=5000,
      ):
        relative: set[str] = set()
        for _, changed_path in changes:
          try:
            relative.add(
                Path(changed_path).resolve(strict=False)
                .relative_to(self.workspace).as_posix()
            )
          except ValueError:
            continue
        if relative:
          self._queue_scan(relative)
    except asyncio.CancelledError:
      raise
    except Exception as error:
      self.last_error = f"watcher: {error}"
      self._update_chrome()

  def _queue_scan(self, changed: Iterable[str] = (), *, force: bool = False) -> None:
    changed_paths = set(changed)
    if changed_paths:
      self.state.mark_changed(changed_paths)
      self.query_one("#results", ScoreTable).refresh()
    self.pending_changes.update(changed_paths)
    self.rescan_requested = self.rescan_requested or force
    self._update_chrome()
    if not self.scanning:
      self._start_scan()

  def _start_scan(self) -> None:
    if self.scanning:
      return
    if not self.pending_changes and not self.rescan_requested:
      return
    changed = set(self.pending_changes)
    self.pending_changes.clear()
    self.rescan_requested = False
    self.scanning = True
    self.last_error = None
    self._update_chrome()
    self.run_worker(
        lambda: self._scan_in_thread(changed), name="analysis", group="analysis",
        thread=True, exit_on_error=False,
    )

  def _scan_in_thread(self, changed: set[str]) -> None:
    try:
      refresh = self.analyzer(None if not changed else changed)
    except Exception as error:
      self.call_from_thread(self._scan_failed, str(error))
      return
    self.call_from_thread(self._scan_completed, refresh, changed)

  def _scan_completed(self, refresh: RefreshResult, changed: set[str]) -> None:
    self.state.apply_refresh(refresh, changed)
    self.scanning = False
    self.last_error = None
    display_paths = self._display_paths()
    if self.selected_path not in display_paths:
      self.selected_path = display_paths[0] if display_paths else None
    self._rebuild_table()
    self._update_chrome()
    if self.pending_changes or self.rescan_requested:
      self._start_scan()

  def _scan_failed(self, message: str) -> None:
    self.scanning = False
    self.last_error = message
    self._update_chrome()
    if self.pending_changes or self.rescan_requested:
      self._start_scan()

  def _cells(self, row: FollowRow, now: float) -> dict[str, Any]:
    item = row.document
    cognitive = _component_values(item, "cognitive_complexity")
    npath = _component_values(item, "npath_complexity")
    method_cyclo = _component_values(item, "cyclomatic_method_complexity")
    shallow_values = _component_values(item, "module_shallowness")
    shallow_value = None if shallow_values is None else max(shallow_values, default=0)
    god_value = item.get("components", {}).get("god_class", {}).get("contribution")
    cog_level = "hot" if cognitive and max(cognitive) >= 25 else "warm" if cognitive and max(cognitive) >= 15 else "normal"
    npath_level = "hot" if npath and max(npath) >= 200 else "warm" if npath and max(npath) >= 100 else "normal"
    cyclo_level = "hot" if method_cyclo and max(method_cyclo) >= 20 else "warm" if method_cyclo and max(method_cyclo) >= 10 else "normal"
    shallow_level = (
        "hot" if shallow_value is not None and shallow_value >= 60
        else "warm" if shallow_value is not None and shallow_value >= 30
        else "normal"
    )
    god_level = "hot" if god_value is not None and float(god_value) >= 20 else "warm" if god_value else "normal"
    cells = {
        "score": _score(row, self.state, now),
        "cog": _metric(_maximum(item, "cognitive_complexity"), cog_level),
        "npath": _metric(_maximum(item, "npath_complexity"), npath_level),
        "cyclo": _metric(_cyclomatic_maximum(item), cyclo_level),
        "deep": _metric(_depth(item), shallow_level),
        "god": _metric(_god(item), god_level),
        "path": _path_text(row.path),
    }
    return {key: _table_cell(key, value) for key, value in cells.items()}

  def _rebuild_table(self) -> None:
    table = self.query_one("#results", ScoreTable)
    table.clear(columns=True)
    visible = [column for column in _COLUMNS if column.key in self.visible_columns]
    indicator = "▼" if self.sort_reverse else "▲"
    headings = [Text(column.heading) for column in visible]
    active = next(
        (index for index, column in enumerate(visible)
         if column.key == self.sort_key), None,
    )
    if active is not None:
      column = visible[active]
      marked = indicator if column.key == "path" else indicator + column.heading
      available = None if column.width is None else column.width - 1
      if available is None or len(marked) <= available:
        headings[active] = Text(marked)
      elif active > 0:
        previous = _table_cell(visible[active - 1].key, headings[active - 1])
        headings[active - 1] = Text(previous.plain[:-1] + indicator)
    for index, column in enumerate(visible):
      heading = headings[index]
      if column.width is not None:
        already_fitted = active == index + 1 and heading.plain.endswith(indicator)
        if not already_fitted:
          heading = _table_cell(column.key, heading)
      table.add_column(heading, key=column.key, width=column.width)
    now = time.monotonic()
    rendered_scores: dict[str, str] = {}
    for path in self._display_paths():
      row = self.state.rows[path]
      cells = self._cells(row, now)
      rendered_scores[path] = cells["score"].plain
      table.add_row(*(cells[column.key] for column in visible), key=path)
    self.rendered_scores = rendered_scores
    display_paths = self._display_paths()
    if self.selected_path in display_paths:
      table.move_cursor(row=display_paths.index(self.selected_path), column=0)

  def _display_paths(self) -> list[str]:
    paths = sorted(self.state.order)
    available: list[tuple[str, Any]] = []
    missing: list[str] = []
    for path in paths:
      value = self._sort_value(self.state.rows[path])
      if value is None:
        missing.append(path)
      else:
        available.append((path, value))
    available.sort(key=lambda item: item[1], reverse=self.sort_reverse)
    ordered = [path for path, _ in available] + missing
    return ordered if self.limit == 0 else ordered[:self.limit]

  def _sort_value(self, row: FollowRow) -> Any | None:
    if self.sort_key == "score":
      return row.score
    if self.sort_key == "filename":
      return (Path(row.path).name.casefold(), row.path.casefold())
    component_id = {
        "cog": "cognitive_complexity",
        "npath": "npath_complexity",
        "cyclo": "cyclomatic_method_complexity",
    }.get(self.sort_key)
    if component_id is not None:
      values = _component_values(row.document, component_id)
      return None if values is None else max(values, default=0)
    if self.sort_key == "deep":
      values = _component_values(row.document, "module_shallowness")
      return None if values is None else max(values, default=0)
    if self.sort_key == "god":
      component = row.document.get("components", {}).get("god_class")
      return None if component is None else float(component.get("contribution", 0))
    return row.rank

  def _refresh_dynamic(self) -> None:
    table = self.query_one("#results", ScoreTable)
    now = time.monotonic()
    for index, path in enumerate(self._display_paths()):
      row = self.state.rows[path]
      if "score" in self.visible_columns:
        score = _table_cell("score", _score(row, self.state, now))
        if self.rendered_scores.get(path) != score.plain:
          self.rendered_scores[path] = score.plain
          table.update_cell(path, "score", score)
      if self.state.background(row, now=now) is not None:
        table.refresh_row(index)

  def _update_chrome(self) -> None:
    top = Text(str(self.workspace), style="#9fb4c6")
    if self.last_error:
      top.append(f"  ERROR {self.last_error}", style="#ff6174")
    else:
      if self.scanning:
        top.append("  SCANNING", style="bold #f5c451")
      if self.pending_changes:
        top.append(f"  {len(self.pending_changes)} queued", style="#f5c451")
    self.query_one("#follow-top", Static).update(top)

    keys = Text()
    for key, label in (("c", "columns"), ("s", "sort"),
                       ("r", "rescan"), ("q", "quit")):
      keys.append(key, style="#6fb9e8")
      keys.append(f" {label}  ", style="#668298")
    self.query_one("#follow-keys", Static).update(keys)

  def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
    if event.row_key.value is not None:
      self.selected_path = event.row_key.value

  def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
    if event.row_key.value is not None:
      self.selected_path = event.row_key.value
    self.action_details()

  def action_details(self) -> None:
    if self.selected_path is None or self.selected_path not in self.state.rows:
      return
    row = self.state.rows[self.selected_path]
    self.push_screen(DetailsScreen(_full_details(row, self.state)))

  def action_help(self) -> None:
    self.push_screen(HelpScreen())

  def action_columns(self) -> None:
    self.push_screen(ColumnsScreen(set(self.visible_columns)), self._columns_selected)

  def _columns_selected(self, selected: set[str] | None) -> None:
    if selected is not None:
      self.visible_columns = selected
      self._rebuild_table()
      self._update_chrome()

  def action_sort(self) -> None:
    self.push_screen(
        SortScreen(self.sort_key, self.sort_reverse), self._sort_selected,
    )

  def _sort_selected(self, selected: tuple[str, bool] | None) -> None:
    if selected is None:
      return
    self.sort_key, self.sort_reverse = selected
    display_paths = self._display_paths()
    if self.selected_path not in display_paths:
      self.selected_path = display_paths[0] if display_paths else None
    self._rebuild_table()
    self._update_chrome()

  def action_rescan(self) -> None:
    self._queue_scan(force=True)

  def action_page_forward(self) -> None:
    self.query_one("#results", ScoreTable).action_page_down()

  def action_page_back(self) -> None:
    self.query_one("#results", ScoreTable).action_page_up()

  def action_quit_follow(self) -> None:
    passed = self.state.summary.get("passed", True)
    self.exit(0 if passed else 3)


def run_follow(initial_document: dict[str, Any], analyzer: ReportBuilder, *,
               workspace: Path, targets: Iterable[Path], include_tests: bool,
               trend_window: float = 900, compact: bool = False, limit: int = 0,
               languages: Iterable[str] | None = None) -> int:
  """Run follow mode and return its eventual process status."""
  app = FollowApp(
      initial_document, analyzer, workspace=workspace, targets=targets,
      include_tests=include_tests, trend_window=trend_window, compact=compact,
      limit=limit, languages=languages,
  )
  # Follow mode is keyboard-driven. Do not request terminal mouse tracking:
  # leaving the pointer to the terminal preserves native text selection/copy.
  result = app.run(mouse=False)
  return 0 if result is None else result
