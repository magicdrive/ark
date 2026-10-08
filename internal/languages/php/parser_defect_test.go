package php

import (
	"context"
	"slices"
	"testing"

	"github.com/magicdrive/ark/internal/source"
)

// knownDestructuringDefect is valid PHP (the reference Tree-sitter runtime
// with the same tree-sitter-php commit parses it without error), minimized
// by delta debugging from Slim's CallableResolver.php. gotreesitter rejects
// the destructuring assignment on line 16 on both of its parser routes, so
// that statement is not analyzed; nothing else is lost.
const knownDestructuringDefect = `<?php
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

// TestKnownParserDefect_PHPDestructuring pins the honest degradation: every
// declaration is extracted, and the only diagnostic is the rejected statement
// (or none, once the parser accepts it).
func TestKnownParserDefect_PHPDestructuring(t *testing.T) {
	ex, err := NewProvider().Extract(context.Background(), source.FileID("CallableResolver.php"), []byte(knownDestructuringDefect))
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
	for _, d := range ex.Diagnostics {
		if d.Code != "parse_error" || d.Location.Range.Start.Line != 16 {
			t.Errorf("unexpected diagnostic %+v", d)
		}
	}
}
