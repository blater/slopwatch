package unitplan

type cargoManifest struct {
	path          string
	directory     string
	packageName   string
	workspace     bool
	members       []string
	exclude       []string
	dependencies  []cargoDependency
	workspaceDeps map[string]cargoDependency
}

type cargoDependency struct {
	alias     string
	packageID string
	path      string
	kind      string
	workspace bool
}
