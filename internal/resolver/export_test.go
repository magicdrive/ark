package resolver

import (
	"path/filepath"
	"strings"

	"github.com/magicdrive/ark/internal/reference"
	"github.com/magicdrive/ark/internal/symbol"
)

// StageLookups returns what the receiver, same-package and qualified-suffix
// stages select for ref in fi, using the lookup indexes.
func (r *Resolver) StageLookups(ref reference.Reference, fi FileIndex) (recv, pkg, suffix []symbol.Symbol) {
	return r.receiverMatch(ref, fi), r.samePackageMatch(ref, fi), r.byNameSuffix(ref.Name)
}

// StageScans is the oracle for StageLookups: the full repository scans the
// lookup indexes replaced, kept verbatim so the two can be compared.
func (r *Resolver) StageScans(ref reference.Reference, fi FileIndex) (recv, pkg, suffix []symbol.Symbol) {
	for _, sym := range r.byQualified {
		for _, s := range sym {
			if s.Name == ref.Name && s.Receiver != "" {
				if strings.EqualFold(s.Receiver, ref.ReceiverExpr) ||
					strings.HasSuffix(strings.ToLower(s.Receiver), strings.ToLower(ref.ReceiverExpr)) {
					recv = append(recv, s)
				}
			}
		}
	}

	dir := filepath.Dir(string(fi.FileID))
	for _, f := range r.files {
		// Identity-only symbols are in no name-based stage (FileIndex.
		// IdentityOnly).
		if string(f.FileID) == string(fi.FileID) || f.IdentityOnly {
			continue
		}
		if filepath.Dir(string(f.FileID)) != dir {
			continue
		}
		for _, sym := range f.Symbols {
			if sym.Name == ref.Name {
				pkg = append(pkg, sym)
			}
		}
	}

	for q, syms := range r.byQualified {
		if strings.HasSuffix(q, "."+ref.Name) {
			suffix = append(suffix, syms...)
		}
	}
	return recv, pkg, suffix
}
