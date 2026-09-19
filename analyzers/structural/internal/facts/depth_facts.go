package facts

import "strconv"

// This file is the transport portion of the responsibility-burden v4
// contract.  It deliberately contains no language-specific syntax.  Adapters
// may leave Depth nil while they are being upgraded; the shared scorer then
// has an explicit unavailable assessment instead of treating missing data as
// a zero score.

// KnowledgeState is the state of one measured depth dimension or boundary.
type KnowledgeState string

const (
	KnowledgeMeasured      KnowledgeState = "measured"
	KnowledgePartial       KnowledgeState = "partial"
	KnowledgeUnavailable   KnowledgeState = "unavailable"
	KnowledgeNotApplicable KnowledgeState = "not_applicable"
)

// Knowledge records completeness and the source reason for a dimension.
type Knowledge struct {
	State     KnowledgeState `json:"state"`
	Reason    string         `json:"reason"`
	Essential bool           `json:"essential"`
}

type KnowledgeDimension string

const (
	DimensionInventory KnowledgeDimension = "inventory"
	DimensionBurden    KnowledgeDimension = "burden"
	DimensionBehavior  KnowledgeDimension = "behavior"
	DimensionAliases   KnowledgeDimension = "alias_effects"
)

type KnowledgeDimensions struct {
	Inventory    Knowledge `json:"inventory"`
	Burden       Knowledge `json:"burden"`
	Behavior     Knowledge `json:"behavior"`
	AliasEffects Knowledge `json:"alias_effects"`
}

// BoundaryIdentity is stable under source movement and helper relocation.
type BoundaryIdentity struct {
	Artifact string `json:"artifact"`
	Audience string `json:"audience"`
	View     string `json:"view"`
	Symbol   string `json:"symbol"`
}

// String is the canonical, human-readable identity used for ordering.
func (id BoundaryIdentity) String() string {
	return strconv.Itoa(len(id.Artifact)) + ":" + id.Artifact + strconv.Itoa(len(id.Audience)) + ":" + id.Audience + strconv.Itoa(len(id.View)) + ":" + id.View + strconv.Itoa(len(id.Symbol)) + ":" + id.Symbol
}

// Concept is a normalized caller-visible concept (number, text, sequence,
// callback, choice, a canonical named contract, and so on).
type Concept struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Children []string `json:"children"`
}

// Slot is a canonical downstream formal or field path. Parameter names are
// intentionally absent: equivalent record packaging must produce the same ID.
type Slot struct {
	ID       string `json:"id"`
	Concept  string `json:"concept"`
	Required bool   `json:"required"`
	Policy   bool   `json:"policy"`
	Path     string `json:"path"`
}

// Route describes one externally callable route to a service family.
type Route struct {
	ID                 string           `json:"id"`
	Family             string           `json:"family"`
	Signature          string           `json:"signature"`
	TargetFunctionID   string           `json:"target_function_id"`
	Boundary           BoundaryIdentity `json:"boundary"`
	RequiredSlots      []string         `json:"required_slots"`
	ExposedSlots       []string         `json:"exposed_slots"`
	RequiredPolicies   []string         `json:"required_policies"`
	LifecycleRelations []string         `json:"lifecycle_relations"`
	// Sequencing is retained by the contract for adapters which need to expose
	// caller lifecycle order; it is evidence, not a second burden category.
	Sequencing []string `json:"sequencing"`
}

// RouteFamily groups aliases/overloads that resolve to one service.
type RouteFamily struct {
	ID     string  `json:"id"`
	Routes []Route `json:"routes"`
}

// ObligationCategory is the only set of categories which can affect H. U is
// retained as relevance evidence and has zero weight.
type ObligationCategory string

const (
	ObligationValidation   ObligationCategory = "V"
	ObligationResource     ObligationCategory = "R"
	ObligationState        ObligationCategory = "C"
	ObligationCoordination ObligationCategory = "Y"
	ObligationTransform    ObligationCategory = "X"
	ObligationOutcome      ObligationCategory = "U"
)

// Obligation is a canonical hidden responsibility. ID must not contain a
// route ID or source location; adapters should encode the governed root or
// outcome and normalized rule in that identity.
type Obligation struct {
	ID         string             `json:"id"`
	Category   ObligationCategory `json:"category"`
	Governed   string             `json:"governed"`
	Rule       string             `json:"rule"`
	Evidence   []string           `json:"evidence"`
	Provenance []Provenance       `json:"provenance"`
}

// ObligationSet is one normal service behavior alternative. IDs are sorted and
// deduplicated by the scorer before they are composed.
type ObligationSet []string

// Evidence is semantic support retained separately from identity.
type Evidence struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	// Description is retained for decoding legacy evidence. New evidence uses
	// Details so renderers can choose its wording without receiving prose from
	// the analyzer.
	Description string         `json:"description,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
	Provenance  []Provenance   `json:"provenance"`
}

// Reason is a stable machine-readable partial/unavailable explanation.
type Reason struct {
	Code      string   `json:"code"`
	Dimension string   `json:"dimension"`
	Message   string   `json:"message"`
	FactIDs   []string `json:"fact_ids"`
}

// Burden is represented as signed integers so malformed negative JSON values
// can be rejected explicitly. Valid values are non-negative.
type Burden struct {
	O int64 `json:"O"`
	T int64 `json:"T"`
	A int64 `json:"A"`
	E int64 `json:"E"`
	P int64 `json:"P"`
	S int64 `json:"S"`
	L int64 `json:"L"`
}

// BoundaryAssessment is the scorer input and serialized output-independent
// evidence unit. FamilyAlternatives retain route/dispatch alternatives per
// family; they are composed only after canonical deduplication.
type BoundaryAssessment struct {
	SupportingContract *SupportingContract `json:"supporting_contract,omitempty"`
	// BoundedAssessment is an adapter-provided v4 assessment for the same
	// boundary when precise analysis is incomplete. It is scored only after
	// the precise assessment has been validated and may not be nested.
	BoundedAssessment    *BoundaryAssessment  `json:"bounded_assessment,omitempty"`
	Identity             BoundaryIdentity     `json:"identity"`
	State                KnowledgeState       `json:"state"`
	Knowledge            map[string]Knowledge `json:"knowledge"`
	Burden               Burden               `json:"burden"`
	Concepts             []Concept            `json:"concepts"`
	Slots                []Slot               `json:"slots"`
	RouteFamilies        []RouteFamily        `json:"route_families"`
	FamilyAlternatives   [][]ObligationSet    `json:"family_alternatives"`
	FamilyAlternativeIDs []string             `json:"family_alternative_ids"`
	Obligations          []Obligation         `json:"obligations"`
	Evidence             []Evidence           `json:"evidence"`
	Reasons              []Reason             `json:"reasons"`
	SelectedRoutes       []string             `json:"selected_routes"`
	Creation             *CreationFacts       `json:"creation"`
	SourceLocations      []Location           `json:"source_locations"`
	Files                []string             `json:"files"`
	LeakRoots            []string             `json:"leak_roots"`
	Dependencies         []string             `json:"dependencies,omitempty"`
}

// DepthFacts is the optional Program-level v4 payload.
type DepthFacts struct {
	Boundaries []BoundaryAssessment `json:"boundaries"`
	Flows      []FlowArtifact       `json:"flows"`
	Creations  []CreationFacts      `json:"creations"`
	Reasons    []Reason             `json:"reasons"`
}

// DepthProgram is retained as a descriptive alias for clients that call the
// payload a normalized program rather than depth facts.
type DepthProgram = DepthFacts

// Descriptive aliases keep the contract readable to adapters without
// multiplying wire representations.
type BoundaryFacts = BoundaryAssessment
type ServiceFamily = RouteFamily
type Alternative = ObligationSet
type ValueRoot = AliasRoot
type TypedEdge = FlowEdge
type FlowInstruction = Instruction

// CreationFacts normalizes constructors, literals and factories to one
// create:<canonical type> family while retaining bindings and behavior.
type CreationFacts struct {
	ID               string         `json:"id"`
	CanonicalType    string         `json:"canonical_type"`
	Family           string         `json:"family"`
	Route            string         `json:"route"`
	InputBindings    []Binding      `json:"input_bindings"`
	InitialFields    []FieldBinding `json:"initial_fields"`
	PassiveAccessors []string       `json:"passive_accessors,omitempty"`
	PossibleFailures []string       `json:"possible_failures"`
	Behavior         []string       `json:"behavior"`
	DataOnly         bool           `json:"data_only"`
	Accessible       bool           `json:"accessible"`
	Knowledge        KnowledgeState `json:"knowledge"`
}

// Provenance identifies source support without being part of semantic IDs.
type Provenance struct {
	Artifact             string   `json:"artifact"`
	Path                 string   `json:"path"`
	Span                 Location `json:"span"`
	RuleID               string   `json:"rule_id"`
	FactIDs              []string `json:"fact_ids"`
	OmittedLocationCount int      `json:"omitted_location_count"`
}

// FlowArtifact is a language-neutral flow unit consumed by the later shared
// transfer engine.
type FlowArtifact struct {
	Artifact       string         `json:"artifact"`
	Language       string         `json:"language"`
	BuildSelection string         `json:"build_selection"`
	Functions      []FlowFunction `json:"functions"`
	Types          []FlowType     `json:"types"`
	PublicRoutes   []Route        `json:"public_routes"`
	Provenance     []Provenance   `json:"provenance"`
}

type FlowType struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Fields  []Field  `json:"fields"`
	Methods []string `json:"methods"`
}

type FlowFunction struct {
	Results        []Formal     `json:"results,omitempty"`
	ID             string       `json:"id"`
	Formals        []Formal     `json:"formals"`
	Receiver       string       `json:"receiver"`
	ReceiverFormal *Formal      `json:"receiver_formal,omitempty"`
	Entry          string       `json:"entry"`
	Blocks         []FlowBlock  `json:"blocks"`
	Exits          []FlowExit   `json:"exits"`
	Recurrences    []Recurrence `json:"recurrences"`
	Provenance     []Provenance `json:"provenance"`
	ReturnType     string       `json:"return_type"`
	ReturnConcepts []string     `json:"return_concepts"`
}

type Formal struct {
	ID        string        `json:"id"`
	Path      string        `json:"path"`
	Type      string        `json:"type"`
	Concept   string        `json:"concept"`
	ValueKind FlowValueKind `json:"value_kind,omitempty"`
}

// FlowValueKind is resolved adapter metadata. The shared engine never infers
// scalar/reference semantics from type-name spelling.
type FlowValueKind string

const (
	FlowKindUnknown   FlowValueKind = "unknown"
	FlowKindError     FlowValueKind = "error_result"
	FlowKindNumeric   FlowValueKind = "numeric"
	FlowKindBoolean   FlowValueKind = "boolean"
	FlowKindString    FlowValueKind = "string"
	FlowKindBytes     FlowValueKind = "bytes"
	FlowKindReference FlowValueKind = "reference"
	FlowKindRecord    FlowValueKind = "record"
)

type FlowBlock struct {
	ID           string        `json:"id"`
	Instructions []Instruction `json:"instructions"`
	Edges        []FlowEdge    `json:"edges"`
}

type EdgeKind string

const (
	EdgeNormal        EdgeKind = "normal"
	EdgeTrue          EdgeKind = "true"
	EdgeFalse         EdgeKind = "false"
	EdgeThrow         EdgeKind = "throw"
	EdgeReturnError   EdgeKind = "return_error"
	EdgePanic         EdgeKind = "panic"
	EdgeCleanup       EdgeKind = "cleanup"
	EdgeSuspendResume EdgeKind = "suspend_resume"
)

type FlowEdge struct {
	From          string   `json:"from"`
	To            string   `json:"to"`
	Kind          EdgeKind `json:"kind"`
	ErrorTag      string   `json:"error_tag"`
	Guard         string   `json:"guard"`
	GuardPolarity string   `json:"guard_polarity,omitempty"`
	Payload       string   `json:"payload"`
}

type FlowExit struct {
	Kind     EdgeKind `json:"kind"`
	Payload  string   `json:"payload"`
	ErrorTag string   `json:"error_tag"`
}

type Opcode string

const (
	OpConstant       Opcode = "constant"
	OpBind           Opcode = "bind"
	OpPhi            Opcode = "phi"
	OpErrorPresent   Opcode = "error_present"
	OpPrimitive      Opcode = "primitive"
	OpFieldRead      Opcode = "field_read"
	OpFieldWrite     Opcode = "field_write"
	OpAllocate       Opcode = "allocate"
	OpPack           Opcode = "pack"
	OpCall           Opcode = "call"
	OpSelectTarget   Opcode = "select_target"
	OpBranch         Opcode = "branch"
	OpReturn         Opcode = "return"
	OpThrow          Opcode = "throw"
	OpAcquire        Opcode = "acquire"
	OpUseResource    Opcode = "use_resource"
	OpCleanupAttempt Opcode = "cleanup_attempt"
	OpLock           Opcode = "lock"
	OpUnlock         Opcode = "unlock"
	OpAtomicRMW      Opcode = "atomic_rmw"
	OpEscape         Opcode = "escape"
	OpUnknown        Opcode = "unknown"
)

type Instruction struct {
	// CompletionKind is return_error only on a proven failure OpReturn. Typed
	// error-result tuples are otherwise partitioned by their presence relation;
	// error values are not automatically exceptions in callers.
	CompletionKind EdgeKind       `json:"completion_kind,omitempty"`
	ID             string         `json:"id"`
	Opcode         Opcode         `json:"opcode"`
	Operands       []string       `json:"operands"`
	Results        []string       `json:"results"`
	Type           string         `json:"type"`
	Roots          []AliasRoot    `json:"roots"`
	Effects        []string       `json:"effects"`
	Provenance     []Provenance   `json:"provenance"`
	Call           *CallBinding   `json:"call"`
	Value          *Value         `json:"value"`
	Operator       string         `json:"operator"`
	ArithmeticMode string         `json:"arithmetic_mode"`
	Bindings       []Binding      `json:"bindings"`
	FieldBindings  []FieldBinding `json:"field_bindings"`
	ValueKind      FlowValueKind  `json:"value_kind,omitempty"`
	PhiInputs      []PhiInput     `json:"phi_inputs,omitempty"`
	FieldID        string         `json:"field_id,omitempty"`
	// AccessPath carries resolved index/member steps which are not representable
	// by FieldID.  Dynamic indexes retain Dynamic=true; a literal "*" remains
	// an ordinary named step.
	AccessPath []AliasPathSegment `json:"access_path,omitempty"`
}

// PhiInput preserves edge-specific SSA correlation at a join.
type PhiInput struct {
	Predecessor string `json:"predecessor"`
	Value       string `json:"value"`
}

type AliasRoot struct {
	ID           string             `json:"id"`
	Kind         string             `json:"kind"`
	Path         string             `json:"path"`
	Mutable      bool               `json:"mutable"`
	Ownership    string             `json:"ownership"`
	PathSegments []AliasPathSegment `json:"path_segments,omitempty"`
}

type AliasPathSegment struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Dynamic bool   `json:"dynamic,omitempty"`
}

type Value struct {
	ID        string        `json:"id"`
	Origin    string        `json:"origin"`
	Type      string        `json:"type"`
	Concepts  []string      `json:"concepts"`
	MayRoots  []string      `json:"may_roots"`
	Constant  string        `json:"constant"`
	ValueKind FlowValueKind `json:"value_kind,omitempty"`
}

type Binding struct {
	Formal string `json:"formal"`
	Actual string `json:"actual"`
	Value  string `json:"value"`
}

type CallBinding struct {
	Targets            []string  `json:"targets"`
	Bindings           []Binding `json:"bindings"`
	ResultBindings     []Binding `json:"result_bindings"`
	ErrorContinuations []string  `json:"error_continuations"`
	OwnershipEffects   []string  `json:"ownership_effects"`
}

type FieldBinding struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

// Recurrence identifies a loop-header phi result. PhiSlot is an SSA result ID,
// not an instruction ID. The phi's predecessor operands are authoritative for
// initial and update values. Definition, when supplied, asserts one common update
// SSA value on every backedge; leave it empty when distinct latches supply distinct
// updates. References lists recurrence IDs for linkage, not source expressions or
// a complete declaration of data dependencies (which the engine derives).
type Recurrence struct {
	ID         string   `json:"id"`
	LoopHeader string   `json:"loop_header"`
	PhiSlot    string   `json:"phi_slot"`
	Definition string   `json:"definition"`
	References []string `json:"references"`
}
