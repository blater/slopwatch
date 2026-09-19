package goadapter

import (
	"go/types"
	"slopslap.dev/structural/internal/facts"
)

func depthType(typ types.Type) (string, facts.FlowValueKind, string) {
	if typ == nil {
		return "unknown", facts.FlowKindUnknown, ""
	}
	if depthErrorType(typ) {
		return "error", facts.FlowKindError, ""
	}
	if _, named := typ.(*types.Named); named {
		return types.TypeString(typ, nil), facts.FlowKindUnknown, ""
	}
	basic, ok := typ.Underlying().(*types.Basic)
	if !ok {
		return types.TypeString(typ, nil), facts.FlowKindUnknown, ""
	}
	// Default untyped literals before transport; no evaluator guesses kinds from
	// spelling. Contextual assignment/operand conversions are resolved by go/types.
	if basic.Info()&types.IsUntyped != 0 {
		typ = types.Default(typ)
		basic = typ.Underlying().(*types.Basic)
	}
	name := types.TypeString(typ, nil)
	switch {
	case basic.Info()&types.IsInteger != 0:
		return name, facts.FlowKindNumeric, "integer"
	case basic.Info()&types.IsFloat != 0:
		return name, facts.FlowKindNumeric, "float"
	case basic.Info()&types.IsBoolean != 0:
		return name, facts.FlowKindBoolean, "boolean"
	case basic.Info()&types.IsString != 0:
		return name, facts.FlowKindString, "text"
	default:
		return name, facts.FlowKindUnknown, ""
	}
}

func depthConcept(kind facts.FlowValueKind) string {
	switch kind {
	case facts.FlowKindNumeric:
		return "number"
	case facts.FlowKindBoolean:
		return "boolean"
	case facts.FlowKindString:
		return "text"
	case facts.FlowKindError:
		return "error"
	default:
		return "unknown"
	}
}
