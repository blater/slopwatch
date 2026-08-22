// Package facts defines the language-neutral input consumed by metric strategies.
package facts

// SchemaVersion identifies the normalized adapter-to-strategy fact contract.
const SchemaVersion = 1

// Location identifies a stable source subject using one-based coordinates.
type Location struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"end_line"`
	EndColumn int    `json:"end_column"`
}

// ExprKind identifies only expression structure that affects PMD metrics.
type ExprKind uint8

const (
	ExprOther ExprKind = iota
	ExprAnd
	ExprOr
	ExprNot
	ExprConditional
)

// Expression is a normalized Boolean/call expression.
type Expression struct {
	Kind     ExprKind      `json:"kind"`
	Children []*Expression `json:"children"`
	Calls    []string      `json:"calls"`
	Nested   []*Function   `json:"nested"`
}

// StmtKind identifies normalized structured control flow.
type StmtKind uint8

const (
	StmtLinear StmtKind = iota
	StmtBlock
	StmtIf
	StmtLoop
	StmtSwitch
	StmtReturn
	StmtPanic
	StmtBreak
	StmtContinue
	StmtGoto
)

// Case is one switch, type-switch, or select alternative.
type Case struct {
	Default      bool          `json:"default"`
	FallsThrough bool          `json:"falls_through"`
	Expressions  []*Expression `json:"expressions"`
	Body         []*Statement  `json:"body"`
}

// Statement is a normalized control-flow statement.
type Statement struct {
	Kind        StmtKind      `json:"kind"`
	Location    Location      `json:"location"`
	Condition   *Expression   `json:"condition,omitempty"`
	Expressions []*Expression `json:"expressions"`
	Body        []*Statement  `json:"body"`
	Else        []*Statement  `json:"else"`
	Cases       []Case        `json:"cases"`
	MaySkip     bool          `json:"may_skip"`
	Labeled     bool          `json:"labeled"`
}

// Function is one independently measured executable body.
type Function struct {
	Name        string       `json:"name"`
	Receiver    string       `json:"receiver"`
	ReceiverVar string       `json:"receiver_var"`
	Location    Location     `json:"location"`
	Body        []*Statement `json:"body"`
}

// TypeShape is the caller-visible shape of a signature value. It intentionally
// excludes private representation unless the type is exposed in the signature.
type TypeShape struct {
	StableID       string       `json:"stable_id"`
	Kind           string       `json:"kind"`
	Name           string       `json:"name,omitempty"`
	Children       []*TypeShape `json:"children,omitempty"`
	ExposedMembers []string     `json:"exposed_members,omitempty"`
	Complexity     int          `json:"complexity"`
}

// PublicOperation is the normalized signature evidence for one public
// function or method. It is optional so older adapters can retain fallback
// structural measurements while they are upgraded.
type PublicOperation struct {
	StableID           string       `json:"stable_id"`
	Name               string       `json:"name"`
	OwnerType          string       `json:"owner_type,omitempty"`
	Location           Location     `json:"location"`
	Parameters         []*TypeShape `json:"parameters"`
	Results            []*TypeShape `json:"results"`
	EmitsOutput        bool         `json:"emits_output"`
	ObservableMutation bool         `json:"observable_mutation"`
}

// Field is a declared field/property with visibility relevant to interface
// cost and representation leakage.
type Field struct {
	Name    string     `json:"name"`
	Public  bool       `json:"public"`
	Mutable bool       `json:"mutable"`
	Type    *TypeShape `json:"type,omitempty"`
}

// RepresentationExposure records a caller-visible entity that exposes module
// representation rather than merely increasing interface learning cost.
type RepresentationExposure struct {
	StableID   string   `json:"stable_id"`
	Kind       string   `json:"kind"`
	Entity     string   `json:"entity"`
	Location   Location `json:"location"`
	Evidence   string   `json:"evidence"`
	Confidence string   `json:"confidence"`
}

// Type is the normalized design surface for a named type.
type Type struct {
	Name                 string              `json:"name"`
	Kind                 string              `json:"kind"`
	Location             Location            `json:"location"`
	Methods              []*Function         `json:"methods"`
	InterfaceMethodCount int                 `json:"interface_method_count"`
	ForeignTypes         []string            `json:"foreign_types"`
	MethodFields         map[string][]string `json:"method_fields"`
	ForeignFields        []string            `json:"foreign_fields"`
	Fields               []Field             `json:"fields,omitempty"`
}

// Program is the complete fact set for one analyzer unit.
type Program struct {
	Functions        []*Function                  `json:"functions"`
	Types            []*Type                      `json:"types"`
	PublicOperations []*PublicOperation           `json:"public_operations,omitempty"`
	Representation   []*RepresentationExposure    `json:"representation_exposure,omitempty"`
	Files            []string                     `json:"files"`
	Unavailable      map[string]map[string]string `json:"unavailable"`
}

// Availability returns whether a component has complete evidence for a file.
func (program *Program) Availability(path, component string) (bool, string) {
	components := program.Unavailable[path]
	if components == nil {
		return true, ""
	}
	reason, unavailable := components[component]
	return !unavailable, reason
}
