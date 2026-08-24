package policy

type Config struct {
	SoftDeny []string `yaml:"soft_deny"`
	HardDeny []string `yaml:"hard_deny"`

	// Namespace is the Rego package conftest evaluates (conftest --namespace),
	// defaulting to "main" (querying data.main.deny).
	Namespace string `yaml:"namespace"`

	// Data lists directories of conftest --data JSON files, provided to both
	// tiers as data documents
	Data []string `yaml:"data"`
}
