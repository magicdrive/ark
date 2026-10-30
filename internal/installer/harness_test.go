//go:build !windows

package installer

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// The hosts install.sh may connect to: github.com, and the host GitHub
// redirects release downloads to.
const (
	githubHost = "github.com"
	assetHost  = "release-assets.githubusercontent.com"
)

// project describes the releases of one tool, as GoReleaser publishes them.
type project struct {
	app, owner, repo string
	latest           string
	tags             []string
	// legacySums: tags whose checksum file is <app>_<version>_checksums.txt
	legacySums map[string]bool
	// noV: tags whose binary prints "<app> version 1.2.3" (no "v")
	noV map[string]bool
	// extra files in every archive, besides the binary
	files []string
}

// selfProject is ark, with fictitious releases of the shapes the tests need:
// an old one with the old checksum file name whose binary prints its version
// without "v" (v0.0.21), two older ones and the latest (v2.0.1). The archives
// have the files of the real ark releases.
var selfProject = project{
	app: "ark", owner: "magicdrive", repo: "ark", latest: "v2.0.1",
	tags:       []string{"v0.0.21", "v1.1.7", "v2.0.0", "v2.0.1"},
	legacySums: map[string]bool{"v0.0.21": true},
	noV:        map[string]bool{"v0.0.21": true},
	files:      []string{"completions/ark-completion.sh", "completions/bash/ark-completion.bash", "completions/fish/ark.fish", "completions/zsh/_ark"},
}

// The three projects install.sh was written for (it comes from
// https://github.com/magicdrive/enma), with the release layouts seen on their
// GitHub Releases pages: the lifecycle runs with them too.
var (
	enmaProject = project{
		app: "enma", owner: "magicdrive", repo: "enma", latest: "v2.0.1",
		tags:       []string{"v0.0.21", "v1.1.7", "v2.0.0", "v2.0.1"},
		legacySums: map[string]bool{"v0.0.21": true},
		noV:        map[string]bool{"v0.0.21": true},
		files: []string{"LICENSE", "README.md", "completions/enma-completion.sh",
			"completions/bash/enma-completion.bash", "completions/fish/enma.fish", "completions/zsh/_enma"},
	}
	arkProject = project{
		app: "ark", owner: "magicdrive", repo: "ark", latest: "v6.5.1",
		tags: []string{"v4.1.0", "v6.5.0", "v6.5.1"},
		files: []string{"completions/ark-completion.sh", "completions/bash/ark-completion.bash",
			"completions/fish/ark.fish", "completions/zsh/_ark"},
	}
	goregProject = project{
		app: "goreg", owner: "magicdrive", repo: "goreg", latest: "v1.3.10",
		tags:       []string{"v1.0.0", "v1.3.9", "v1.3.10"},
		legacySums: map[string]bool{"v1.0.0": true},
		noV:        map[string]bool{"v1.0.0": true},
		files: []string{"completions/bash/goreg-completion.bash", "completions/goreg-completion.sh",
			"completions/zsh/_goreg"},
	}
)

var platforms = [][2]string{{"darwin", "amd64"}, {"darwin", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"linux", "386"}}

// fakeBinary is the "binary" of a release: a shell script, so that it runs on
// any test machine. Its text names the platform, so the binaries differ.
func (p project) fakeBinary(tag, goos, goarch string) []byte {
	v := tag
	if p.noV[tag] {
		v = strings.TrimPrefix(tag, "v")
	}
	return []byte(fmt.Sprintf("#!/bin/sh\n# %s %s %s/%s\necho '%s version %s'\n", p.app, tag, goos, goarch, p.app, v))
}

func (p project) archiveName(tag, goos, goarch string) string {
	return fmt.Sprintf("%s_%s_%s_%s.tar.gz", p.app, strings.TrimPrefix(tag, "v"), goos, goarch)
}

func (p project) sumsName(tag string) string {
	if p.legacySums[tag] {
		return fmt.Sprintf("%s_%s_checksums.txt", p.app, strings.TrimPrefix(tag, "v"))
	}
	return p.app + "_checksums.txt"
}

// entry is one member of a test archive.
type entry struct {
	name     string
	typ      byte // tar.TypeReg, tar.TypeSymlink, ...
	body     []byte
	linkname string
	mode     int64
}

func makeTarGz(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Linkname: e.linkname, Mode: e.mode,
			ModTime: time.Unix(1700000000, 0), Format: tar.FormatPAX}
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// standardEntries is the archive GoReleaser builds: the extra files, then the
// binary at the top.
func (p project) standardEntries(tag, goos, goarch string) []entry {
	var es []entry
	for _, f := range p.files {
		es = append(es, entry{name: f, body: []byte("text of " + f + "\n")})
	}
	return append(es, entry{name: p.app, body: p.fakeBinary(tag, goos, goarch), mode: 0o755})
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// fault changes how the fake GitHub answers one request.
type fault struct {
	status   int    // answer with this status (and no redirect)
	body     []byte // with this body (status 200 when status is 0)
	location string // redirect here (for releases/latest)
	truncate bool   // send half of the file, then close the connection
	stall    bool   // send half of the file, then wait until the client goes
}

// fakeGitHub serves releases like github.com and its asset host.
type fakeGitHub struct {
	t        *testing.T
	mu       sync.Mutex
	assets   map[string][]byte // "/owner/repo/releases/download/tag/file" -> content
	latest   map[string]string // "/owner/repo" -> tag
	faults   map[string]fault  // same keys as assets, or "/owner/repo/releases/latest"
	requests []string          // "host path" of each request
	connects []string          // host:port of each proxy CONNECT

	server  *httptest.Server
	proxy   *httptest.Server
	plain   *httptest.Server // plain http
	caFile  string
	release chan struct{} // closed at cleanup: stalled handlers return
	conns   []net.Conn
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	g := &fakeGitHub{t: t, assets: map[string][]byte{}, latest: map[string]string{},
		faults: map[string]fault{}, release: make(chan struct{})}
	cert, caPEM := testCertificates(t)
	g.caFile = filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(g.caFile, caPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	g.server = httptest.NewUnstartedServer(http.HandlerFunc(g.serve))
	g.server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	g.server.StartTLS()
	g.proxy = httptest.NewServer(http.HandlerFunc(g.connect))
	// the same files over plain http, for the redirect tests
	g.plain = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.requests = append(g.requests, "plain "+r.URL.Path)
		body, ok := g.assets[r.URL.Path]
		g.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body) // the client's failures are the tests' to see
	}))
	t.Cleanup(func() {
		close(g.release)
		g.mu.Lock()
		for _, c := range g.conns {
			_ = c.Close()
		}
		g.mu.Unlock()
		g.proxy.Close()
		g.plain.Close()
		g.server.Close()
	})
	return g
}

// testCertificates makes a CA and a certificate for the two GitHub hosts.
func testCertificates(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ark installer test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, BasicConstraintsValid: true}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: githubHost},
		DNSNames: []string{githubHost, assetHost}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: key},
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
}

// connect is the HTTPS proxy: it tunnels the two GitHub hosts to the fake
// server and refuses anything else.
func (g *fakeGitHub) connect(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.connects = append(g.connects, r.Host)
	g.mu.Unlock()
	if r.Method != http.MethodConnect || (r.Host != githubHost+":443" && r.Host != assetHost+":443") {
		http.Error(w, "this proxy only reaches the fake GitHub", http.StatusForbidden)
		return
	}
	dst, err := net.Dial("tcp", g.server.Listener.Addr().String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	src, buf, err := w.(http.Hijacker).Hijack()
	if err != nil {
		_ = dst.Close()
		return
	}
	g.mu.Lock()
	g.conns = append(g.conns, src, dst)
	g.mu.Unlock()
	if _, err := src.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); err != nil {
		_ = src.Close()
		_ = dst.Close()
		return
	}
	go func() {
		_, _ = io.Copy(dst, bufio.NewReader(buf)) // a tunnel ends with an error when either side closes
		_ = dst.Close()
	}()
	_, _ = io.Copy(src, dst)
	_ = src.Close()
}

func (g *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.requests = append(g.requests, r.Host+" "+r.URL.Path)
	g.mu.Unlock()
	switch r.Host {
	case githubHost:
		g.serveGitHub(w, r)
	case assetHost:
		g.serveAsset(w, r)
	default:
		http.Error(w, "unknown host", http.StatusMisdirectedRequest)
	}
}

func (g *fakeGitHub) serveGitHub(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	g.mu.Lock()
	f, hasFault := g.faults[path]
	_, hasAsset := g.assets[path]
	g.mu.Unlock()
	if strings.HasSuffix(path, "/releases/latest") {
		repo := strings.TrimSuffix(path, "/releases/latest")
		g.mu.Lock()
		tag, ok := g.latest[repo]
		g.mu.Unlock()
		switch {
		case hasFault && f.location != "":
			w.Header().Set("Location", f.location)
			w.WriteHeader(http.StatusFound)
		case hasFault:
			g.answer(w, f, f.body)
		case ok:
			w.Header().Set("Location", "https://"+githubHost+repo+"/releases/tag/"+tag)
			w.WriteHeader(http.StatusFound)
		default: // a repository without releases
			w.Header().Set("Location", "https://"+githubHost+repo+"/releases")
			w.WriteHeader(http.StatusFound)
		}
		return
	}
	if !strings.Contains(path, "/releases/download/") || !hasAsset {
		http.NotFound(w, r)
		return
	}
	if hasFault && f.status != 0 {
		g.answer(w, f, f.body)
		return
	}
	if hasFault && f.location != "" {
		w.Header().Set("Location", f.location)
		w.WriteHeader(http.StatusFound)
		return
	}
	// GitHub sends release downloads to another host.
	w.Header().Set("Location", "https://"+assetHost+path)
	w.WriteHeader(http.StatusFound)
}

func (g *fakeGitHub) serveAsset(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	body, ok := g.assets[r.URL.Path]
	f := g.faults[r.URL.Path]
	g.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if f.body != nil {
		body = f.body
	}
	g.answer(w, f, body)
}

func (g *fakeGitHub) answer(w http.ResponseWriter, f fault, body []byte) {
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	if f.truncate || f.stall {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.WriteHeader(status)
		_, _ = w.Write(body[:len(body)/2])
		w.(http.Flusher).Flush()
		if f.stall {
			<-g.release // the client gives up first (INSTALLER_TIMEOUT)
		}
		// returning without the rest: the connection is closed mid-body
		panic(http.ErrAbortHandler)
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// publish adds the releases of p, as GoReleaser makes them.
func (g *fakeGitHub) publish(t *testing.T, p project) {
	t.Helper()
	for _, tag := range p.tags {
		var sums strings.Builder
		for _, pl := range platforms {
			name := p.archiveName(tag, pl[0], pl[1])
			data := makeTarGz(t, p.standardEntries(tag, pl[0], pl[1]))
			g.setAsset(p, tag, name, data)
			fmt.Fprintf(&sums, "%s  %s\n", sha(data), name)
		}
		g.setAsset(p, tag, p.sumsName(tag), []byte(sums.String()))
	}
	g.mu.Lock()
	g.latest["/"+p.owner+"/"+p.repo] = p.latest
	g.mu.Unlock()
}

func (g *fakeGitHub) assetPath(p project, tag, name string) string {
	return "/" + p.owner + "/" + p.repo + "/releases/download/" + tag + "/" + name
}

func (g *fakeGitHub) setAsset(p project, tag, name string, data []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.assets[g.assetPath(p, tag, name)] = data
}

func (g *fakeGitHub) asset(p project, tag, name string) []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.assets[g.assetPath(p, tag, name)]
}

func (g *fakeGitHub) removeAsset(p project, tag, name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.assets, g.assetPath(p, tag, name))
}

func (g *fakeGitHub) setFault(path string, f fault) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.faults[path] = f
}

// replaceArchive publishes entries as the archive of tag for the platform, and
// lists its real checksum (an attacker who controls the release can do both).
func (g *fakeGitHub) replaceArchive(t *testing.T, p project, tag, goos, goarch string, data []byte) {
	t.Helper()
	name := p.archiveName(tag, goos, goarch)
	g.setAsset(p, tag, name, data)
	sumsName := p.sumsName(tag)
	var out []string
	for _, line := range strings.Split(strings.TrimSuffix(string(g.asset(p, tag, sumsName)), "\n"), "\n") {
		if strings.HasSuffix(line, "  "+name) {
			line = sha(data) + "  " + name
		}
		out = append(out, line)
	}
	g.setAsset(p, tag, sumsName, []byte(strings.Join(out, "\n")+"\n"))
}

func (g *fakeGitHub) snapshot() (requests, connects []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.requests), slices.Clone(g.connects)
}

// --- the environment install.sh runs in --------------------------------------------

// system tools install.sh uses; looked up in these directories first
var toolDirs = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}

var scriptTools = []string{"mktemp", "gzip", "tar", "awk", "tr", "ls", "find", "cat", "chmod", "mkdir", "mv", "rm", "id"}

// lookTool finds a tool in the system directories, then in PATH.
func lookTool(name string) (string, bool) {
	for _, d := range toolDirs {
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, true
		}
	}
	p, err := exec.LookPath(name)
	return p, err == nil
}

// shellSpec is a shell to run install.sh with: argv before the script.
type shellSpec struct {
	name string
	argv []string
	// appletsFirst: the shell runs its own built-in commands (applets) before
	// those in PATH, as a BusyBox built with its "standalone shell" does (the
	// busybox of the GitHub Ubuntu runners). In it, the fake uname and the
	// PATH's wget of a test are not what install.sh runs. Measured with
	// probeAppletsFirst, not guessed from the name.
	appletsFirst bool
}

// probeAppletsFirst runs uname with argv while PATH starts with a fake uname:
// a shell that does not run the fake one runs its own commands first.
func probeAppletsFirst(t *testing.T, argv []string) bool {
	t.Helper()
	key := strings.Join(argv, "\x00")
	probeMu.Lock()
	defer probeMu.Unlock()
	if v, ok := probed[key]; ok {
		return v
	}
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "uname"), []byte("#!/bin/sh\necho fake-uname\n"))
	cmd := exec.Command(argv[0], append(slices.Clone(argv[1:]), "-c", "uname")...)
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v -c uname: %v", argv, err)
	}
	probed[key] = strings.TrimSpace(string(out)) != "fake-uname"
	return probed[key]
}

// The shells do not change while the tests run: each is probed once.
var (
	probeMu sync.Mutex
	probed  = map[string]bool{}
	shellMu sync.Mutex
	shells  []shellSpec // the result of testShells, once it succeeded
)

// pathShell is the first test shell that runs the commands of PATH: the tests
// that put a fake tool (uname, mv, ...) in PATH need one.
func pathShell(t *testing.T) shellSpec {
	t.Helper()
	for _, sh := range testShells(t) {
		if !sh.appletsFirst {
			return sh
		}
	}
	t.Fatal("no shell of ARK_INSTALLER_SHELLS runs the commands of PATH")
	return shellSpec{}
}

// hostUname is what uname -s and -m print here.
func hostUname(t *testing.T) [2]string {
	t.Helper()
	var u [2]string
	for i, f := range []string{"-s", "-m"} {
		out, err := exec.Command("uname", f).Output()
		if err != nil {
			t.Fatal(err)
		}
		u[i] = strings.TrimSpace(string(out))
	}
	return u
}

// toolset is how the PATH of a run is built.
type toolset struct {
	downloader string // "curl" or "wget"
	shaTool    string // "sha256sum", "shasum" or "openssl"
	busybox    string // when set, the POSIX tools are applets of this busybox
	uname      [2]string
	sysctl     string            // what `sysctl -n sysctl.proc_translated` prints; "" = no sysctl
	brewPrefix string            // when set, a fake brew prints it for `brew --prefix`
	fakes      map[string]string // name -> shell script body, replaces the tool
	omit       []string          // tools left out of PATH
	real       bool              // the real uname and sysctl (for the real releases)
}

// env is one isolated place to run install.sh: its own HOME, TMPDIR, PATH.
type env struct {
	t      *testing.T
	gh     *fakeGitHub
	p      project
	script string
	shell  shellSpec
	tools  toolset
	home   string
	tmp    string
	bin    string
	vars   []string // more environment variables
	path   []string // more PATH entries (after the tools)
}

func newEnv(t *testing.T, gh *fakeGitHub, p project, sh shellSpec, ts toolset) *env {
	t.Helper()
	// the physical path: install.sh reports the directory with links resolved
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if sh.appletsFirst && !ts.real {
		// install.sh runs the shell's own uname: expect this machine's platform
		if ts.uname != ([2]string{}) || ts.sysctl != "" || len(ts.fakes) > 0 || ts.brewPrefix != "" {
			t.Fatalf("shell %s runs its own commands, not the fake tools of this test: use pathShell", sh.name)
		}
		ts.uname = hostUname(t)
	}
	e := &env{t: t, gh: gh, p: p, script: scriptFor(t, p), shell: sh, tools: ts,
		home: filepath.Join(root, "home"), tmp: filepath.Join(root, "tmp"), bin: filepath.Join(root, "tools")}
	for _, d := range []string{e.home, e.tmp, e.bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.buildTools()
	return e
}

func (e *env) buildTools() {
	t := e.t
	ts := e.tools
	if ts.downloader == "" {
		ts.downloader = "curl"
	}
	if ts.shaTool == "" {
		ts.shaTool = defaultShaTool()
	}
	if ts.uname == [2]string{} {
		ts.uname = [2]string{"Linux", "x86_64"}
	}
	link := func(name, target string) {
		if slices.Contains(ts.omit, name) {
			return
		}
		if err := os.Symlink(target, filepath.Join(e.bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range append(slices.Clone(scriptTools), ts.downloader, ts.shaTool) {
		if _, fake := ts.fakes[name]; fake {
			continue
		}
		if ts.busybox != "" && slices.Contains(append(slices.Clone(scriptTools), "sha256sum"), name) {
			link(name, ts.busybox)
			continue
		}
		p, ok := lookTool(name)
		if !ok {
			t.Fatalf("the tests need %s on this machine", name)
		}
		link(name, p)
	}
	if ts.shaTool == "shasum" {
		// shasum is a Perl script on macOS and Linux
		if p, ok := lookTool("perl"); ok {
			link("perl", p)
		}
	}
	write := func(name, body string) {
		writeExecutable(t, filepath.Join(e.bin, name), []byte("#!/bin/sh\n"+body+"\n"))
	}
	if ts.real {
		for _, n := range []string{"uname", "sysctl"} {
			if p, ok := lookTool(n); ok {
				link(n, p)
			}
		}
		e.tools = ts
		return
	}
	write("uname", fmt.Sprintf(`case "$1" in -s) echo %s ;; -m) echo %s ;; *) exit 1 ;; esac`, ts.uname[0], ts.uname[1]))
	if ts.sysctl != "" {
		write("sysctl", fmt.Sprintf(`[ "$*" = "-n sysctl.proc_translated" ] && echo %s`, ts.sysctl))
	}
	if ts.brewPrefix != "" {
		write("brew", fmt.Sprintf(`[ "$1" = --prefix ] && echo '%s'`, ts.brewPrefix))
	}
	for name, body := range ts.fakes {
		write(name, body)
	}
	e.tools = ts
}

// buildFake writes the fake tool name of e.tools.fakes into the tools.
func (e *env) buildFake(name string) {
	e.t.Helper()
	body := e.tools.fakes[name]
	writeExecutable(e.t, filepath.Join(e.bin, name), []byte("#!/bin/sh\n"+body+"\n"))
}

func defaultShaTool() string {
	for _, n := range []string{"sha256sum", "shasum", "openssl"} {
		if _, ok := lookTool(n); ok {
			return n
		}
	}
	return "sha256sum"
}

// writeExecutable writes a file that a test runs, with mode 0755. A child
// process writes it: a file this process had open for writing could still be
// open in a child another parallel test is forking at that moment, and
// running it would fail with ETXTBSY ("text file busy") on Linux.
func writeExecutable(t *testing.T, path string, data []byte) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", `cat >"$1" && chmod 755 "$1"`, "sh", path)
	cmd.Stdin = bytes.NewReader(data)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("writing %s: %v %s", path, err, out)
	}
}

// scriptFor writes install.sh configured for p: only the two lines of the
// project configuration differ from the file in the repository.
func scriptFor(t *testing.T, p project) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return writeScript(t, configure(t, string(src), p.app, "https://github.com/"+p.owner+"/"+p.repo))
}

const (
	appLine = `APP_NAME="ark"` + "\n"
	urlLine = `GITHUB_URL="https://github.com/magicdrive/ark"` + "\n"
)

// configure replaces the two configuration lines of install.sh.
func configure(t *testing.T, src, app, url string) string {
	t.Helper()
	if strings.Count(src, appLine) != 1 || strings.Count(src, urlLine) != 1 {
		t.Fatalf("install.sh must have exactly one %q and one %q", appLine, urlLine)
	}
	src = strings.Replace(src, appLine, "APP_NAME=\""+app+"\"\n", 1)
	return strings.Replace(src, urlLine, "GITHUB_URL=\""+url+"\"\n", 1)
}

func writeScript(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

type result struct {
	code           int
	stdout, stderr string
}

func (r result) String() string {
	return fmt.Sprintf("exit %d\n--- stdout\n%s--- stderr\n%s", r.code, r.stdout, r.stderr)
}

func (e *env) environ() []string {
	wgetrc := filepath.Join(e.home, "test.wgetrc")
	if err := os.WriteFile(wgetrc, []byte("ca_certificate = "+e.gh.caFile+"\n"), 0o644); err != nil {
		e.t.Fatal(err)
	}
	path := strings.Join(append([]string{e.bin}, e.path...), ":")
	vars := []string{
		"PATH=" + path, "HOME=" + e.home, "TMPDIR=" + e.tmp,
		"https_proxy=" + e.gh.proxy.URL, "HTTPS_PROXY=" + e.gh.proxy.URL,
		"CURL_CA_BUNDLE=" + e.gh.caFile, "SSL_CERT_FILE=" + e.gh.caFile, "WGETRC=" + wgetrc,
	}
	return append(vars, e.vars...)
}

// run runs install.sh with args and waits for it (at most a minute).
func (e *env) run(args ...string) result {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := e.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			e.t.Fatalf("running install.sh: %v", err)
		}
		code = ee.ExitCode() // -1 when a signal ended it
	}
	if ctx.Err() != nil {
		e.t.Fatalf("install.sh did not finish in a minute\n%s%s", stdout.String(), stderr.String())
	}
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func (e *env) command(ctx context.Context, args ...string) *exec.Cmd {
	argv := append(slices.Clone(e.shell.argv[1:]), e.script)
	cmd := exec.CommandContext(ctx, e.shell.argv[0], append(argv, args...)...)
	cmd.Env = e.environ()
	cmd.Dir = e.home
	return cmd
}

// shellOutput runs a command line with the shell of e, in its environment.
func (e *env) shellOutput(line string) string {
	e.t.Helper()
	cmd := exec.Command(e.shell.argv[0], append(slices.Clone(e.shell.argv[1:]), "-c", line)...)
	cmd.Env = e.environ()
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// defaultDir is where install.sh installs without --dir.
func (e *env) defaultDir() string { return filepath.Join(e.home, ".local", "bin") }

// wantBinary is the binary install.sh must put in place for tag.
func (e *env) wantBinary(tag string) []byte {
	return e.p.fakeBinary(tag, e.goos(), e.goarch())
}

func (e *env) goos() string {
	if e.tools.uname[0] == "Darwin" {
		return "darwin"
	}
	return "linux"
}

func (e *env) goarch() string {
	switch e.tools.uname[1] {
	case "aarch64", "arm64":
		return "arm64"
	}
	if e.tools.uname[0] == "Darwin" && e.tools.sysctl == "1" {
		return "arm64"
	}
	return "amd64"
}

// install puts tag's binary into dir as a previous installation would have.
func (e *env) install(dir, tag string) string {
	e.t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	p := filepath.Join(dir, e.p.app)
	writeExecutable(e.t, p, e.wantBinary(tag))
	return p
}

// fileState is what a test compares before and after a run.
type fileState struct {
	exists bool
	data   []byte
	mode   os.FileMode
	info   os.FileInfo
}

func stateOf(t *testing.T, path string) fileState {
	t.Helper()
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileState{}
	}
	if err != nil {
		t.Fatal(err)
	}
	s := fileState{exists: true, mode: fi.Mode(), info: fi}
	if fi.Mode().IsRegular() {
		if s.data, err = os.ReadFile(path); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// checkInstalled: path holds tag's binary, a regular executable file.
func (e *env) checkInstalled(r result, path, tag string) {
	e.t.Helper()
	s := stateOf(e.t, path)
	if !s.exists || !s.mode.IsRegular() {
		e.t.Fatalf("%s is not a regular file after the run\n%s", path, r)
	}
	if !bytes.Equal(s.data, e.wantBinary(tag)) {
		e.t.Fatalf("%s is not the %s binary for %s/%s:\n%s\n%s", path, tag, e.goos(), e.goarch(), s.data, r)
	}
	if s.mode.Perm() != 0o755 {
		e.t.Fatalf("%s has mode %v, want 0755\n%s", path, s.mode.Perm(), r)
	}
}

// checkUnchanged: path is exactly as before (same file, same content).
func (e *env) checkUnchanged(r result, path string, before fileState) {
	e.t.Helper()
	after := stateOf(e.t, path)
	if after.exists != before.exists || !bytes.Equal(after.data, before.data) || after.mode != before.mode ||
		(before.exists && !os.SameFile(before.info, after.info)) {
		e.t.Fatalf("%s changed (before: exists=%v %q, after: exists=%v %q)\n%s",
			path, before.exists, before.data, after.exists, after.data, r)
	}
}

// checkClean: no temporary file is left in TMPDIR or in dir.
func (e *env) checkClean(r result, dirs ...string) {
	e.t.Helper()
	left, err := os.ReadDir(e.tmp)
	if err != nil {
		e.t.Fatal(err)
	}
	if len(left) != 0 {
		e.t.Fatalf("TMPDIR is not empty after the run: %v\n%s", names(left), r)
	}
	for _, d := range dirs {
		es, err := os.ReadDir(d)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			e.t.Fatal(err)
		}
		for _, f := range es {
			if strings.HasPrefix(f.Name(), "."+e.p.app+".install.") {
				e.t.Fatalf("temporary file %s left in %s\n%s", f.Name(), d, r)
			}
		}
	}
}

func names(es []os.DirEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// checkFails: the run failed with exit status code, and stderr says want.
func checkFails(t *testing.T, r result, code int, want string) {
	t.Helper()
	if r.code != code {
		t.Fatalf("want exit status %d\n%s", code, r)
	}
	if !strings.Contains(r.stderr, want) {
		t.Fatalf("stderr does not say %q\n%s", want, r)
	}
}

func checkOK(t *testing.T, r result, want string) {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("want exit status 0\n%s", r)
	}
	if !strings.Contains(r.stdout, want) {
		t.Fatalf("stdout does not say %q\n%s", want, r)
	}
}

// --- shells -----------------------------------------------------------------------------

// testShells are the shells install.sh is run with. ARK_INSTALLER_SHELLS
// (e.g. "sh dash bash bash-nonposix ksh busybox") names the ones that must be
// tested: a missing one fails the tests. Without it, sh and those of dash,
// bash (with --posix, and without: "bash-nonposix"), ksh and busybox found
// here are used.
func testShells(t *testing.T) []shellSpec {
	t.Helper()
	shellMu.Lock()
	defer shellMu.Unlock()
	if shells != nil {
		return shells
	}
	want := strings.Fields(os.Getenv("ARK_INSTALLER_SHELLS"))
	required := len(want) > 0
	if !required {
		want = []string{"sh", "dash", "bash", "bash-nonposix", "ksh", "busybox"}
	}
	var out []shellSpec
	for _, n := range want {
		p, ok := lookTool(strings.TrimSuffix(n, "-nonposix"))
		if !ok {
			if required || n == "sh" {
				t.Fatalf("shell %s (ARK_INSTALLER_SHELLS) is not installed", n)
			}
			continue
		}
		argv := []string{p}
		switch n {
		case "busybox":
			argv = append(argv, "sh")
		case "bash":
			argv = append(argv, "--posix")
		}
		// The shell must run a POSIX script; check it rather than trust its name.
		out2, err := exec.Command(argv[0], append(slices.Clone(argv[1:]), "-c", `x=a.b.c; echo "${x%%.*}${x#*.}"`)...).Output()
		if err != nil || strings.TrimSpace(string(out2)) != "ab.c" {
			t.Fatalf("shell %s (%s) does not run POSIX sh: %v %q", n, p, err, out2)
		}
		out = append(out, shellSpec{name: n, argv: argv, appletsFirst: probeAppletsFirst(t, argv)})
	}
	shells = out
	return out
}

// busyboxPath is the busybox for the BusyBox userland tests: ARK_INSTALLER_BUSYBOX,
// or one in PATH. ARK_INSTALLER_BUSYBOX=required fails the tests without one.
func busyboxPath(t *testing.T) (string, bool) {
	t.Helper()
	v := os.Getenv("ARK_INSTALLER_BUSYBOX")
	if v != "" && v != "required" {
		return v, true
	}
	p, ok := lookTool("busybox")
	if !ok && v == "required" {
		t.Fatal("ARK_INSTALLER_BUSYBOX=required, but busybox is not installed")
	}
	return p, ok
}

// downloaders: ARK_INSTALLER_DOWNLOADERS ("curl wget") names the ones that
// must be tested; without it, curl, and GNU Wget when installed.
func downloaders(t *testing.T) []string {
	t.Helper()
	want := strings.Fields(os.Getenv("ARK_INSTALLER_DOWNLOADERS"))
	required := len(want) > 0
	if !required {
		want = []string{"curl", "wget"}
	}
	var out []string
	for _, n := range want {
		p, ok := lookTool(n)
		if ok && n == "wget" {
			v, _ := exec.Command(p, "--version").Output()
			ok = strings.HasPrefix(string(v), "GNU Wget 1.")
		}
		if !ok {
			if required || n == "curl" {
				t.Fatalf("downloader %s (ARK_INSTALLER_DOWNLOADERS) is not installed", n)
			}
			continue
		}
		out = append(out, n)
	}
	return out
}

// must fails the test on a failed set-up step.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// isRoot: root ignores file permissions, so the permission tests do not apply.
func isRoot() bool { return os.Geteuid() == 0 }

// sortedKeys is for deterministic subtests.
func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
