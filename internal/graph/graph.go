package graph

import (
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/resolver"
	"github.com/magicdrive/ark/internal/symbol"
)

// Graph wraps a RepositoryIndex and provides transitive traversal queries.
//
// Direction contract (index.RepositoryIndex): GetCallees(id) returns the
// forward edges of id (From == id, To == a callee); GetCallers(id) returns the
// reverse edges stored for id (From == id, Kind == index.EdgeCalledBy,
// To == a caller). In both directions the symbol one hop further is
// therefore edge.To; the edge kind never changes which end that is.
type Graph struct {
	idx *index.RepositoryIndex
}

// New returns a Graph backed by idx.
func New(idx *index.RepositoryIndex) *Graph {
	return &Graph{idx: idx}
}

// Hop is one symbol reached by a traversal along one chosen path from the
// start. Depth is its hop count (1 = a direct neighbour): the shortest path's
// length. Confidence is the chosen path's confidence — that of its weakest
// edge, the most a chain of evidence can claim. Among the shortest paths the
// one with the highest such confidence is chosen; among equals, the first in
// traversal order. Edge is the chosen path's last edge (Edge.To is the
// symbol): its own evidence is about that one hop, not about the path.
type Hop struct {
	Edge       index.GraphEdge
	Depth      int
	Confidence resolver.Confidence
}

// TransitiveCallees returns, for every symbol reachable from id through graph
// edges within maxDepth hops, the edge it was first reached through.
// maxDepth <= 0 means no limit (cycle protection is built in). Each symbol
// appears once, the start never; the order is breadth-first and
// deterministic.
func (g *Graph) TransitiveCallees(id symbol.SymbolID, maxDepth int) []index.GraphEdge {
	return edgesOf(g.bfs(id, maxDepth, g.idx.GetCallees))
}

// TransitiveCallers returns, for every symbol that transitively refers to id
// within maxDepth hops, the reverse edge it was first reached through
// (Edge.To is the caller). Same depth, uniqueness and order contract as
// TransitiveCallees.
func (g *Graph) TransitiveCallers(id symbol.SymbolID, maxDepth int) []index.GraphEdge {
	return edgesOf(g.bfs(id, maxDepth, g.idx.GetCallers))
}

// TransitiveCallerHops is TransitiveCallers with each caller's hop count.
func (g *Graph) TransitiveCallerHops(id symbol.SymbolID, maxDepth int) []Hop {
	return g.bfs(id, maxDepth, g.idx.GetCallers)
}

type neighborFn func(symbol.SymbolID) []index.GraphEdge

func edgesOf(hops []Hop) []index.GraphEdge {
	out := make([]index.GraphEdge, len(hops))
	for i, h := range hops {
		out[i] = h.Edge
	}
	return out
}

// bfs visits symbols breadth-first from start, one depth at a time.
// neighbors returns edges whose To is the next symbol in the traversal
// direction (see Graph). A symbol's depth is fixed by the layer that first
// reaches it; within that layer every edge into it competes and the strongest
// path confidence wins (min of the predecessor's path confidence and the
// edge's). Frontier order is the order symbols were first reached and
// neighbour order is the index's sorted edge order, so the result — symbols,
// depths, confidences and chosen edges — is deterministic.
func (g *Graph) bfs(start symbol.SymbolID, maxDepth int, neighbors neighborFn) []Hop {
	visited := map[symbol.SymbolID]bool{start: true}
	// pathConf of the start is the identity of min: Exact.
	pathConf := map[symbol.SymbolID]resolver.Confidence{start: resolver.ConfidenceExact}
	frontier := []symbol.SymbolID{start}
	var out []Hop

	for depth := 1; len(frontier) > 0 && (maxDepth <= 0 || depth <= maxDepth); depth++ {
		layer := map[symbol.SymbolID]int{} // symbol -> index in out
		var next []symbol.SymbolID
		for _, cur := range frontier {
			for _, edge := range neighbors(cur) {
				peer := edge.To
				conf := min(pathConf[cur], edge.Confidence)
				if i, ok := layer[peer]; ok {
					if conf > out[i].Confidence {
						out[i].Edge, out[i].Confidence = edge, conf
						pathConf[peer] = conf
					}
					continue
				}
				if visited[peer] {
					continue // reached by a shorter path
				}
				visited[peer] = true
				pathConf[peer] = conf
				layer[peer] = len(out)
				out = append(out, Hop{Edge: edge, Depth: depth, Confidence: conf})
				next = append(next, peer)
			}
		}
		frontier = next
	}
	return out
}
