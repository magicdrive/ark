package php_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	ctxengine "github.com/magicdrive/ark/internal/context"
	"github.com/magicdrive/ark/internal/index"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/languages/php"
	"github.com/magicdrive/ark/internal/source"
)

func benchCorpusDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", "corpus")
}

var benchSource = []byte(`<?php
namespace App\Service;

use App\Domain\User;
use App\Repository\UserRepository;

class UserService
{
    public function __construct(
        private readonly UserRepository $repository,
    ) {}

    public function find(int $id): ?User
    {
        $u = $this->repository->find($id);
        return $u;
    }

    public static function create(): self
    {
        return new self(new DbUserRepository());
    }
}
`)

// BenchmarkPHPExtract measures single-file parse+extract throughput.
func BenchmarkPHPExtract(b *testing.B) {
	p := php.NewProvider()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := p.Extract(context.Background(), source.FileID("bench.php"), benchSource); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPHPIndex measures repository index build (parse+extract+resolve+graph)
// over the realistic medium corpus.
func BenchmarkPHPIndex(b *testing.B) {
	dir := benchCorpusDir()
	providers := []language.Provider{php.NewProvider()}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := index.New(context.Background(), dir, providers); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPHPContext measures context-query latency over the corpus.
func BenchmarkPHPContext(b *testing.B) {
	dir := benchCorpusDir()
	idx, err := index.New(context.Background(), dir, []language.Provider{php.NewProvider()})
	if err != nil {
		b.Fatal(err)
	}
	targets := idx.FindSymbolsByQualified("App\\Service\\UserService")
	if len(targets) == 0 {
		b.Skip("target not found")
	}
	eng := ctxengine.New(idx, dir)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Build(context.Background(), ctxengine.Request{Target: targets[0].ID, MaxTokens: 8000, MaxDepth: 2}); err != nil {
			b.Fatal(err)
		}
	}
}
