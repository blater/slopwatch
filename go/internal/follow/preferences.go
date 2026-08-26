package follow

import (
	"fmt"
	"math"
	"strings"
	"time"

	userprefs "github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/style"
)

const defaultTrendWindow = 10 * time.Minute
const defaultMaximumWeight = 20.0
const defaultWeightStep = 0.5

func defaultUserPreferences() userprefs.Document {
	value := userprefs.DefaultDocument()
	components := make(map[string]userprefs.ComponentPreference, len(componentWeights))
	enabled := defaultWeightEnabled()
	for _, item := range componentWeights {
		components[item.id] = userprefs.ComponentPreference{Enabled: enabled[item.id], Weight: item.value}
	}
	value.Version = userprefs.CurrentVersion
	value.Appearance = userprefs.Appearance{Theme: string(style.ThemeDark)}
	value.Table = userprefs.Table{
		VisibleColumns: visibleColumnKeys(defaultColumnVisibility()),
		SortBy:         "score", SortDescending: true,
	}
	value.Interaction = userprefs.Interaction{TrendWindow: defaultTrendWindow.String()}
	value.Scoring = userprefs.Scoring{
		WeightStep: defaultWeightStep, MaximumWeight: defaultMaximumWeight,
		Components: components,
	}
	return value
}

func loadUserPreferences(path string) (userprefs.Document, time.Duration, error) {
	value := defaultUserPreferences()
	var err error
	if path != "" {
		value, err = userprefs.LoadOrCreate(path, value)
		if err != nil {
			return userprefs.Document{}, 0, err
		}
	}
	trendWindow, err := validateUserPreferences(value)
	if err != nil {
		return userprefs.Document{}, 0, fmt.Errorf("validate preferences %s: %w", path, err)
	}
	return value, trendWindow, nil
}

func validateUserPreferences(value userprefs.Document) (time.Duration, error) {
	if value.Appearance.Theme != string(style.ThemeDark) && value.Appearance.Theme != string(style.ThemeLight) {
		return 0, fmt.Errorf("appearance.theme must be dark or light")
	}
	if err := validatePreferenceColumns(value.Table.VisibleColumns); err != nil {
		return 0, err
	}
	if !knownSortField(value.Table.SortBy) {
		return 0, fmt.Errorf("table.sort_by %q is not a known field", value.Table.SortBy)
	}
	trendWindow, err := time.ParseDuration(value.Interaction.TrendWindow)
	if err != nil || trendWindow <= 0 {
		return 0, fmt.Errorf("interaction.trend_window must be a positive duration")
	}
	if !positiveFinite(value.Scoring.MaximumWeight) {
		return 0, fmt.Errorf("scoring.maximum_weight must be positive and finite")
	}
	if !positiveFinite(value.Scoring.WeightStep) || value.Scoring.WeightStep > value.Scoring.MaximumWeight {
		return 0, fmt.Errorf("scoring.weight_step must be positive, finite, and no greater than maximum_weight")
	}
	if err := validateComponentPreferences(value.Scoring); err != nil {
		return 0, err
	}
	return trendWindow, nil
}

func validatePreferenceColumns(columns []string) error {
	known := map[string]bool{}
	for _, column := range columnNames() {
		known[column.key] = true
	}
	seen := map[string]bool{}
	for _, column := range columns {
		if !known[column] {
			return fmt.Errorf("table.visible_columns contains unknown column %q", column)
		}
		if seen[column] {
			return fmt.Errorf("table.visible_columns contains duplicate column %q", column)
		}
		seen[column] = true
	}
	return nil
}

func knownSortField(key string) bool {
	for _, field := range sortFields() {
		if field.key == key {
			return true
		}
	}
	return false
}

func validateComponentPreferences(scoring userprefs.Scoring) error {
	known := map[string]bool{}
	for _, item := range componentWeights {
		known[item.id] = true
		component, ok := scoring.Components[item.id]
		if !ok {
			return fmt.Errorf("scoring.components.%s is missing", item.id)
		}
		if component.Weight < 0 || component.Weight > scoring.MaximumWeight || math.IsNaN(component.Weight) || math.IsInf(component.Weight, 0) {
			return fmt.Errorf("scoring.components.%s.weight must be finite and between 0 and maximum_weight", item.id)
		}
	}
	for id := range scoring.Components {
		if !known[id] {
			return fmt.Errorf("scoring.components contains unknown component %q", id)
		}
	}
	return nil
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func preferenceWeights(value userprefs.Document) map[string]float64 {
	result := make(map[string]float64, len(value.Scoring.Components))
	for id, component := range value.Scoring.Components {
		result[id] = component.Weight
	}
	return result
}

func preferenceWeightEnabled(value userprefs.Document) map[string]bool {
	result := make(map[string]bool, len(value.Scoring.Components))
	for id, component := range value.Scoring.Components {
		result[id] = component.Enabled
	}
	return result
}

func preferenceColumns(value userprefs.Document) map[string]bool {
	result := make(map[string]bool, len(value.Table.VisibleColumns))
	for _, key := range value.Table.VisibleColumns {
		result[key] = true
	}
	return result
}

func visibleColumnKeys(visible map[string]bool) []string {
	result := make([]string, 0, len(visible))
	for _, column := range columnNames() {
		if visible[column.key] {
			result = append(result, column.key)
		}
	}
	return result
}

func persistUserPreferences(model *Model) {
	if model.preferencesPath == "" {
		return
	}
	value := model.preferences
	// Feature settings save asynchronously through appconfig. Refresh the
	// shared document before a dashboard edit so Appearance,
	// Columns, or Weights cannot overwrite newer agent/fix properties.
	latest, _, err := loadUserPreferences(model.preferencesPath)
	if err != nil {
		model.status = err.Error()
		return
	}
	value = latest
	value.Appearance.Theme = string(model.theme)
	value.Table.VisibleColumns = visibleColumnKeys(model.visible)
	value.Table.SortBy = model.sortKey
	value.Table.SortDescending = model.sortReverse
	value.Scoring.WeightStep = model.weightStepValue()
	value.Scoring.MaximumWeight = model.maximumWeightValue()
	for _, item := range componentWeights {
		value.Scoring.Components[item.id] = userprefs.ComponentPreference{
			Enabled: model.isWeightEnabled(item.id), Weight: model.weights[item.id],
		}
	}
	model.preferences = value
	if err := userprefs.Save(model.preferencesPath, value); err != nil {
		model.status = err.Error()
	} else if strings.HasPrefix(model.status, "write preferences ") {
		model.status = ""
	}
}

func (model Model) weightStepValue() float64 {
	if model.weightStep > 0 {
		return model.weightStep
	}
	return defaultWeightStep
}

func (model Model) maximumWeightValue() float64 {
	if model.maximumWeight > 0 {
		return model.maximumWeight
	}
	return defaultMaximumWeight
}
