package php

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// destructuringRecovery is valid PHP (the reference Tree-sitter runtime with
// the same tree-sitter-php commit parses it without error), minimized by
// delta debugging from Slim's CallableResolver.php. gotreesitter's production
// and candidate routes reject the destructuring assignment on line 16; its
// forest route parses the file exactly as the reference runtime does, field
// names included (tsparse).
const destructuringRecovery = `<?php
/**
 */
final class CallableResolver implements AdvancedCallableResolverInterface
{
    public function __construct(?ContainerInterface $container = null)
    {
    }
    /**
     */
    private function isMiddleware($toResolve): bool
    {
    }
    private function resolveSlimNotation(string $toResolve): array
    {
        [$class, $method] = $matches ? [$matches[1], $matches[2]] : [$toResolve, null];
        if ($this->container && $this->container->has($class)) {
            if (!is_object($instance)) {
            }
        }
    }
    private function prepareToResolve($toResolve)
    {
    }
}
`

// TestParserRecovery_PHPDestructuring: the statement is analyzed, so it has
// no diagnostic, and every declaration and call around it is extracted.
func TestParserRecovery_PHPDestructuring(t *testing.T) {
	ex, err := NewProvider().Extract(context.Background(), source.FileID("CallableResolver.php"), []byte(destructuringRecovery))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range ex.Symbols {
		got = append(got, s.Qualified)
	}
	slices.Sort(got)
	want := []string{"CallableResolver", "CallableResolver.__construct", "CallableResolver.isMiddleware", "CallableResolver.prepareToResolve", "CallableResolver.resolveSlimNotation"}
	if !slices.Equal(got, want) {
		t.Errorf("symbols %v, want %v", got, want)
	}
	if len(ex.Diagnostics) != 0 {
		t.Errorf("diagnostics on valid source: %+v", ex.Diagnostics)
	}
	var calls []string
	for _, r := range ex.References {
		if r.Kind == "call" {
			calls = append(calls, fmt.Sprintf("%s L%d %s", r.Name, r.Location.Range.Start.Line, r.Container))
		}
	}
	slices.Sort(calls)
	if want := []string{"has L17 CallableResolver.resolveSlimNotation", "is_object L18 CallableResolver.resolveSlimNotation"}; !slices.Equal(calls, want) {
		t.Errorf("calls %v, want %v", calls, want)
	}
}
