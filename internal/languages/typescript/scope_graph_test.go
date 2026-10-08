package typescript_test

import (
	"slices"
	"strings"
	"testing"
)

// A mapped-type key, an `infer` name or a nested signature's type parameter
// shadows a repository type of the same name: it is never an edge to it.
func TestGraph_TypeScopedNamesAreNoEdgesToSameNamedTypes(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"src/k.ts": "export type K = string;\nexport type U = number;\nexport type V = boolean;\n",
		"src/m.ts": `import { K, U, V } from "./k";
export type Keys<T> = { [K in keyof T]: T[K] };
export type Elem<T> = T extends (infer U)[] ? U : never;
export type Fn = <V>(v: V) => V;
export type Uses = K | U | V;
`,
	})
	var bad, uses []string
	for _, e := range edgeSet(t, idx) {
		if strings.HasPrefix(e, "src/m.ts:Uses ") {
			uses = append(uses, e)
		} else if strings.HasPrefix(e, "src/m.ts:") && strings.Contains(e, "-> src/k.ts:") {
			bad = append(bad, e)
		}
	}
	if len(bad) != 0 {
		t.Errorf("edges from scoped names to same-named types: %v", bad)
	}
	if len(uses) != 3 {
		t.Errorf("the real uses must stay edges: %v", uses)
	}
}

// A recovered generic call resolves like any call: through the module
// binding of its receiver, exactly; a call on a receiver of unproven type
// (z.number().apply<...>) is no edge.
func TestGraph_RecoveredGenericCallResolvesThroughItsBinding(t *testing.T) {
	idx := buildIndex(t, map[string]string{
		"src/processors.ts": "export function aggregateChecks<T>(x: unknown): { minimum: T } { return { minimum: x as T }; }\n",
		"src/use.ts": `import * as processors from "./processors";
export const c = processors.aggregateChecks<number>(inst).minimum ?? null;
z.number().apply<z.ZodMiniNumber>((schema) => schema.check(0));
`,
	})
	want := []string{"src/use.ts:c -calls-> src/processors.ts:aggregateChecks exact"}
	if got := edgeSet(t, idx); !slices.Equal(got, want) {
		t.Errorf("edges %v, want %v", got, want)
	}
}
