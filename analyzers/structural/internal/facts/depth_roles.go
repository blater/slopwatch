package facts

// SupportingContract records resolved production relationships, not a role
// inferred from a name or a numerical responsibility bonus. Members is the
// complete candidate member inventory, including unrelated public members.
type SupportingContract struct {
	Inventory      KnowledgeState `json:"inventory"`
	Exposure       KnowledgeState `json:"exposure"`
	Members        []string       `json:"members"`
	ExternalRoutes []string       `json:"external_routes"`
	Bindings       []ContractUse  `json:"bindings"`
}

// ContractUse links an implementation member to a contract and its actual
// production injection/use. Test-only uses must not be emitted here.
type ContractUse struct {
	Member         string       `json:"member"`
	Contract       string       `json:"contract"`
	ContractMember string       `json:"contract_member"`
	Consumer       string       `json:"consumer"`
	Slot           string       `json:"slot"`
	Use            string       `json:"use"`
	Provenance     []Provenance `json:"provenance"`
}
