package index

import (
	"context"
	"runtime"
	"sync"

	"github.com/magicdrive/ark/internal/cache"
	"github.com/magicdrive/ark/internal/language"
	"github.com/magicdrive/ark/internal/source"
)

// Parallel extraction for an index build.
//
// A build's walk reads every source in order; extracting a file (the cache
// lookup, the provider's parse, the cache store) depends only on the file's
// repository-relative name and bytes, and providers are safe for concurrent
// use (concurrent requests already extract concurrently). So the walk hands
// each file to a pool of workers and goes on, and the results are taken
// back strictly in walk order: diagnostics, ingestion and statistics happen
// in the order, and with the values, of a sequential build — the index and
// its fingerprint do not depend on scheduling. A worker's panic is carried
// back and raised again where the sequential build would have raised it,
// on the building goroutine.

// extractBudget bounds the source bytes being extracted at once. A parse's
// memory grows with the file (the parser's node arena), so files whose
// extraction is in flight together are bounded by their size: many small
// files run in parallel, large ones (nearly) one at a time — a build of
// large files peaks near the sequential build's memory. A file larger than
// the budget runs alone.
var extractBudget int64 = 256 << 10

// extractWorkers is the size of a build's worker pool. Each worker holds a
// parser's memory while it extracts, so more workers trade the build's peak
// memory for its time: measured on a repository of small Go files, four
// workers took a third of the sequential time at about 1.6 times its peak
// resident memory, eight a quarter at about twice.
func extractWorkers() int {
	n := runtime.GOMAXPROCS(0)
	if n > 4 {
		n = 4
	}
	return n
}

// extractJob is one source in walk order and, once done is closed, its
// extraction.
type extractJob struct {
	relPath string
	prov    language.Provider
	src     []byte
	readErr error

	done   chan struct{}
	cached *cache.CachedExtraction // a cache hit
	ext    language.Extraction     // a miss, extracted
	err    error                   // the provider's error
	panic  any                     // the provider's panic, raised again in order
}

type extractor struct {
	ctx    context.Context
	store  cache.Store
	work   chan *extractJob
	queue  []*extractJob // in walk order, not yet ingested
	quit   chan struct{} // closed when every worker has stopped
	closed bool          // work is closed

	mu       sync.Mutex
	freed    *sync.Cond
	inflight int64 // source bytes handed to workers, not yet extracted
}

func newExtractor(ctx context.Context, store cache.Store, workers int) *extractor {
	x := &extractor{ctx: ctx, store: store, work: make(chan *extractJob, 2*workers), quit: make(chan struct{})}
	x.freed = sync.NewCond(&x.mu)
	done := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := range x.work {
				x.extract(j)
			}
		}()
	}
	go func() {
		for i := 0; i < workers; i++ {
			<-done
		}
		close(x.quit)
	}()
	return x
}

// add queues a source the walk read (or failed to read).
func (x *extractor) add(relPath string, prov language.Provider, src []byte, readErr error) {
	j := &extractJob{relPath: relPath, prov: prov, src: src, readErr: readErr, done: make(chan struct{})}
	x.queue = append(x.queue, j)
	if readErr != nil {
		close(j.done)
		return
	}
	n := int64(len(src))
	x.mu.Lock()
	for x.inflight > 0 && x.inflight+n > extractBudget {
		x.freed.Wait()
	}
	x.inflight += n
	x.mu.Unlock()
	x.work <- j
}

func (x *extractor) extract(j *extractJob) {
	n := int64(len(j.src))
	defer func() {
		x.mu.Lock()
		x.inflight -= n
		x.mu.Unlock()
		x.freed.Broadcast()
	}()
	defer close(j.done)
	defer func() {
		if r := recover(); r != nil {
			j.panic = r
		}
	}()
	key := cache.NewCacheKey(j.relPath, j.src, ArkVersion, j.prov.CacheVersion())
	if cached, hit, _ := x.store.Get(key); hit {
		j.cached = cached
		j.src = nil
		return
	}
	ext, err := j.prov.Extract(x.ctx, source.FileID(j.relPath), j.src)
	j.src = nil
	if err != nil {
		j.err = err
		return
	}
	j.ext = ext
	// Persist to cache (best-effort; errors are ignored).
	_ = x.store.Put(&cache.CachedExtraction{
		Key:          key,
		Language:     string(j.prov.Language()),
		Symbols:      ext.Symbols,
		References:   ext.References,
		Imports:      ext.Imports,
		Diagnostics:  ext.Diagnostics,
		Bindings:     ext.Bindings,
		Exports:      ext.Exports,
		ModuleScoped: ext.ModuleScoped,
		IdentityOnly: ext.IdentityOnly,

		Package:           ext.Package,
		PackageScoped:     ext.PackageScoped,
		MembersShareNames: ext.MembersShareNames,
		LexicalNesting:    ext.LexicalNesting,
		Aliases:           ext.Aliases,
		TypeFacts:         ext.TypeFacts,
		SourceRoot:        ext.SourceRoot,
	})
}

// drain ingests the queued sources whose extraction is done, in walk order,
// stopping at the first one still running — or, when all is set (the walk
// has ended), waiting for every one and for the workers to stop.
func (x *extractor) drain(b *builder, all bool) {
	if all {
		x.stop()
	}
	for len(x.queue) > 0 {
		j := x.queue[0]
		if !all {
			select {
			case <-j.done:
			default:
				return
			}
		}
		<-j.done
		x.queue[0] = nil
		x.queue = x.queue[1:]
		x.ingest(b, j)
	}
}

// stop closes the work queue and waits for the workers to finish what they
// hold.
func (x *extractor) stop() {
	if !x.closed {
		x.closed = true
		close(x.work)
	}
	<-x.quit
}

// ingest is what the sequential build did with one source.
func (x *extractor) ingest(b *builder, j *extractJob) {
	if j.panic != nil {
		// Stop the workers (none is left running or blocked), then raise
		// it as the sequential build would have.
		x.stop()
		x.queue = nil
		panic(j.panic)
	}
	if j.readErr != nil {
		b.addDiagnostic(fileFailure(j.relPath, language.DiagReadError, "read error", j.readErr))
		b.stats.Skipped++
		return
	}
	fileID := source.FileID(j.relPath)
	if cached := j.cached; cached != nil {
		// A hit must yield the index a miss would: diagnostics included.
		b.addDiagnostics(cached.Diagnostics)
		b.ingestExtraction(fileID, cached.Language, language.Extraction{
			Symbols:      cached.Symbols,
			References:   cached.References,
			Imports:      cached.Imports,
			Diagnostics:  cached.Diagnostics,
			Bindings:     cached.Bindings,
			Exports:      cached.Exports,
			ModuleScoped: cached.ModuleScoped,
			IdentityOnly: cached.IdentityOnly,

			Package:           cached.Package,
			PackageScoped:     cached.PackageScoped,
			MembersShareNames: cached.MembersShareNames,
			LexicalNesting:    cached.LexicalNesting,
			Aliases:           cached.Aliases,
			TypeFacts:         cached.TypeFacts,
			SourceRoot:        cached.SourceRoot,
		})
		return
	}
	if j.err != nil {
		b.addDiagnostic(fileFailure(j.relPath, language.DiagExtractionError, "extraction error", j.err))
		b.stats.Skipped++
		return
	}
	b.addDiagnostics(j.ext.Diagnostics)
	b.ingestExtraction(fileID, string(j.prov.Language()), j.ext)
}
