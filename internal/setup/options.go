package setup

// Options is the resolved, client-neutral input to a setup run.
//
// The commandline package is responsible only for turning argv into these
// fields; it must not read or write any client configuration. All paths here
// are expected to be already resolved (RootDir absolute, ArkPath/Name
// defaulted) by Run's preflight before an adapter sees them.
type Options struct {
	// Client is the target coding agent.
	Client ClientID
	// Name is the MCP server entry name (default "ark").
	Name string
	// ArkPath is the command stored in the MCP config (default "ark").
	ArkPath string
	// RootDir is the repository root Ark should serve (absolute).
	RootDir string
	// Global selects the client's user-level MCP configuration instead of the
	// project-level one. The concrete location is client-specific (plan §15).
	Global bool
	// Force permits replacing an existing Ark-owned entry on conflict. It never
	// authorizes touching unrelated configuration (plan §4).
	Force bool
}

// scopeLabel returns "project" or "global" for output.
func (o Options) scopeLabel() string {
	if o.Global {
		return "global"
	}
	return "project"
}
