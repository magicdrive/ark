package typescript_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/index"
)

// synthRepo builds n feature modules, each with a service class, a repository
// class (all sharing the member names save/find/create), a barrel and a TSX
// component, wired together with relative imports, aliases, namespace imports,
// typed receivers, inheritance and `export *` chains.
func synthRepo(n int) map[string]string {
	files := map[string]string{
		"base.ts": "export class BaseService { log(m: string) {} }\nexport interface Service { create(n: string): unknown }\n",
	}
	var barrel strings.Builder
	for i := range n {
		m := fmt.Sprintf("m%d", i)
		files[m+"/entity.ts"] = fmt.Sprintf("export class Entity%d { constructor(public id: string) {} }\n", i)
		files[m+"/repo.ts"] = fmt.Sprintf(`import { Entity%[1]d } from "./entity";
export class Repo%[1]d {
  save(e: Entity%[1]d): void {}
  find(id: string): Entity%[1]d | undefined { return undefined; }
}
`, i)
		files[m+"/index.ts"] = "export * from \"./entity\";\nexport * from \"./repo\";\nexport * from \"./service\";\n"
		files[m+"/service.ts"] = fmt.Sprintf(`import { BaseService, Service } from "../base";
import { Repo%[1]d } from "./repo";
import { Entity%[1]d as E } from "./entity";
import * as ns from "./repo";
export class Service%[1]d extends BaseService implements Service {
  constructor(private readonly repo: Repo%[1]d) { super(); }
  create(name: string): E {
    const e = new E(name);
    this.repo.save(e);
    this.audit();
    return e;
  }
  audit(): void { const r = new ns.Repo%[1]d(); r.find("x"); }
  static from(r: Repo%[1]d): Service%[1]d { return new Service%[1]d(r); }
}
`, i)
		files[m+"/view.tsx"] = fmt.Sprintf(`import { Service%[1]d } from "./index";
export function View%[1]d(p: { s: Service%[1]d }) { return <div><Item%[1]d /></div>; }
export const Item%[1]d = () => <span />;
`, i)
		if i > 0 {
			fmt.Fprintf(&barrel, "import { Service%d } from \"./m%d\";\nexport function use%d() { return new Service%d(null as any); }\n", i, i, i, i)
		}
	}
	files["app.ts"] = barrel.String()
	return files
}

func TestCorpus_ScalingAndCorrectness(t *testing.T) {
	var perFile []float64
	for _, n := range []int{20, 40, 80} {
		root := writeRepo(t, synthRepo(n))
		start := time.Now()
		idx, err := index.New(context.Background(), root, tsProviders())
		if err != nil {
			t.Fatal(err)
		}
		el := time.Since(start)
		files := idx.Stats().Files
		perFile = append(perFile, float64(el.Microseconds())/float64(files))
		t.Logf("n=%d files=%d symbols=%d refs=%d edges=%d elapsed=%s (%.0fµs/file) skipped=%d",
			n, files, idx.Stats().Symbols, idx.Stats().References, idx.Stats().Relations, el, perFile[len(perFile)-1], idx.Stats().Skipped)
		if idx.Stats().Skipped != 0 {
			t.Errorf("n=%d: %d files skipped", n, idx.Stats().Skipped)
		}
		if len(idx.Diagnostics()) != 0 {
			t.Errorf("n=%d: unexpected diagnostics %v", n, idx.Diagnostics())
		}

		edges := edgeSet(t, idx)
		checkGraphInvariants(t, idx)
		for i := range n {
			from := fmt.Sprintf("m%d/service.ts:Service%d.create", i, i)
			wantEdges(t, edges, from,
				fmt.Sprintf("-calls-> m%d/entity.ts:Entity%d exact", i, i),
				fmt.Sprintf("-calls-> m%d/repo.ts:Repo%d.save exact", i, i),
				fmt.Sprintf("-calls-> m%d/service.ts:Service%d.audit exact", i, i),
				fmt.Sprintf("-uses_type-> m%d/entity.ts:Entity%d exact", i, i))
			// Same member names in every module: no cross-module leakage.
			for _, e := range edgesFrom(edges, from) {
				for j := range n {
					if j != i && strings.Contains(e, fmt.Sprintf("m%d/", j)) {
						t.Errorf("%s leaks into module %d: %s", from, j, e)
					}
				}
			}
			// Barrel chain resolves to the defining module through ./index.
			if i > 0 {
				wantEdges(t, edges, fmt.Sprintf("app.ts:use%d", i),
					fmt.Sprintf("-calls-> m%d/service.ts:Service%d exact", i, i))
			}
			wantEdges(t, edges, fmt.Sprintf("m%d/service.ts:Service%d", i, i),
				"-extends-> base.ts:BaseService exact", "-implements-> base.ts:Service exact")
			wantEdges(t, edges, fmt.Sprintf("m%d/service.ts:Service%d.constructor", i, i),
				fmt.Sprintf("-uses_type-> m%d/repo.ts:Repo%d exact", i, i))
			wantEdges(t, edges, fmt.Sprintf("m%d/view.tsx:View%d", i, i),
				fmt.Sprintf("-calls-> m%d/view.tsx:Item%d exact", i, i),
				fmt.Sprintf("-uses_type-> m%d/service.ts:Service%d exact", i, i))
		}
	}
	// Roughly linear: per-file cost at 4x the files stays within a generous bound.
	if perFile[2] > perFile[0]*8+2000 {
		t.Errorf("per-file cost grows super-linearly: %.0fµs → %.0fµs per file", perFile[0], perFile[2])
	}
}
