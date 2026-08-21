from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from rich.text import Text

from slopslap_app.follow import (
    _BASE_BACKGROUND, _cyclomatic_maximum, _display_number, _god, _maximum,
    _metric, _metric_summary, _score, _table_cell, FollowApp, FollowState,
    RefreshResult, measurement_plan, run_follow,
)
from textual import constants as textual_constants


def _file(path: str, score: float, rank: int) -> dict[str, object]:
  return {
      "path": path, "language": "go", "score": score, "rank": rank,
      "complete": True, "components": {},
  }


class FollowStateTests(unittest.TestCase):
  def test_follow_uses_a_short_terminal_escape_delay(self) -> None:
    self.assertEqual(textual_constants.ESCAPE_DELAY, 0.005)

  def test_compliant_metrics_are_green_but_missing_metrics_are_grey(self) -> None:
    self.assertEqual(_metric("7/1").style, "#58e7ad")
    self.assertEqual(_metric("-").style, "#526b80")

  def test_displayed_measurements_never_exceed_one_decimal_place(self) -> None:
    self.assertEqual(_display_number(82.567), "82.6")
    self.assertEqual(_display_number(82.500), "82.5")
    self.assertEqual(_display_number(82.0), "82")

  def test_dense_metric_cells_keep_exactly_one_separator(self) -> None:
    for key, value in (
        ("cog", "1234567"), ("npath", "12345678"),
        ("cyclo", "1234567"),
    ):
      self.assertEqual(_table_cell(key, Text(value)).plain, value + " ")
    self.assertEqual(_table_cell("god", Text("100.0")).plain, " 100.0  ")

  def test_overview_uses_one_maximum_and_details_label_all_metrics(self) -> None:
    def component(*values: int, contribution: float = 0) -> dict[str, object]:
      return {
          "contribution": contribution,
          "subjects": [{"value": value} for value in values],
      }

    item = {"components": {
        "cognitive_complexity": component(5, 30),
        "npath_complexity": component(12, 38),
        "cyclomatic_method_complexity": component(4, 18),
        "cyclomatic_class_complexity": component(80, 150),
        "deeply_nested_if": component(1, 1),
        "god_class": component(contribution=82.5),
    }}

    self.assertEqual(_maximum(item, "cognitive_complexity"), "30")
    self.assertEqual(_maximum(item, "npath_complexity"), "38")
    self.assertEqual(_cyclomatic_maximum(item), "18")
    self.assertEqual(dict(_metric_summary(item)), {
        "Cognitive complexity": "maximum 30 across 2 routines",
        "NPath complexity": "maximum 38 across 2 routines",
        "Cyclomatic complexity":
            "method maximum 18; largest class/type total 150",
        "Deep nesting": "2 findings",
        "God class score": "82.5",
    })

  def test_rank_arrow_appears_only_inside_score_when_rank_changes(self) -> None:
    state = FollowState(900)
    state.apply({"files": [
        _file("a.go", 100, 1), _file("b.go", 80, 2),
    ]}, (), now=0)
    self.assertEqual(_score(state.rows["a.go"], state, 1).plain.strip(), "100.0")

    state.apply({"files": [
        _file("b.go", 120, 1), _file("a.go", 100, 2),
    ]}, {"a.go", "b.go"}, now=10)
    self.assertEqual(_score(state.rows["a.go"], state, 11).plain.strip(), "100.0↓")

  def test_exact_god_value_keeps_one_decimal_place(self) -> None:
    self.assertEqual(_god({"components": {
        "god_class": {"contribution": 100},
    }}), "100.0")

  def test_targeted_refresh_merges_and_reranks_without_losing_other_files(self) -> None:
    state = FollowState(900)
    state.apply({"files": [
        _file("a.go", 100, 1), _file("b.go", 80, 2), _file("c.go", 60, 3),
    ], "summary": {"files_analyzed": 3}}, (), now=0)
    state.apply_refresh(RefreshResult(
        {"files": [_file("b.go", 120, 1)], "summary": {}},
        frozenset({"b.go"}),
    ), {"b.go"}, now=10)

    self.assertEqual(state.order, ["b.go", "a.go", "c.go"])
    self.assertEqual(state.rows["b.go"].delta, 40)
    self.assertEqual(state.rank_shift(state.rows["b.go"], now=11), 1)
    self.assertEqual(state.rank_shift(state.rows["b.go"], now=911), 0)
    self.assertIsNotNone(state.background(state.rows["b.go"], now=11))
    self.assertIsNone(state.background(state.rows["b.go"], now=910))

  def test_measurement_plan_expands_only_cross_file_language_units(self) -> None:
    with tempfile.TemporaryDirectory() as temporary:
      root = Path(temporary).resolve()
      for relative, content in {
          "go/a.go": "package sample\n",
          "go/b.go": "package sample\n",
          "go/a_test.go": "package sample\n",
          "go/nested/c.go": "package nested\n",
          "rust/Cargo.toml": "[package]\nname='sample'\nversion='0.1.0'\n",
          "rust/src/a.rs": "struct A;\n",
          "rust/src/nested/b.rs": "struct B;\n",
          "java/A.java": "class A {}\n",
          "java/B.java": "class B {}\n",
      }.items():
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

      plan = measurement_plan(
          root, {"go/a.go", "rust/src/a.rs", "java/A.java"},
          include_tests=False,
      )
      relative = {path.relative_to(root).as_posix() for path in plan.targets}
      self.assertEqual(relative, {
          "go/a.go", "go/b.go", "rust/src/a.rs", "rust/src/nested/b.rs",
          "java/A.java",
      })
      self.assertNotIn("go/a_test.go", relative)
      self.assertNotIn("go/nested/c.go", relative)

      exact = measurement_plan(
          root, {"go/a.go"}, include_tests=False, scopes=(root / "go/a.go",),
      )
      self.assertEqual(
          {path.relative_to(root).as_posix() for path in exact.targets},
          {"go/a.go"},
      )


class FollowAppTests(unittest.IsolatedAsyncioTestCase):
  async def test_idle_animation_tick_does_not_rewrite_or_refresh_rows(self) -> None:
    document = {
        "summary": {"files_analyzed": 2},
        "files": [_file("a.go", 100, 1), _file("b.go", 80, 2)],
    }
    app = FollowApp(
        document, lambda changed: RefreshResult(document, frozenset(), full=True),
        workspace=Path.cwd(), targets=(Path("."),), include_tests=False,
        trend_window=900, watch=False,
    )
    async with app.run_test(size=(100, 20)) as pilot:
      table = app.query_one("#results")
      with (
          patch.object(table, "update_cell", wraps=table.update_cell) as update_cell,
          patch.object(table, "refresh_row", wraps=table.refresh_row) as refresh_row,
      ):
        app._refresh_dynamic()
        self.assertEqual(update_cell.call_count, 0)
        self.assertEqual(refresh_row.call_count, 0)

        app.state.mark_changed({"a.go"})
        app._refresh_dynamic()
        self.assertEqual(update_cell.call_count, 0)
        self.assertEqual(refresh_row.call_count, 1)
      app.exit(0)
      await pilot.pause()

  async def test_ctrl_f_and_ctrl_b_move_by_a_visible_page(self) -> None:
    files = [_file(f"file-{index}.go", 100 - index, index + 1)
             for index in range(40)]
    document = {"summary": {"files_analyzed": len(files)}, "files": files}
    app = FollowApp(
        document, lambda changed: RefreshResult(document, frozenset(), full=True),
        workspace=Path.cwd(), targets=(Path("."),), include_tests=False,
        trend_window=900, watch=False,
    )
    async with app.run_test(size=(100, 14)) as pilot:
      table = app.query_one("#results")
      self.assertEqual(table.cursor_row, 0)
      await pilot.press("ctrl+f")
      await pilot.pause()
      self.assertGreater(table.cursor_row, 1)
      await pilot.press("ctrl+b")
      await pilot.pause()
      self.assertEqual(table.cursor_row, 0)
      app.exit(0)

  async def test_dashboard_uses_full_height_without_movement_or_heat_columns(self) -> None:
    document = {
        "summary": {"files_analyzed": 2},
        "files": [_file("a.go", 100, 1), _file("b.go", 80, 2)],
    }
    app = FollowApp(
        document, lambda changed: RefreshResult(document, frozenset(), full=True),
        workspace=Path.cwd(), targets=(Path("slopslap.py"),),
        include_tests=False, trend_window=900, limit=1, watch=False,
    )
    async with app.run_test(size=(120, 36)) as pilot:
      await pilot.pause()
      table = app.query_one("#results")
      self.assertEqual(table.row_count, 1)
      self.assertGreaterEqual(table.size.height, 33)
      self.assertEqual(len(app.query("#follow-top")), 1)
      self.assertEqual(len(app.query("#follow-status")), 0)
      self.assertEqual(table.styles.overflow_x, "hidden")
      self.assertEqual(table.styles.scrollbar_size_horizontal, 0)
      self.assertEqual(table.cell_padding, 0)
      footer = app.query_one("#follow-keys").render().plain
      top = app.query_one("#follow-top").render().plain
      self.assertIn(str(Path.cwd()), top)
      self.assertNotIn("LIVE", top)
      self.assertNotIn("^C", footer)
      self.assertNotIn("navigate", footer)
      self.assertNotIn("Enter", footer)
      self.assertNotIn("details", footer)
      self.assertNotIn("space", footer)
      self.assertNotIn("pause", footer)
      self.assertIn("s sort", footer)
      self.assertIn("q quit", footer)
      keys = {key.value for key in table.columns}
      headings = {column.label.plain for column in table.columns.values()}
      columns = {key.value: column for key, column in table.columns.items()}
      self.assertNotIn("rank", columns)
      self.assertEqual(columns["score"].label.plain, " ▼SCORE  ")
      self.assertEqual(columns["god"].label.plain, "   GOD  ")
      self.assertEqual(columns["path"].label.plain, "")
      self.assertNotIn("trend", keys)
      self.assertNotIn("delta", keys)
      self.assertNotIn("HEAT", " ".join(headings))
      self.assertNotIn("Δ SCORE", headings)
      self.assertNotRegex(
          app._cells(app.state.rows["a.go"], 0)["score"].plain, "[↑↓]",
      )
      rendered_cells = " ".join(
          cell.plain for cell in app._cells(app.state.rows["a.go"], 0).values()
          if hasattr(cell, "plain")
      )
      self.assertNotRegex(rendered_cells, "[◆◇●]")
      cells = app._cells(app.state.rows["a.go"], 0)
      self.assertNotIn("rank", cells)
      self.assertEqual(cells["score"].plain, "  100.0  ")
      self.assertTrue(cells["god"].plain.startswith(" "))
      for key, cell in cells.items():
        if key != "path":
          self.assertTrue(cell.plain.endswith(" "), f"{key} lost its separator")
      self.assertEqual(table.cursor_foreground_priority, "renderable")
      self.assertEqual(table.cursor_background_priority, "css")
      cursor_colour = table.get_component_rich_style(
          "datatable--cursor",
      ).bgcolor.get_truecolor()
      cursor_rgb = tuple(cursor_colour)
      self.assertGreaterEqual(
          sum(cursor_rgb) - sum(_BASE_BACKGROUND), 100,
          "selected row background must remain visibly distinct",
      )
      await pilot.press("c")
      await pilot.pause()
      self.assertEqual(type(app.screen).__name__, "ColumnsScreen")
      app.exit(0)

  async def test_sort_chooser_orders_all_rows_and_marks_the_active_heading(self) -> None:
    document = {
        "summary": {"files_analyzed": 3},
        "files": [
            _file("zeta.go", 100, 1),
            _file("alpha.go", 20, 2),
            _file("middle.go", 60, 3),
        ],
    }
    app = FollowApp(
        document, lambda changed: RefreshResult(document, frozenset(), full=True),
        workspace=Path.cwd(), targets=(Path("."),), include_tests=False,
        trend_window=900, limit=2, watch=False,
    )
    async with app.run_test(size=(100, 24)) as pilot:
      await pilot.press("s")
      await pilot.pause()
      self.assertEqual(type(app.screen).__name__, "SortScreen")
      await pilot.press("left", "enter")  # score ascending
      await pilot.pause()
      self.assertEqual(app._display_paths(), ["alpha.go", "middle.go"])
      table = app.query_one("#results")
      self.assertEqual(table.columns["score"].label.plain, " ▲SCORE  ")
      self.assertEqual(app.selected_path, "alpha.go")
      app.exit(0)

  async def test_enter_opens_details_q_closes_them_and_ctrl_c_quits(self) -> None:
    document = {
        "summary": {"files_analyzed": 1},
        "files": [_file("a.go", 100, 1)],
    }
    app = FollowApp(
        document, lambda changed: RefreshResult(document, frozenset(), full=True),
        workspace=Path.cwd(), targets=(Path("slopslap.py"),),
        include_tests=False, trend_window=900, watch=False,
    )
    async with app.run_test(size=(100, 30)) as pilot:
      await pilot.press("enter")
      await pilot.pause()
      self.assertEqual(type(app.screen).__name__, "DetailsScreen")
      await pilot.press("q")
      await pilot.pause()
      self.assertNotEqual(type(app.screen).__name__, "DetailsScreen")
      self.assertTrue(app.is_running)
      await pilot.press("ctrl+c")
      await pilot.pause()
      self.assertEqual(app.return_value, 0)

  async def test_source_change_requests_targeted_not_full_measurement(self) -> None:
    document = {
        "summary": {"files_analyzed": 1},
        "files": [_file("a.go", 100, 1)],
    }
    calls: list[set[str] | None] = []

    def refresh(changed: set[str] | None) -> RefreshResult:
      calls.append(changed)
      return RefreshResult(
          {"summary": {}, "files": [_file("a.go", 90, 1)]},
          frozenset({"a.go"}),
      )

    app = FollowApp(
        document, refresh, workspace=Path.cwd(), targets=(Path("slopslap.py"),),
        include_tests=False, trend_window=900, watch=False,
    )
    async with app.run_test(size=(100, 30)) as pilot:
      app._queue_scan({"a.go"})
      await app.workers.wait_for_complete()
      await pilot.pause()
      self.assertEqual(calls, [{"a.go"}])
      app.exit(0)


class FollowLauncherTests(unittest.TestCase):
  def test_follow_leaves_mouse_selection_to_the_terminal(self) -> None:
    document = {"summary": {}, "files": []}
    with patch.object(FollowApp, "run", return_value=0) as run:
      result = run_follow(
          document,
          lambda changed: RefreshResult(document, frozenset(), full=True),
          workspace=Path.cwd(), targets=(Path("."),), include_tests=False,
      )

    self.assertEqual(result, 0)
    run.assert_called_once_with(mouse=False)


if __name__ == "__main__":
  unittest.main()
