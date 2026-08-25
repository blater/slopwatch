# Preferences

The live dashboard stores user preferences in a versioned TOML file. TOML was
chosen because the hierarchy remains readable and editable without YAML's type
ambiguities or JSON's lack of comments.

The platform-default location is:

- macOS: `~/Library/Application Support/slopwatch/preferences.toml`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/slopwatch/preferences.toml`
- Windows: `%AppData%\slopwatch\preferences.toml`

The file is created with complete defaults on the first dashboard launch.
Changes made through Settings, Columns, Weights, or Sort are written
immediately using an atomic file replacement. Hand edits are read on the next
launch. An explicitly supplied command-line option takes precedence for that
run; currently this applies to `--trend-window`.

```toml
version = 1

[appearance]
theme = 'dark'

[table]
visible_columns = ['cog', 'npath', 'cyclo', 'deep', 'god', 'coupling']
sort_by = 'score'
sort_descending = true

[interaction]
trend_window = '10m0s'

[scoring]
weight_step = 0.5
maximum_weight = 20.0

[scoring.components.ambiguous_boolean_expression]
enabled = false
weight = 4.0

[scoring.components.cognitive_complexity]
enabled = true
weight = 10.0

[scoring.components.coupling_between_objects]
enabled = true
weight = 10.0

[scoring.components.cyclomatic_class_complexity]
enabled = true
weight = 5.0

[scoring.components.cyclomatic_method_complexity]
enabled = true
weight = 5.0

[scoring.components.deeply_nested_if]
enabled = false
weight = 6.0

[scoring.components.explicit_any]
enabled = false
weight = 3.0

[scoring.components.god_class]
enabled = true
weight = 1.0

[scoring.components.module_shallowness]
enabled = true
weight = 5.0

[scoring.components.non_exhaustive_union]
enabled = false
weight = 8.0

[scoring.components.npath_complexity]
enabled = true
weight = 8.0

[scoring.components.unsafe_type_assertion]
enabled = false
weight = 5.0

[scoring.components.unsafe_type_boundary]
enabled = false
weight = 10.0

[scoring.components.unsafe_type_propagation]
enabled = false
weight = 4.0

[scoring.components.unsafe_type_use]
enabled = false
weight = 4.0
```

`visible_columns` accepts `cog`, `npath`, `cyclo`, `deep`, `god`, `coupling`,
`nesting`, and `typesafety`. `sort_by` accepts those names plus `score` and
`filename`. Themes are `dark` and `light`; durations use Go duration syntax.

The file exposes presentation preferences and the dashboard's score mixture.
Analyzer thresholds, formulas, and caps remain versioned in
`component-catalog.json`: changing them alters the meaning of analyzer output
and therefore requires cache-key and evidence-contract handling rather than a
presentation preference.
