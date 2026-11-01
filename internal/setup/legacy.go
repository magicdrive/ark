package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// peekedServers is what a read-only look at a JSON MCP configuration found.
// It serves the advisories about files earlier Ark versions wrote where the
// client never reads them (~/.claude/settings.json, ~/.cline/mcp.json): Ark
// only reports on those files and never changes them.
type peekedServers struct {
	exists  bool
	servers map[string]any // the server map; nil when absent or not inspectable
	// problem is non-empty when the file exists but could not be inspected
	// safely (symlink, not a regular file, too large, unreadable, not JSON, or
	// a server map that is not an object).
	problem string
}

// peekServers reads the server map under key from path without modifying
// anything. A symlinked or non-regular path is not followed, the same rule the
// writers apply (loadJSONConfigKey); it is reported as a problem instead.
func peekServers(path, key string) peekedServers {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return peekedServers{}
	}
	if err != nil {
		return peekedServers{exists: true, problem: fmt.Sprintf("cannot access it: %v", err)}
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return peekedServers{exists: true, problem: "it is a symlink, which Ark does not follow"}
	case !info.Mode().IsRegular():
		return peekedServers{exists: true, problem: "it is not a regular file"}
	case info.Size() > maxConfigSize:
		return peekedServers{exists: true, problem: fmt.Sprintf("it is larger than %d bytes", maxConfigSize)}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return peekedServers{exists: true, problem: fmt.Sprintf("cannot read it: %v", err)}
	}
	doc := map[string]any{}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &doc); err != nil {
			return peekedServers{exists: true, problem: "it is not valid JSON"}
		}
	}
	switch v := doc[key].(type) {
	case nil:
		return peekedServers{exists: true, servers: map[string]any{}}
	case map[string]any:
		return peekedServers{exists: true, servers: v}
	default:
		return peekedServers{exists: true, problem: fmt.Sprintf("its %q is not an object", key)}
	}
}

// otherServerNames returns the names in servers other than name, sorted.
func otherServerNames(servers map[string]any, name string) []string {
	var out []string
	for k := range servers {
		if k != name {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
