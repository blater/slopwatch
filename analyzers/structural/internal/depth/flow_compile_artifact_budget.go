package depth

import "slopslap.dev/structural/internal/facts"

func preflightArtifactMetadata(artifact facts.FlowArtifact, options CompileOptions) (int, error) {
	count := compilePreflight{}
	if err := metadataAdd(&count, len(artifact.Types), options); err != nil {
		return 0, err
	}
	visiting := map[*facts.TypeShape]bool{}
	for _, typ := range artifact.Types {
		if err := metadataAdd(&count, len(typ.Fields)+len(typ.Methods), options); err != nil {
			return 0, err
		}
		for _, field := range typ.Fields {
			if err := preflightTypeShape(&count, field.Type, visiting, options); err != nil {
				return 0, err
			}
		}
	}
	if err := metadataAdd(&count, len(artifact.PublicRoutes), options); err != nil {
		return 0, err
	}
	for _, route := range artifact.PublicRoutes {
		if err := metadataAdd(&count, len(route.RequiredSlots)+len(route.ExposedSlots)+len(route.RequiredPolicies)+len(route.LifecycleRelations)+len(route.Sequencing), options); err != nil {
			return 0, err
		}
	}
	if err := metadataAdd(&count, len(artifact.Provenance), options); err != nil {
		return 0, err
	}
	for _, provenance := range artifact.Provenance {
		if err := metadataAdd(&count, 1+len(provenance.FactIDs), options); err != nil {
			return 0, err
		}
	}
	return count.work, nil
}

func preflightTypeShape(count *compilePreflight, shape *facts.TypeShape, visiting map[*facts.TypeShape]bool, options CompileOptions) error {
	type frame struct {
		shape *facts.TypeShape
		next  int
	}
	if shape == nil {
		return nil
	}
	stack := []frame{{shape: shape}}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next == 0 {
			if visiting[top.shape] {
				return compileFailure("", "", "", "cyclic_type_shape", "type shape contains a cycle")
			}
			visiting[top.shape] = true
			if err := metadataAdd(count, 1+len(top.shape.Children)+len(top.shape.ExposedMembers), options); err != nil {
				return err
			}
		}
		if top.next == len(top.shape.Children) {
			delete(visiting, top.shape)
			stack = stack[:len(stack)-1]
			continue
		}
		child := top.shape.Children[top.next]
		top.next++
		if child != nil {
			stack = append(stack, frame{shape: child})
		}
	}
	return nil
}

func metadataAdd(count *compilePreflight, amount int, options CompileOptions) error {
	return addPreflight(count, amount, options, "", "", "")
}
