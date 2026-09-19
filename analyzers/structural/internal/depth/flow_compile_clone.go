package depth

import "slopslap.dev/structural/internal/facts"

func artifactHeader(in facts.FlowArtifact) facts.FlowArtifact {
	return facts.FlowArtifact{Artifact: in.Artifact, Language: in.Language, BuildSelection: in.BuildSelection}
}

func cloneInstruction(in facts.Instruction) facts.Instruction {
	out := in
	out.Operands = cloneStrings(in.Operands)
	out.Results = cloneStrings(in.Results)
	out.Roots = cloneRoots(in.Roots)
	out.Effects = cloneStrings(in.Effects)
	out.Provenance = cloneProvenance(in.Provenance)
	out.Bindings = cloneSlice(in.Bindings)
	out.FieldBindings = cloneSlice(in.FieldBindings)
	out.PhiInputs = cloneSlice(in.PhiInputs)
	if in.AccessPath != nil {
		out.AccessPath = append([]facts.AliasPathSegment(nil), in.AccessPath...)
	}
	if in.Call != nil {
		call := *in.Call
		call.Targets = cloneStrings(in.Call.Targets)
		call.Bindings = cloneSlice(in.Call.Bindings)
		call.ResultBindings = cloneSlice(in.Call.ResultBindings)
		call.ErrorContinuations = cloneStrings(in.Call.ErrorContinuations)
		call.OwnershipEffects = cloneStrings(in.Call.OwnershipEffects)
		out.Call = &call
	}
	if in.Value != nil {
		value := *in.Value
		value.Concepts = cloneStrings(in.Value.Concepts)
		value.MayRoots = cloneStrings(in.Value.MayRoots)
		out.Value = &value
	}
	return out
}

func cloneRoots(in []facts.AliasRoot) []facts.AliasRoot {
	if in == nil {
		return nil
	}
	out := make([]facts.AliasRoot, len(in))
	for i, root := range in {
		out[i] = root
		out[i].PathSegments = append([]facts.AliasPathSegment(nil), root.PathSegments...)
	}
	return out
}

func cloneProvenance(in []facts.Provenance) []facts.Provenance {
	if in == nil {
		return nil
	}
	out := make([]facts.Provenance, len(in))
	for i, item := range in {
		out[i] = item
		out[i].FactIDs = cloneStrings(item.FactIDs)
	}
	return out
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneTypeShape(in *facts.TypeShape) *facts.TypeShape {
	if in == nil {
		return nil
	}
	root := &facts.TypeShape{StableID: in.StableID, Kind: in.Kind, Name: in.Name, Complexity: in.Complexity, ExposedMembers: cloneStrings(in.ExposedMembers)}
	root.Children = make([]*facts.TypeShape, len(in.Children))
	type frame struct {
		source, target *facts.TypeShape
		next           int
	}
	stack := []frame{{source: in, target: root}}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next == len(top.source.Children) {
			stack = stack[:len(stack)-1]
			continue
		}
		i := top.next
		top.next++
		child := top.source.Children[i]
		if child == nil {
			continue
		}
		copy := &facts.TypeShape{StableID: child.StableID, Kind: child.Kind, Name: child.Name, Complexity: child.Complexity, ExposedMembers: cloneStrings(child.ExposedMembers)}
		copy.Children = make([]*facts.TypeShape, len(child.Children))
		top.target.Children[i] = copy
		stack = append(stack, frame{source: child, target: copy})
	}
	return root
}

func cloneSlice[T any](in []T) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	copy(out, in)
	return out
}

func cloneTypeShapes(in []*facts.TypeShape) []*facts.TypeShape {
	if in == nil {
		return nil
	}
	out := make([]*facts.TypeShape, len(in))
	for i, child := range in {
		out[i] = cloneTypeShape(child)
	}
	return out
}

func cloneField(in facts.Field) facts.Field {
	out := in
	out.Type = cloneTypeShape(in.Type)
	return out
}

func cloneFlowType(in facts.FlowType) facts.FlowType {
	out := in
	if in.Fields != nil {
		out.Fields = make([]facts.Field, len(in.Fields))
		for i := range in.Fields {
			out.Fields[i] = cloneField(in.Fields[i])
		}
	}
	out.Methods = cloneStrings(in.Methods)
	return out
}

func cloneFlowTypes(in []facts.FlowType) []facts.FlowType {
	if in == nil {
		return nil
	}
	out := make([]facts.FlowType, len(in))
	for i := range in {
		out[i] = cloneFlowType(in[i])
	}
	return out
}

func cloneRoutes(in []facts.Route) []facts.Route {
	if in == nil {
		return nil
	}
	out := make([]facts.Route, len(in))
	for i, route := range in {
		out[i] = route
		out[i].RequiredSlots = cloneStrings(route.RequiredSlots)
		out[i].ExposedSlots = cloneStrings(route.ExposedSlots)
		out[i].RequiredPolicies = cloneStrings(route.RequiredPolicies)
		out[i].LifecycleRelations = cloneStrings(route.LifecycleRelations)
		out[i].Sequencing = cloneStrings(route.Sequencing)
	}
	return out
}
