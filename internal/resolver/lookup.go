package resolver

import (
	"path/filepath"

	"github.com/magicdrive/ark/internal/source"
	"github.com/magicdrive/ark/internal/symbol"
)

// Lookup indexes for the name-based resolution stages.
//
// receiverMatch, byNameSuffix and samePackageMatch used to scan every symbol of
// the repository for every reference, which made resolution O(references ×
// symbols). Each index below is keyed by the exact value that stage compares,
// so a lookup yields the same symbols the scan selected — the stage's remaining
// filters then run unchanged. Entries are appended in r.files order (and symbol
// order within a file), so every lookup has a fixed order that does not depend
// on map iteration. The stages' callers order candidates themselves
// (pickBest / sortCandidates); the indexes never decide which candidate wins.
//
// Entries point into r.files' symbol slices, which the resolver never modifies,
// rather than holding copies, so the indexes cost a pointer per entry.

// dirSymbol is a symbol together with the FileIndex it was declared in, as the
// same-package stage excludes the referencing file by FileIndex identity.
type dirSymbol struct {
	file source.FileID
	sym  *symbol.Symbol
}

func (r *Resolver) buildLookupIndexes() {
	r.members = make(map[string][]*symbol.Symbol)
	r.suffixes = make(map[string][]*symbol.Symbol)
	r.dirSymbols = make(map[string]map[string][]dirSymbol)
	for i := range r.files {
		fi := &r.files[i]
		dir := filepath.Dir(string(fi.FileID))
		byName := r.dirSymbols[dir]
		if byName == nil {
			byName = make(map[string][]dirSymbol)
			r.dirSymbols[dir] = byName
		}
		for k := range fi.Symbols {
			sym := &fi.Symbols[k]
			// samePackageMatch: every symbol of every file, by (directory, name).
			byName[sym.Name] = append(byName[sym.Name], dirSymbol{file: fi.FileID, sym: sym})

			// receiverMatch and byNameSuffix scanned r.byQualified, which holds
			// only symbols with a qualified name.
			if sym.Qualified == "" {
				continue
			}
			if sym.Receiver != "" {
				r.members[sym.Name] = append(r.members[sym.Name], sym)
			}
			// strings.HasSuffix(q, "."+name) holds exactly when name is the text
			// after one of q's dots; each dot gives a distinct suffix.
			q := sym.Qualified
			for j := 0; j < len(q); j++ {
				if q[j] == '.' {
					r.suffixes[q[j+1:]] = append(r.suffixes[q[j+1:]], sym)
				}
			}
		}
	}
}
