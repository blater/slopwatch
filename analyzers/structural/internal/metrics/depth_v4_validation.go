package metrics

import (
	"fmt"
	"slopslap.dev/structural/internal/facts"
	"strings"
)

func validateBoundary(boundary facts.BoundaryAssessment) error {
	if err := validateIdentity(boundary.Identity); err != nil {
		return err
	}
	if err := validateBurden(boundary.Burden); err != nil {
		return err
	}
	if err := validateInventory(boundary); err != nil {
		return err
	}
	if err := validateRouteFamilies(boundary.RouteFamilies); err != nil {
		return err
	}
	if err := validateKnowledge(boundary); err != nil {
		return err
	}
	if boundary.State == facts.KnowledgeMeasured && boundary.Burden.O < 1 {
		return fmt.Errorf("measured boundary must expose at least one service family")
	}
	if boundary.State == facts.KnowledgeMeasured && len(boundary.FamilyAlternatives) == 0 {
		return fmt.Errorf("measured boundary has no service alternatives")
	}
	if boundary.RouteFamilies != nil && int64(len(boundary.RouteFamilies)) != boundary.Burden.O {
		return fmt.Errorf("route family inventory conflicts with O")
	}
	obligations, err := obligationIndex(boundary.Obligations)
	if err != nil {
		return err
	}
	return validateAlternativeReferences(boundary.FamilyAlternatives, obligations)
}

func validateIdentity(identity facts.BoundaryIdentity) error {
	if identity.Artifact == "" || identity.Audience == "" || identity.View == "" || identity.Symbol == "" {
		return fmt.Errorf("boundary identity is incomplete")
	}
	return nil
}
func validateBurden(b facts.Burden) error {
	values := []int64{b.O, b.T, b.A, b.E, b.P, b.S, b.L}
	for _, value := range values {
		if value < 0 {
			return fmt.Errorf("negative burden")
		}
	}
	return nil
}
func validateInventory(boundary facts.BoundaryAssessment) error {
	concepts := map[string]facts.Concept{}
	for _, concept := range boundary.Concepts {
		if concept.ID == "" || concept.Kind == "" {
			return fmt.Errorf("concept identity is incomplete")
		}
		if prior, ok := concepts[concept.ID]; ok && (prior.Kind != concept.Kind || !sameStrings(prior.Children, concept.Children)) {
			return fmt.Errorf("conflicting duplicate concept ID %s", concept.ID)
		}
		concepts[concept.ID] = concept
	}
	for _, slot := range boundary.Slots {
		if slot.ID == "" {
			return fmt.Errorf("slot has empty ID")
		}
	}
	if hasEmpty(boundary.LeakRoots) {
		return fmt.Errorf("leak root has empty ID")
	}
	if err := validateAlternativeFamilyMapping(boundary); err != nil {
		return err
	}
	return nil
}

func validateAlternativeFamilyMapping(boundary facts.BoundaryAssessment) error {
	if boundary.RouteFamilies == nil || boundary.State == facts.KnowledgeNotApplicable {
		return nil
	}
	if len(boundary.FamilyAlternatives) != len(boundary.RouteFamilies) {
		return fmt.Errorf("route family/alternative count mismatch")
	}
	ids := make(map[string]bool, len(boundary.RouteFamilies))
	for _, family := range boundary.RouteFamilies {
		ids[family.ID] = true
	}
	if len(boundary.FamilyAlternativeIDs) == 0 {
		return nil
	}
	if len(boundary.FamilyAlternativeIDs) != len(boundary.RouteFamilies) {
		return fmt.Errorf("family alternative mapping length mismatch")
	}
	seen := make(map[string]bool, len(boundary.FamilyAlternativeIDs))
	for _, id := range boundary.FamilyAlternativeIDs {
		if id == "" {
			return fmt.Errorf("family alternative has empty family ID")
		}
		if seen[id] {
			return fmt.Errorf("duplicate family alternative ID %s", id)
		}
		if !ids[id] {
			return fmt.Errorf("family alternative references unknown family %s", id)
		}
		seen[id] = true
	}
	return nil
}
func validateKnowledge(boundary facts.BoundaryAssessment) error {
	switch boundary.State {
	case facts.KnowledgeMeasured, facts.KnowledgePartial, facts.KnowledgeUnavailable, facts.KnowledgeNotApplicable:
	default:
		return fmt.Errorf("unknown knowledge state %q", boundary.State)
	}
	for dimension, item := range boundary.Knowledge {
		switch item.State {
		case facts.KnowledgeMeasured, facts.KnowledgePartial, facts.KnowledgeUnavailable, facts.KnowledgeNotApplicable:
		default:
			return fmt.Errorf("unknown knowledge state %q for %s", item.State, dimension)
		}
	}
	if boundary.State != facts.KnowledgeMeasured {
		return nil
	}
	for _, dimension := range []string{"inventory", "burden", "behavior", "alias_effects"} {
		item, ok := boundary.Knowledge[dimension]
		if !ok {
			return fmt.Errorf("missing knowledge dimension %s", dimension)
		}
		if item.State != facts.KnowledgeMeasured && item.State != facts.KnowledgePartial && item.State != facts.KnowledgeUnavailable {
			return fmt.Errorf("unknown knowledge state %q for %s", item.State, dimension)
		}
	}
	return nil
}
func validateAlternativeReferences(families [][]facts.ObligationSet, obligations map[string]facts.Obligation) error {
	for familyIndex, family := range families {
		for _, alternative := range family {
			for _, id := range alternative {
				if id == "" {
					return fmt.Errorf("family %d has empty obligation reference", familyIndex)
				}
				if _, ok := obligations[id]; !ok {
					return fmt.Errorf("unknown obligation reference %s", id)
				}
			}
		}
	}
	return nil
}
func hasEmpty(items []string) bool {
	for _, item := range items {
		if item == "" {
			return true
		}
	}
	return false
}
func sameStrings(left, right []string) bool {
	return strings.Join(left, "\x00") == strings.Join(right, "\x00")
}
