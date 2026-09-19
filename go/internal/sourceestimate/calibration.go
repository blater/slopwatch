package sourceestimate

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
)

// CalibrationProfile contains grading units only. It never changes descriptive
// evidence or fix-safety gates. Pass by value; no request mutates shared defaults.
type CalibrationProfile struct {
	Name                         string  `json:"name"`
	Operation                    float64 `json:"operation"`
	Accessor                     float64 `json:"accessor"`
	Input                        float64 `json:"input"`
	MutableAlias                 float64 `json:"mutable_alias"`
	CoupledField                 float64 `json:"coupled_field"`
	Sequencing                   float64 `json:"sequencing"`
	ResidualReference            float64 `json:"residual_reference"`
	DenominatorReference         float64 `json:"denominator_reference"`
	ResponsibilityMultiplier     float64 `json:"responsibility_multiplier"`
	Validation                   float64 `json:"validation"`
	Transform                    float64 `json:"transform"`
	State                        float64 `json:"state"`
	Resource                     float64 `json:"resource"`
	Coordination                 float64 `json:"coordination"`
	InvariantTransformation      float64 `json:"invariant_transformation"`
	StateConsistency             float64 `json:"state_consistency"`
	RepresentationTransformation float64 `json:"representation_transformation"`
	Cleanup                      float64 `json:"cleanup"`
	ExposedValidationCap         float64 `json:"exposed_validation_cap"`
	ValidationEnvelope           float64 `json:"validation_envelope"`
	TransformationEnvelope       float64 `json:"transformation_envelope"`
}

//go:embed calibration_default.json
var calibrationDefaultJSON string

// Defaults are parsed once; callers receive value copies.
var defaultCalibration = loadDefaultCalibration()
var defaultCalibrationIdentity = defaultCalibration.Identity()

func DefaultCalibration() CalibrationProfile { return defaultCalibration }

func loadDefaultCalibration() CalibrationProfile {
	var p CalibrationProfile
	decoder := json.NewDecoder(strings.NewReader(calibrationDefaultJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		panic(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		panic("calibration profile must contain exactly one JSON object")
	}
	if err := p.Validate(); err != nil {
		panic(err)
	}
	return p
}

func (p CalibrationProfile) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("calibration name is required")
	}
	value := reflect.ValueOf(p)
	for i := 1; i < value.NumField(); i++ {
		v := value.Field(i).Float()
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > 1e6 {
			return fmt.Errorf("calibration %s must be finite in (0, 1000000]", value.Type().Field(i).Name)
		}
	}
	return nil
}

// Identity hashes all named parameters, independent of JSON whitespace.
func (p CalibrationProfile) Identity() string {
	data, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(bytes.TrimSpace(data))
	return hex.EncodeToString(hash[:])
}

func DefaultCalibrationIdentity() string { return defaultCalibrationIdentity }

// AnalyzeWithCalibration evaluates the actual per-abstraction grading pipeline,
// including category-envelope replacement and file maximum selection.
func AnalyzeWithCalibration(files []File, p CalibrationProfile) (map[string]Result, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	_, results := analyzeWithAttribution(files, false, p)
	return results, nil
}

func (u unit) gradingProfile() CalibrationProfile {
	if u.calibration.Name != "" {
		return u.calibration
	}
	return DefaultCalibration()
}

func (p CalibrationProfile) categoryWeight(category string) float64 {
	switch category {
	case "validation":
		return p.Validation
	case "transform":
		return p.Transform
	case "state":
		return p.State
	case "resource":
		return p.Resource
	case "coordination":
		return p.Coordination
	default:
		panic("unrecognized graded responsibility: " + category)
	}
}
