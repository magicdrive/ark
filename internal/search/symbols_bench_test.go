package search

import (
	"fmt"
	"testing"

	"github.com/magicdrive/ark/internal/symbol"
)

// BenchmarkMatchSymbols ranks a synthetic symbol set of growing size.
func BenchmarkMatchSymbols(b *testing.B) {
	words := []string{"user", "auth", "service", "get", "create", "token", "repository", "handler", "index", "context"}
	for _, n := range []int{1000, 10000, 100000} {
		syms := make([]symbol.Symbol, n)
		for i := range syms {
			name := fmt.Sprintf("%s%s%d", words[i%len(words)], words[(i/len(words))%len(words)], i)
			syms[i] = sym(fmt.Sprintf("pkg%d/f%d.go", i%97, i%13), "T"+fmt.Sprint(i%50)+"."+name, symbol.KindMethod, uint32(i%400+1))
			syms[i].Name = name
		}
		b.Run(fmt.Sprintf("symbols=%d", n), func(b *testing.B) {
			for b.Loop() {
				MatchSymbols("authServ", syms)
			}
		})
	}
}
