package depth

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

type RecipeID string

// UnknownRecipeID is the canonical unresolved result used when a join or
// widening cannot preserve one exact outcome identity.
const UnknownRecipeID RecipeID = "<unknown-recipe>"

const (
	RecipeFormal      = "formal"
	RecipeConstant    = "constant"
	RecipePrimitive   = "primitive"
	RecipeFieldRead   = "field_read"
	RecipePack        = "pack"
	RecipeSelect      = "select"
	RecipeRecurrence  = "recurrence_ref"
	RecipeLoop        = "numeric_loop"
	RecipeLoopBinding = "loop_binding"
	RecipeStorage     = "storage"
	RecipeUnknown     = "unknown"
)

type RecipeKind string

const (
	KindFormal      RecipeKind = RecipeFormal
	KindConstant    RecipeKind = RecipeConstant
	KindPrimitive   RecipeKind = RecipePrimitive
	KindFieldRead   RecipeKind = RecipeFieldRead
	KindPack        RecipeKind = RecipePack
	KindSelect      RecipeKind = RecipeSelect
	KindRecurrence  RecipeKind = RecipeRecurrence
	KindLoop        RecipeKind = RecipeLoop
	KindLoopBinding RecipeKind = RecipeLoopBinding
	KindStorage     RecipeKind = RecipeStorage
	KindUnknown     RecipeKind = RecipeUnknown
)

type ArithmeticMode string

const (
	ModeInteger  ArithmeticMode = "integer"
	ModeWrapping ArithmeticMode = "wrapping"
	ModeBoolean  ArithmeticMode = "boolean"
	ModeFloat    ArithmeticMode = "float"
	ModeJSNumber ArithmeticMode = "js_number"
	ModeText     ArithmeticMode = "text"
)

type TransformationClass string

const (
	TransformationIdentity    TransformationClass = "identity"
	TransformationConstant    TransformationClass = "constant"
	TransformationPrimitive   TransformationClass = "transformation"
	TransformationUnknown     TransformationClass = "unknown"
	TransformationNonIdentity TransformationClass = TransformationPrimitive
	ClassIdentity             TransformationClass = TransformationIdentity
	ClassConstant             TransformationClass = TransformationConstant
	ClassTransformation       TransformationClass = TransformationPrimitive
	ClassUnknown              TransformationClass = TransformationUnknown
)

type RecipeError struct{ Code, Message string }

func (e *RecipeError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func reasonCode(err error) string {
	if e, ok := err.(*RecipeError); ok {
		return e.Code
	}
	return ReasonUnknownOperand
}

const (
	ReasonInvalidChild      = "invalid_child"
	ReasonInvalidArity      = "invalid_arity"
	ReasonUnknownOperand    = "unknown_operand"
	ReasonUnknownOperator   = "unknown_operator"
	ReasonUnknownArithmetic = "unknown_arithmetic_mode"
	ReasonMissingFormal     = "missing_formal_binding"
	ReasonRecipeLimit       = "recipe_limit"
	ReasonUnknownRecipe     = "unknown_recipe"
	ReasonInvalidFormal     = "invalid_formal"
	ReasonInvalidRecurrence = "invalid_recurrence"
	ReasonDuplicateField    = "duplicate_field"
	ReasonInvalidType       = "invalid_operand_type"
)

type FieldRecipeBinding struct {
	Field  string
	Recipe RecipeID
}

type RecipeNode struct {
	ID             RecipeID
	Kind           RecipeKind
	Type           string
	Operator       string
	ArithmeticMode ArithmeticMode
	FormalIndex    int
	Literal        string
	Field          string
	Predicate      RecipeID
	TrueValue      RecipeID
	FalseValue     RecipeID
	RecurrenceID   string
	// StorageID is the canonical producer/storage identity for an allocation
	// or a prior value read from abstract memory.
	StorageID string
	Children  []RecipeID
	Fields    []FieldRecipeBinding
}

type recipeNode struct {
	RecipeNode
}
type RecipeArenaOptions struct{ MaxNodes int }
type recipeStore struct {
	maxNodes int
	nodes    map[RecipeID]recipeNode
	unknown  map[string]RecipeID
}
type recipeMetadata struct {
	*recipeStore
}

type recipeBuilder struct{ *recipeMetadata }
type recipeAnalyzer struct{ *recipeMetadata }
type recipeSubstituter struct {
	*recipeMetadata
	builder  *recipeBuilder
	analyzer *recipeAnalyzer
}

type recipeRuntime struct {
	metadata *recipeMetadata
	builder  *recipeBuilder
	analyzer *recipeAnalyzer
	sub      *recipeSubstituter
}

// RecipeArena exposes the domain operations through focused collaborators.
// Each collaborator shares the same immutable intern store.
type RecipeArena struct{ runtime *recipeRuntime }

func NewRecipeArena(opts ...RecipeArenaOptions) *RecipeArena {
	limit := 256
	if len(opts) != 0 && opts[0].MaxNodes > 0 {
		limit = opts[0].MaxNodes
	}
	store := &recipeStore{maxNodes: limit, nodes: make(map[RecipeID]recipeNode), unknown: make(map[string]RecipeID)}
	metadata := &recipeMetadata{recipeStore: store}
	builder := &recipeBuilder{recipeMetadata: metadata}
	analyzer := &recipeAnalyzer{recipeMetadata: metadata}
	sub := &recipeSubstituter{recipeMetadata: metadata, builder: builder, analyzer: analyzer}
	return &RecipeArena{runtime: &recipeRuntime{metadata: metadata, builder: builder, analyzer: analyzer, sub: sub}}
}

func NewArena(opts ...RecipeArenaOptions) *RecipeArena { return NewRecipeArena(opts...) }

func (a *RecipeArena) Formal(index int, typ string) (RecipeID, error) {
	return a.runtime.builder.Formal(index, typ)
}
func (a *RecipeArena) Constant(typ, literal string) RecipeID {
	return a.runtime.builder.Constant(typ, literal)
}
func (a *RecipeArena) FieldRead(typ, field string, receiver RecipeID) (RecipeID, error) {
	return a.runtime.builder.FieldRead(typ, field, receiver)
}
func (a *RecipeArena) Pack(typ string, fields []FieldRecipeBinding) (RecipeID, error) {
	return a.runtime.builder.Pack(typ, fields)
}
func (a *RecipeArena) Select(typ string, predicate, whenTrue, whenFalse RecipeID) (RecipeID, error) {
	return a.runtime.builder.Select(typ, predicate, whenTrue, whenFalse)
}
func (a *RecipeArena) RecurrenceRef(typ, identity string, references []RecipeID) (RecipeID, error) {
	return a.runtime.builder.RecurrenceRef(typ, identity, references)
}
func (a *RecipeArena) Storage(typ, identity string) (RecipeID, error) {
	return a.runtime.builder.Storage(typ, identity)
}
func (a *RecipeArena) Primitive(typ string, mode ArithmeticMode, op string, operands []RecipeID) (RecipeID, error) {
	return buildPrimitive(a.runtime.builder.recipeStore, typ, mode, op, operands)
}
func (a *RecipeArena) Lookup(id RecipeID) (RecipeNode, bool) { return a.runtime.metadata.Lookup(id) }
func (a *RecipeArena) Snapshot() []RecipeNode                { return a.runtime.metadata.Snapshot() }
func (a *RecipeArena) NodeCount() int                        { return a.runtime.metadata.NodeCount() }
func (a *RecipeArena) MaxNodes() int                         { return a.runtime.metadata.MaxNodes() }

func (a *RecipeArena) BuildOutcome(id RecipeID) Outcome { return a.runtime.analyzer.BuildOutcome(id) }
func (a *RecipeArena) Classify(id RecipeID) TransformationClass {
	return a.runtime.analyzer.Classify(id)
}
func (a *RecipeArena) Substitute(root RecipeID, bindings map[int]RecipeID) (RecipeID, error) {
	return a.runtime.sub.Substitute(root, bindings)
}
func (a *RecipeArena) SubstituteWithAccounting(root RecipeID, bindings map[int]RecipeID) SubstituteResult {
	return a.runtime.sub.SubstituteWithAccounting(root, bindings)
}

func appendString(b []byte, value string) []byte {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	return append(append(b, size[:]...), value...)
}

func appendID(b []byte, id RecipeID) []byte { return appendString(b, string(id)) }

func canonicalNode(node RecipeNode) []byte {
	b := make([]byte, 0, 128)
	b = appendString(b, string(node.Kind))
	b = appendString(b, node.Type)
	b = appendString(b, node.Operator)
	b = appendString(b, string(node.ArithmeticMode))
	var integer [8]byte
	binary.BigEndian.PutUint64(integer[:], uint64(node.FormalIndex+1))
	b = append(b, integer[:]...)
	b = appendString(b, node.Literal)
	b = appendString(b, node.Field)
	b = appendString(b, string(node.Predicate))
	b = appendString(b, string(node.TrueValue))
	b = appendString(b, string(node.FalseValue))
	b = appendString(b, node.RecurrenceID)
	b = appendString(b, node.StorageID)
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(node.Children)))
	b = append(b, count[:]...)
	for _, child := range node.Children {
		b = appendID(b, child)
	}
	binary.BigEndian.PutUint64(count[:], uint64(len(node.Fields)))
	b = append(b, count[:]...)
	for _, field := range node.Fields {
		b = appendString(b, field.Field)
		b = appendID(b, field.Recipe)
	}
	return b
}

func (a *recipeStore) intern(node RecipeNode) RecipeID {
	digest := sha256.Sum256(canonicalNode(node))
	id := RecipeID(fmt.Sprintf("%x", digest[:]))
	if old, ok := a.nodes[id]; ok {
		return old.ID
	}
	node.ID = id
	a.nodes[id] = recipeNode{RecipeNode: node}
	return id
}

func (a *recipeStore) validateChildren(children []RecipeID, min, max int) error {
	if len(children) < min || (max >= 0 && len(children) > max) {
		return &RecipeError{Code: ReasonInvalidArity, Message: fmt.Sprintf("got %d children, want %d..%d", len(children), min, max)}
	}
	for _, child := range children {
		if _, ok := a.nodes[child]; !ok {
			return &RecipeError{Code: ReasonInvalidChild, Message: string(child)}
		}
	}
	return nil
}

func (a *recipeStore) unknownResult(code, message string) (RecipeID, error) {
	key := code + "\x00" + message
	if id, ok := a.unknown[key]; ok {
		return id, &RecipeError{Code: code, Message: message}
	}
	id := a.intern(RecipeNode{Kind: KindUnknown, Type: "unknown", Literal: key})
	a.unknown[key] = id
	return id, &RecipeError{Code: code, Message: message}
}
