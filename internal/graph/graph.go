package graph

import (
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/symbol"
)

// Graph wraps a RepositoryIndex and provides transitive traversal queries.
type Graph struct {
	idx *index.RepositoryIndex
}

// New returns a Graph backed by idx.
func New(idx *index.RepositoryIndex) *Graph {
	return &Graph{idx: idx}
}

// TransitiveCallees returns all symbols reachable from id via call edges,
// up to maxDepth hops. maxDepth <= 0 means no limit (use with caution on
// large graphs — cycle protection is built in).
func (g *Graph) TransitiveCallees(id symbol.SymbolID, maxDepth int) []index.GraphEdge {
	return g.bfs(id, maxDepth, g.idx.GetCallees)
}

// TransitiveCallers returns all symbols that transitively call id,
// up to maxDepth hops.
func (g *Graph) TransitiveCallers(id symbol.SymbolID, maxDepth int) []index.GraphEdge {
	return g.bfs(id, maxDepth, g.idx.GetCallers)
}

type neighborFn func(symbol.SymbolID) []index.GraphEdge

func (g *Graph) bfs(start symbol.SymbolID, maxDepth int, neighbors neighborFn) []index.GraphEdge {
	type item struct {
		id    symbol.SymbolID
		depth int
	}

	visited := make(map[symbol.SymbolID]bool)
	visited[start] = true

	queue := []item{{id: start, depth: 0}}
	var out []index.GraphEdge

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if maxDepth > 0 && cur.depth >= maxDepth {
			continue
		}

		for _, edge := range neighbors(cur.id) {
			peer := edge.To
			if edge.Kind == index.EdgeCalledBy {
				peer = edge.From
			}
			out = append(out, edge)
			if !visited[peer] {
				visited[peer] = true
				queue = append(queue, item{id: peer, depth: cur.depth + 1})
			}
		}
	}

	return out
}
