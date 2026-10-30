//go:build !windows

package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var projects = []project{selfProject, enmaProject, arkProject, goregProject}

// lifecycle runs the whole life of an installation: install the latest
// release, run again, downgrade, upgrade, install a release with the old
// checksum file name, fail on a missing release. After each step the
// installed file and the temporary files are checked.
func lifecycle(t *testing.T, e *env) {
	t.Helper()
	p := e.p
	dir := e.defaultDir()
	target := filepath.Join(dir, p.app)
	oldest, middle := p.tags[0], p.tags[len(p.tags)-2]

	// 1. the latest release, into the default directory (created)
	r := e.run()
	checkOK(t, r, "Installed "+p.app+" "+p.latest+" to "+target)
	e.checkInstalled(r, target, p.latest)
	e.checkClean(r, dir)
	if !strings.Contains(r.stdout, "is not in PATH") {
		t.Fatalf("no PATH hint\n%s", r)
	}

	// 2. the same again: nothing changes, not even the file
	before := stateOf(t, target)
	r = e.run()
	checkOK(t, r, "already installed")
	e.checkUnchanged(r, target, before)
	e.checkClean(r, dir)

	// 3. a downgrade to an older release (v accepted or not)
	r = e.run("--version", strings.TrimPrefix(middle, "v"))
	checkOK(t, r, "Downgraded "+p.app+" "+p.latest+" to "+middle)
	e.checkInstalled(r, target, middle)

	// 4. --upgrade: to the latest
	r = e.run("--upgrade")
	checkOK(t, r, "Upgraded "+p.app+" "+middle+" to "+p.latest)
	e.checkInstalled(r, target, p.latest)

	// 5. --upgrade --version: to a given (older) one
	r = e.run("--upgrade", "--version="+oldest)
	checkOK(t, r, "Downgraded "+p.app+" "+p.latest+" to "+oldest)
	e.checkInstalled(r, target, oldest)
	e.checkClean(r, dir)

	// 6. a release that does not exist: fails, keeps the installed one
	before = stateOf(t, target)
	r = e.run("--version", "v9.9.9")
	checkFails(t, r, 1, "release v9.9.9 not found")
	e.checkUnchanged(r, target, before)
	e.checkClean(r, dir)

	// 7. upgrade from the oldest straight to a given one, with --dir
	r = e.run("--upgrade", "--version", middle, "--dir", dir)
	checkOK(t, r, "Upgraded "+p.app+" "+oldest+" to "+middle)
	e.checkInstalled(r, target, middle)
	e.checkClean(r, dir)

	reqs, connects := e.gh.snapshot()
	for _, c := range connects {
		if c != githubHost+":443" && c != assetHost+":443" {
			t.Fatalf("install.sh connected to %s (only %s and %s are expected)", c, githubHost, assetHost)
		}
	}
	if len(reqs) == 0 {
		t.Fatal("no request reached the fake GitHub")
	}
}

// TestLifecycle runs the lifecycle for each project (install.sh with only its
// two configuration lines changed), shell and downloader.
func TestLifecycle(t *testing.T) {
	shells := testShells(t)
	dls := downloaders(t)
	for _, p := range projects {
		for _, sh := range shells {
			for _, dl := range dls {
				t.Run(p.app+"/"+sh.name+"/"+dl, func(t *testing.T) {
					t.Parallel()
					gh := newFakeGitHub(t)
					gh.publish(t, p)
					e := newEnv(t, gh, p, sh, toolset{downloader: dl})
					if dl == "wget" && !strings.HasPrefix(e.shellOutput("wget --version"), "GNU Wget 1.") {
						// The shell runs its own wget (BusyBox's), not GNU Wget
						// from PATH: install.sh must refuse it, before connecting.
						r := e.run()
						checkFails(t, r, 1, "is not GNU Wget 1.x")
						if _, c := gh.snapshot(); len(c) != 0 {
							t.Fatalf("it connected with the wget of %s: %v", sh.name, c)
						}
						if _, err := os.Stat(e.defaultDir()); err == nil {
							t.Fatalf("it created %s", e.defaultDir())
						}
						return
					}
					lifecycle(t, e)
				})
			}
		}
	}
}

// TestBusyBoxUserland: the lifecycle with the POSIX tools of BusyBox (as on
// Alpine Linux), and curl.
func TestBusyBoxUserland(t *testing.T) {
	bb, ok := busyboxPath(t)
	if !ok {
		t.Skip("busybox is not installed (set ARK_INSTALLER_BUSYBOX=required to require it)")
	}
	for _, p := range projects {
		t.Run(p.app, func(t *testing.T) {
			t.Parallel()
			gh := newFakeGitHub(t)
			gh.publish(t, p)
			argv := []string{bb, "sh"}
			sh := shellSpec{name: "busybox", argv: argv, appletsFirst: probeAppletsFirst(t, argv)}
			lifecycle(t, newEnv(t, gh, p, sh, toolset{busybox: bb, shaTool: "sha256sum"}))
		})
	}
}

// TestShaTools: each SHA-256 tool install.sh knows works, and without any the
// run stops before downloading.
func TestShaTools(t *testing.T) {
	sh := pathShell(t)
	for _, tool := range []string{"sha256sum", "shasum", "openssl"} {
		t.Run(tool, func(t *testing.T) {
			if _, ok := lookTool(tool); !ok {
				t.Skipf("%s is not installed", tool)
			}
			t.Parallel()
			gh := newFakeGitHub(t)
			gh.publish(t, selfProject)
			e := newEnv(t, gh, selfProject, sh, toolset{shaTool: tool})
			r := e.run()
			checkOK(t, r, "Verified the SHA-256")
			e.checkInstalled(r, filepath.Join(e.defaultDir(), "ark"), selfProject.latest)
		})
	}
	t.Run("none", func(t *testing.T) {
		t.Parallel()
		gh := newFakeGitHub(t)
		gh.publish(t, selfProject)
		e := newEnv(t, gh, selfProject, sh, toolset{shaTool: "sha256sum", omit: []string{"sha256sum"}})
		r := e.run()
		checkFails(t, r, 1, "a SHA-256 tool is required")
		if _, c := gh.snapshot(); len(c) != 0 {
			t.Fatalf("it connected before failing: %v", c)
		}
	})
}

// testEnv is an ark environment with the default shell, curl and a release.
func testEnv(t *testing.T, ts toolset) (*env, *fakeGitHub) {
	t.Helper()
	gh := newFakeGitHub(t)
	gh.publish(t, selfProject)
	return newEnv(t, gh, selfProject, pathShell(t), ts), gh
}

// userland is a set of POSIX tools: this machine's, or BusyBox's.
type userland struct {
	name  string
	shell shellSpec
	tools toolset
}

// userlands: the tools of this machine, and BusyBox when there is one; the
// archive and checksum tests run with each, since tar and awk differ.
func userlands(t *testing.T) []userland {
	t.Helper()
	out := []userland{{name: "host", shell: pathShell(t)}}
	if bb, ok := busyboxPath(t); ok {
		argv := []string{bb, "sh"}
		out = append(out, userland{name: "busybox", shell: shellSpec{name: "busybox", argv: argv, appletsFirst: probeAppletsFirst(t, argv)},
			tools: toolset{busybox: bb, shaTool: "sha256sum"}})
	}
	return out
}

func userlandEnv(t *testing.T, u userland) (*env, *fakeGitHub) {
	t.Helper()
	gh := newFakeGitHub(t)
	gh.publish(t, selfProject)
	return newEnv(t, gh, selfProject, u.shell, u.tools), gh
}

// withInstalled: an ark v2.0.0 is installed in the default directory.
func withInstalled(t *testing.T, e *env) (string, fileState) {
	t.Helper()
	target := e.install(e.defaultDir(), "v2.0.0")
	return target, stateOf(t, target)
}

func TestCommandLine(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"help", []string{"--help"}, 0, "Usage: sh install.sh"},
		{"short help", []string{"-h"}, 0, "--upgrade"},
		{"unknown option", []string{"--force"}, 2, "unknown option '--force'"},
		{"short unknown", []string{"-v"}, 2, "unknown option '-v'"},
		{"argument", []string{"v1.2.3"}, 2, "unexpected argument 'v1.2.3'"},
		{"version without value", []string{"--version"}, 2, "--version needs a value"},
		{"version empty", []string{"--version="}, 2, "--version needs a value"},
		{"version then option", []string{"--version", "--upgrade"}, 2, "--version needs a value"},
		{"version twice", []string{"--version", "v2.0.0", "--version", "v2.0.1"}, 2, "--version is given more than once"},
		{"dir without value", []string{"--dir"}, 2, "--dir needs a directory"},
		{"dir empty", []string{"--dir="}, 2, "--dir needs a directory"},
		{"dir option-like", []string{"--dir", "-x"}, 2, "--dir needs a directory"},
		{"dir twice", []string{"--dir", "a", "--dir=b"}, 2, "--dir is given more than once"},
		{"dir newline", []string{"--dir", "a\nb"}, 2, "--dir must not contain a newline"},
		{"upgrade twice", []string{"--upgrade", "--upgrade"}, 2, "--upgrade is given more than once"},
	}
	for _, bad := range []string{"v1", "v1.2", "v1.2.3.4", "v01.2.3", "v1.02.3", "v1.2.03", "V1.2.3", "vv1.2.3",
		"v1.2.3+build", "v1.2.3-", "v1.2.3-rc..1", "v1.2.3-.rc", "v1.2.3-rc_1", "v1.2.3/../x", "../v1.2.3",
		"v1.2.3 ", " v1.2.3", "v1.2.3;id", "$(id)", "v1.2.3%2f", "latest", "v1234567890.0.0",
		"v1.2.3-" + strings.Repeat("a", 60)} {
		cases = append(cases, struct {
			name string
			args []string
			code int
			want string
		}{"bad version " + bad, []string{"--version", bad}, 2, "is not a version like v1.2.3"})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e, gh := testEnv(t, toolset{})
			r := e.run(c.args...)
			if c.code == 0 {
				checkOK(t, r, c.want)
			} else {
				checkFails(t, r, c.code, c.want)
			}
			if _, conns := gh.snapshot(); len(conns) != 0 {
				t.Fatalf("it connected: %v\n%s", conns, r)
			}
			if _, err := os.Stat(e.defaultDir()); err == nil {
				t.Fatalf("it created %s\n%s", e.defaultDir(), r)
			}
		})
	}
}

// TestPrereleaseVersion: a prerelease is installed when it is asked for.
func TestPrereleaseVersion(t *testing.T) {
	t.Parallel()
	gh := newFakeGitHub(t)
	p := selfProject
	p.tags = append(append([]string{}, p.tags...), "v2.1.0-rc.1")
	gh.publish(t, p)
	e := newEnv(t, gh, p, pathShell(t), toolset{})
	r := e.run("--version", "v2.1.0-rc.1")
	checkOK(t, r, "Installed ark v2.1.0-rc.1")
	e.checkInstalled(r, filepath.Join(e.defaultDir(), "ark"), "v2.1.0-rc.1")
	// then the latest stable one (older than the prerelease)
	r = e.run()
	checkOK(t, r, "Downgraded ark v2.1.0-rc.1 to v2.0.1")
}

func TestTimeoutSetting(t *testing.T) {
	for _, v := range []string{"0", "3601", "abc", "-1", "1.5", "010"} {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			e, _ := testEnv(t, toolset{})
			e.vars = append(e.vars, "INSTALLER_TIMEOUT="+v)
			checkFails(t, e.run(), 1, "INSTALLER_TIMEOUT '"+v+"' must be a number of seconds")
		})
	}
}

// TestConfiguration: invalid project configurations stop install.sh before it
// does anything.
func TestConfiguration(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ app, url, want string }{
		{"", "https://github.com/magicdrive/ark", "APP_NAME '' is not a valid name"},
		{"../ark", "https://github.com/magicdrive/ark", "is not a valid name"},
		{"en/ma", "https://github.com/magicdrive/ark", "is not a valid name"},
		{"en ma", "https://github.com/magicdrive/ark", "is not a valid name"},
		{"ark;id", "https://github.com/magicdrive/ark", "is not a valid name"},
		{"$(id)", "https://github.com/magicdrive/ark", "is not a valid name"},
		{"-ark", "https://github.com/magicdrive/ark", "is not a valid name"},
		{".ark", "https://github.com/magicdrive/ark", "is not a valid name"},
		{"..", "https://github.com/magicdrive/ark", "is not a valid name"},
		{"ark", "http://github.com/magicdrive/ark", "must start with https://github.com/"},
		{"ark", "https://github.com.evil.example/magicdrive/ark", "must start with https://github.com/"},
		{"ark", "https://evil.example/https://github.com/magicdrive/ark", "must start with https://github.com/"},
		{"ark", "https://user@github.com/magicdrive/ark", "must start with https://github.com/"},
		{"ark", "https://github.com:8443/magicdrive/ark", "must start with https://github.com/"},
		{"ark", "https://GitHub.com/magicdrive/ark", "must start with https://github.com/"},
		{"ark", "https://github.com/magicdrive", "must be https://github.com/OWNER/REPOSITORY"},
		{"ark", "https://github.com/magicdrive/ark/", "must be https://github.com/OWNER/REPOSITORY"},
		{"ark", "https://github.com/magicdrive/ark/releases", "must be https://github.com/OWNER/REPOSITORY"},
		{"ark", "https://github.com//ark", "must be https://github.com/OWNER/REPOSITORY"},
		{"ark", "https://github.com/magicdrive/ark?x=1", "is not a valid repository"},
		{"ark", "https://github.com/magicdrive/ark#x", "is not a valid repository"},
		{"ark", "https://github.com/magicdrive/ark.git", "is not a valid repository"},
		{"ark", "https://github.com/magicdrive/..", "is not a valid repository"},
		{"ark", "https://github.com/magic_drive/ark", "is not a valid owner"},
		{"ark", "https://github.com/-magicdrive/ark", "is not a valid owner"},
		{"ark", "https://github.com/magic%2Fdrive/ark", "is not a valid owner"},
		{"ark", "https://github.com/magicdrive/en ma", "is not a valid repository"},
	}
	for _, c := range cases {
		t.Run(c.app+" "+c.url, func(t *testing.T) {
			t.Parallel()
			e, gh := testEnv(t, toolset{})
			e.script = writeScript(t, configure(t, string(src), c.app, c.url))
			r := e.run()
			checkFails(t, r, 1, c.want)
			if _, conns := gh.snapshot(); len(conns) != 0 {
				t.Fatalf("it connected: %v", conns)
			}
		})
	}
	// The configuration is only the two lines: nothing else names ark.
	body := string(src)
	body = strings.Replace(body, appLine, "", 1)
	body = strings.Replace(body, urlLine, "", 1)
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "ark") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Errorf("install.sh names ark outside its configuration: %q", line)
		}
		// copied from enma: no line of enma's own may be left
		if strings.Contains(line, "enma") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Errorf("install.sh names enma outside its comments: %q", line)
		}
	}
}

func TestPlatforms(t *testing.T) {
	cases := []struct {
		uname  [2]string
		sysctl string
		asset  string // "" = unsupported
		want   string
	}{
		{[2]string{"Linux", "x86_64"}, "", "linux_amd64", ""},
		{[2]string{"Linux", "amd64"}, "", "linux_amd64", ""},
		{[2]string{"Linux", "aarch64"}, "", "linux_arm64", ""},
		{[2]string{"Linux", "arm64"}, "", "linux_arm64", ""},
		{[2]string{"Darwin", "x86_64"}, "0", "darwin_amd64", ""},
		{[2]string{"Darwin", "x86_64"}, "", "darwin_amd64", ""},
		{[2]string{"Darwin", "x86_64"}, "1", "darwin_arm64", "Rosetta 2"},
		{[2]string{"Darwin", "arm64"}, "", "darwin_arm64", ""},
		{[2]string{"Linux", "i686"}, "", "", "unsupported CPU 'i686'"},
		{[2]string{"Linux", "armv7l"}, "", "", "unsupported CPU 'armv7l'"},
		{[2]string{"Linux", "riscv64"}, "", "", "unsupported CPU 'riscv64'"},
		{[2]string{"FreeBSD", "amd64"}, "", "", "unsupported operating system 'FreeBSD'"},
		{[2]string{"MINGW64_NT-10.0", "x86_64"}, "", "", "Windows is not supported"},
		{[2]string{"CYGWIN_NT-10.0", "x86_64"}, "", "", "Windows is not supported"},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.uname, c.sysctl), func(t *testing.T) {
			t.Parallel()
			e, gh := testEnv(t, toolset{uname: c.uname, sysctl: c.sysctl})
			r := e.run()
			if c.asset == "" {
				checkFails(t, r, 1, c.want)
				if _, conns := gh.snapshot(); len(conns) != 0 {
					t.Fatalf("it connected before failing: %v", conns)
				}
				return
			}
			checkOK(t, r, c.want)
			if !strings.Contains(r.stdout, "ark_2.0.1_"+c.asset+".tar.gz") {
				t.Fatalf("want the %s archive\n%s", c.asset, r)
			}
			e.checkInstalled(r, filepath.Join(e.defaultDir(), "ark"), "v2.0.1")
		})
	}
}

// TestLatestResolution: wrong answers for releases/latest stop the run, and
// no other release is installed instead.
func TestLatestResolution(t *testing.T) {
	const latest = "/magicdrive/ark/releases/latest"
	cases := []struct {
		name string
		f    fault
		want string
	}{
		{"200 instead of a redirect", fault{status: 200, body: []byte("v2.0.1")}, "answered HTTP status 200"},
		{"404", fault{status: 404}, "answered HTTP status 404"},
		{"500", fault{status: 500}, "answered HTTP status 500"},
		{"no release yet", fault{location: "https://github.com/magicdrive/ark/releases"}, "no stable release yet"},
		{"another repository", fault{location: "https://github.com/evil/ark/releases/tag/v2.0.1"}, "redirects to"},
		{"another host", fault{location: "https://evil.example/magicdrive/ark/releases/tag/v2.0.1"}, "redirects to"},
		{"not a version", fault{location: "https://github.com/magicdrive/ark/releases/tag/nightly"}, "the tag 'nightly'"},
		{"traversal", fault{location: "https://github.com/magicdrive/ark/releases/tag/v2.0.1/../../x"}, "redirects to"},
		{"query", fault{location: "https://github.com/magicdrive/ark/releases/tag/v2.0.1?x=1"}, "the tag 'v2.0.1?x=1'"},
		{"empty tag", fault{location: "https://github.com/magicdrive/ark/releases/tag/"}, "redirects to"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, dl := range downloaders(t) {
				e, gh := testEnv(t, toolset{downloader: dl})
				target, before := withInstalled(t, e)
				gh.setFault(latest, c.f)
				r := e.run("--upgrade")
				checkFails(t, r, 1, c.want)
				e.checkUnchanged(r, target, before)
				e.checkClean(r, e.defaultDir())
				reqs, _ := gh.snapshot()
				for _, q := range reqs {
					if strings.Contains(q, "/releases/download/") {
						t.Fatalf("%s: it downloaded %s after the latest release failed", dl, q)
					}
				}
			}
		})
	}
	t.Run("case of the redirect", func(t *testing.T) {
		t.Parallel()
		e, gh := testEnv(t, toolset{})
		gh.setFault(latest, fault{location: "https://github.com/MagicDrive/Ark/releases/tag/v2.0.0"})
		r := e.run()
		checkOK(t, r, "Installed ark v2.0.0")
	})
	t.Run("explicit version does not ask", func(t *testing.T) {
		t.Parallel()
		e, gh := testEnv(t, toolset{})
		gh.setFault(latest, fault{status: 500})
		r := e.run("--version", "v2.0.0")
		checkOK(t, r, "Installed ark v2.0.0")
		reqs, _ := gh.snapshot()
		for _, q := range reqs {
			if strings.HasSuffix(q, "/releases/latest") {
				t.Fatalf("it asked for the latest release: %v", reqs)
			}
		}
	})
}

// TestDownloadFailures: a failed download leaves the installed binary alone.
func TestDownloadFailures(t *testing.T) {
	p := selfProject
	archive := p.archiveName("v2.0.1", "linux", "amd64")
	sums := p.sumsName("v2.0.1")
	cases := []struct {
		name  string
		file  string
		f     fault
		gone  bool // the asset does not exist
		want  string
		quick bool
	}{
		{"archive 404", archive, fault{}, true, "release v2.0.1 has no " + archive, true},
		{"archive 500", archive, fault{status: 500}, false, "HTTP status 500", true},
		{"archive 403", archive, fault{status: 403}, false, "HTTP status 403", true},
		{"archive truncated", archive, fault{truncate: true}, false, "cannot download " + archive, true},
		{"archive stalled", archive, fault{stall: true}, false, "cannot download " + archive, false},
		{"checksums 500 (no fallback)", sums, fault{status: 500}, false, "cannot download the checksum file", true},
		{"checksums truncated", sums, fault{truncate: true}, false, "cannot download the checksum file", true},
		{"checksums missing", sums, fault{}, true, "release v2.0.1 not found", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, dl := range downloaders(t) {
				e, gh := testEnv(t, toolset{downloader: dl})
				e.vars = append(e.vars, "INSTALLER_TIMEOUT=2")
				target, before := withInstalled(t, e)
				if c.gone {
					gh.removeAsset(p, "v2.0.1", c.file)
				} else {
					gh.setFault(gh.assetPath(p, "v2.0.1", c.file), c.f)
				}
				start := time.Now()
				r := e.run("--upgrade")
				checkFails(t, r, 1, c.want)
				e.checkUnchanged(r, target, before)
				e.checkClean(r, e.defaultDir())
				if c.f.stall && time.Since(start) > 30*time.Second {
					t.Fatalf("%s: a stalled download took %v with INSTALLER_TIMEOUT=2", dl, time.Since(start))
				}
				if strings.Contains(c.name, "500 (no fallback)") {
					reqs, _ := gh.snapshot()
					for _, q := range reqs {
						if strings.Contains(q, "ark_2.0.1_checksums.txt") {
							t.Fatalf("%s: it tried the old checksum file name after a 500", dl)
						}
					}
				}
			}
		})
	}
	// A redirect to plain http is not followed: the file (here the right
	// one) must not be fetched without TLS.
	for _, file := range []string{archive, sums} {
		t.Run("redirect to http "+file, func(t *testing.T) {
			t.Parallel()
			for _, dl := range downloaders(t) {
				e, gh := testEnv(t, toolset{downloader: dl})
				target, before := withInstalled(t, e)
				path := gh.assetPath(p, "v2.0.1", file)
				gh.setFault(path, fault{location: gh.plain.URL + path})
				r := e.run("--upgrade")
				if r.code == 0 {
					t.Fatalf("%s: it followed a redirect to http\n%s", dl, r)
				}
				e.checkUnchanged(r, target, before)
				e.checkClean(r, e.defaultDir())
				reqs, _ := gh.snapshot()
				for _, q := range reqs {
					if strings.HasPrefix(q, "plain ") {
						t.Fatalf("%s: it fetched %s over plain http", dl, q)
					}
				}
			}
		})
	}
	t.Run("too many redirects", func(t *testing.T) {
		t.Parallel()
		for _, dl := range downloaders(t) {
			e, gh := testEnv(t, toolset{downloader: dl})
			target, before := withInstalled(t, e)
			path := gh.assetPath(p, "v2.0.1", sums)
			gh.setFault(path, fault{location: "https://" + githubHost + path}) // to itself
			r := e.run("--upgrade")
			checkFails(t, r, 1, "cannot download the checksum file")
			e.checkUnchanged(r, target, before)
		}
	})
	t.Run("connection refused", func(t *testing.T) {
		t.Parallel()
		for _, dl := range downloaders(t) {
			e, _ := testEnv(t, toolset{downloader: dl})
			target, before := withInstalled(t, e)
			e.vars = append(e.vars, "https_proxy=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1")
			r := e.run("--version", "v2.0.1")
			checkFails(t, r, 1, "cannot download the checksum file")
			e.checkUnchanged(r, target, before)
			e.checkClean(r, e.defaultDir())
			r = e.run()
			checkFails(t, r, 1, "cannot reach https://github.com/magicdrive/ark/releases/latest")
			e.checkUnchanged(r, target, before)
		}
	})
	t.Run("untrusted certificate", func(t *testing.T) {
		t.Parallel()
		for _, dl := range downloaders(t) {
			e, _ := testEnv(t, toolset{downloader: dl})
			other := newFakeGitHub(t) // another CA
			e.vars = append(e.vars, "CURL_CA_BUNDLE="+other.caFile, "SSL_CERT_FILE="+other.caFile)
			wgetrc := filepath.Join(e.home, "other.wgetrc")
			must(t, os.WriteFile(wgetrc, []byte("ca_certificate = "+other.caFile+"\n"), 0o644))
			e.vars = append(e.vars, "WGETRC="+wgetrc)
			r := e.run("--version", "v2.0.1")
			checkFails(t, r, 1, "cannot download the checksum file")
			if _, err := os.Stat(e.defaultDir()); err == nil {
				t.Fatalf("%s: it created the directory", dl)
			}
		}
	})
}

// TestBusyBoxWget: a wget that is not GNU Wget is refused (BusyBox wget does
// not check certificates).
func TestBusyBoxWget(t *testing.T) {
	t.Parallel()
	e, gh := testEnv(t, toolset{downloader: "wget", omit: []string{"wget"},
		fakes: map[string]string{"wget": `echo "BusyBox v1.36.1 multi-call binary." >&2; exit 1`}})
	r := e.run()
	checkFails(t, r, 1, "is not GNU Wget")
	if _, c := gh.snapshot(); len(c) != 0 {
		t.Fatalf("it connected: %v", c)
	}
	e2, _ := testEnv(t, toolset{omit: []string{"curl"}})
	checkFails(t, e2.run(), 1, "curl (or GNU Wget) is required")
	for v, want := range map[string]string{
		"GNU Wget2 2.1.0 - multithreaded metalink/file/website downloader": "wget2 is not supported",
		"GNU Wget 1.14 built on linux-gnu.":                                "GNU Wget 1.17 or later is needed",
	} {
		e3, gh3 := testEnv(t, toolset{downloader: "wget", omit: []string{"wget"},
			fakes: map[string]string{"wget": `[ "$1" = --version ] && echo "` + v + `" && exit 0; exit 1`}})
		checkFails(t, e3.run(), 1, want)
		if _, c := gh3.snapshot(); len(c) != 0 {
			t.Fatalf("it connected: %v", c)
		}
	}
}

// TestLegacyChecksumName: <app>_<version>_checksums.txt is used only when
// <app>_checksums.txt is not there (404).
func TestLegacyChecksumName(t *testing.T) {
	t.Parallel()
	e, gh := testEnv(t, toolset{})
	r := e.run("--version", "v0.0.21")
	checkOK(t, r, "Installed ark v0.0.21")
	e.checkInstalled(r, filepath.Join(e.defaultDir(), "ark"), "v0.0.21")
	reqs, _ := gh.snapshot()
	var got []string
	for _, q := range reqs {
		if strings.HasPrefix(q, githubHost+" ") && strings.Contains(q, "checksums") {
			got = append(got, q[strings.LastIndex(q, "/")+1:])
		}
	}
	if strings.Join(got, " ") != "ark_checksums.txt ark_0.0.21_checksums.txt" {
		t.Fatalf("checksum files asked for: %v", got)
	}
	// both names present: the current one wins
	p := selfProject
	gh.setAsset(p, "v2.0.1", "ark_2.0.1_checksums.txt", bytes.Repeat([]byte("x"), 10))
	r = e.run("--version", "v2.0.1")
	checkOK(t, r, "Upgraded ark v0.0.21 to v2.0.1")
}

// TestChecksums: the checksum file is read strictly.
func TestChecksums(t *testing.T) {
	p := selfProject
	zero := strings.Repeat("0", 64)
	// name is the archive install.sh fetches in the environment of the run
	type checksumCase struct {
		name  string
		edit  func(sum, all, name string) string
		want  func(name string) string
		wants bool // install succeeds
	}
	is := func(s string) func(string) string { return func(string) string { return s } }
	cases := []checksumCase{
		{"mismatch", func(sum, all, name string) string {
			return strings.Replace(all, sum, zero, 1)
		}, is("SHA-256 mismatch"), false},
		{"entry missing", func(sum, all, name string) string {
			return strings.Replace(all, sum+"  "+name+"\n", "", 1)
		}, func(name string) string { return "has no " + name }, false},
		{"duplicate entry", func(sum, all, name string) string {
			return all + sum + "  " + name + "\n"
		}, func(name string) string { return "lists " + name + " more than once" }, false},
		{"duplicate with another sum", func(sum, all, name string) string {
			return all + zero + "  " + name + "\n"
		}, is("more than once"), false},
		{"only partial matches", func(sum, all, name string) string {
			all = strings.Replace(all, sum+"  "+name+"\n", "", 1)
			return all + sum + "  " + strings.TrimSuffix(name, ".tar.gz") + "\n" + sum + "  x" + name + "\n" +
				sum + "  " + name + ".sig\n" + sum + "  ./" + name + "\n"
		}, func(name string) string { return "has no " + name }, false},
		{"partial matches around the right one", func(sum, all, name string) string {
			return zero + "  " + strings.TrimSuffix(name, ".tar.gz") + "\n" + zero + "  x" + name + "\n" + all +
				zero + "  " + name + ".sig\n"
		}, is("Verified the SHA-256"), true},
		{"one space", func(sum, all, name string) string { return strings.Replace(all, sum+"  ", sum+" ", 1) }, is("malformed"), false},
		{"binary marker", func(sum, all, name string) string { return strings.Replace(all, sum+"  ", sum+" *", 1) }, is("malformed"), false},
		{"upper case", func(sum, all, name string) string { return strings.Replace(all, sum, strings.ToUpper(sum), 1) }, is("malformed"), false},
		{"short sum", func(sum, all, name string) string { return strings.Replace(all, sum, sum[:63], 1) }, is("malformed"), false},
		{"long sum", func(sum, all, name string) string { return strings.Replace(all, sum, sum+"0", 1) }, is("malformed"), false},
		{"not hex", func(sum, all, name string) string { return strings.Replace(all, sum, "g"+sum[1:], 1) }, is("malformed"), false},
		{"CRLF", func(sum, all, name string) string { return strings.ReplaceAll(all, "\n", "\r\n") }, is("malformed"), false},
		{"blank line", func(sum, all, name string) string { return "\n" + all }, is("malformed"), false},
		{"trailing space", func(sum, all, name string) string { return strings.Replace(all, name+"\n", name+" \n", 1) }, is("malformed"), false},
		{"garbage line", func(sum, all, name string) string { return all + "<html>not found</html>\n" }, is("malformed"), false},
		{"empty file", func(sum, all, name string) string { return "" }, func(name string) string { return "has no " + name }, false},
		{"no final newline", func(sum, all, name string) string { return strings.TrimSuffix(all, "\n") }, is("Verified the SHA-256"), true},
	}
	for _, u := range userlands(t) {
		for _, c := range cases {
			t.Run(u.name+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				e, gh := userlandEnv(t, u)
				target, before := withInstalled(t, e)
				name := p.archiveName("v2.0.1", e.goos(), e.goarch())
				all := string(gh.asset(p, "v2.0.1", "ark_checksums.txt"))
				sum := sha(gh.asset(p, "v2.0.1", name))
				gh.setAsset(p, "v2.0.1", "ark_checksums.txt", []byte(c.edit(sum, all, name)))
				r := e.run("--upgrade")
				if c.wants {
					checkOK(t, r, c.want(name))
					e.checkInstalled(r, target, "v2.0.1")
					return
				}
				checkFails(t, r, 1, c.want(name))
				e.checkUnchanged(r, target, before)
				e.checkClean(r, e.defaultDir())
			})
		}
	}
}

// TestArchives: archives that are not safe are refused, even with a matching
// checksum.
func TestArchives(t *testing.T) {
	p := selfProject
	// f holds the release of the platform install.sh runs on (see fixtureFor)
	cases := []struct {
		name    string
		data    func(t *testing.T, f archiveFixture) []byte
		want    string
		success bool
	}{
		{"standard", func(t *testing.T, f archiveFixture) []byte { return makeTarGz(t, f.std) }, "Upgraded", true},
		{"directory entries", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, append([]entry{{name: "completions/", typ: tar.TypeDir, mode: 0o755}}, f.std...))
		}, "Upgraded", true},
		{"not gzip", func(t *testing.T, f archiveFixture) []byte { return []byte("this is not a gzip file at all") }, "not a valid gzip file", false},
		{"truncated gzip", func(t *testing.T, f archiveFixture) []byte { d := makeTarGz(t, f.std); return d[:len(d)/2] }, "not a valid gzip file", false},
		{"gzip of garbage", func(t *testing.T, f archiveFixture) []byte { return gz(t, bytes.Repeat([]byte("garbage "), 200)) }, "not a valid tar archive", false},
		{"absolute path", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "/tmp/evil", body: []byte("x")}))
		}, "an absolute path OR tar reported", false},
		{"dot-dot", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "../evil", body: []byte("x")}))
		}, `a path with ".." OR tar reported`, false},
		{"dot-dot inside", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "completions/../../evil", body: []byte("x")}))
		}, `a path with ".." OR tar reported`, false},
		{"symlink", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "link", typ: tar.TypeSymlink, linkname: "/etc/passwd"}))
		}, "not a file or directory", false},
		{"binary is a symlink", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, []entry{{name: "ark", typ: tar.TypeSymlink, linkname: "/bin/sh"}})
		}, "", false},
		{"hardlink", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "hard", typ: tar.TypeLink, linkname: "ark"}))
		}, "", false},
		{"binary is a hardlink", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, []entry{{name: "README.md", body: f.bin, mode: 0o755}, {name: "ark", typ: tar.TypeLink, linkname: "README.md"}})
		}, "", false},
		{"fifo", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "fifo", typ: tar.TypeFifo}))
		}, "not a file or directory", false},
		{"char device", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "dev", typ: tar.TypeChar}))
		}, "not a file or directory", false},
		{"duplicate binary", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "ark", body: []byte("#!/bin/sh\necho evil\n"), mode: 0o755}))
		}, "a duplicate entry", false},
		{"duplicate other file", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: selfProject.files[0], body: []byte("x")}))
		}, "a duplicate entry", false},
		{"binary missing", func(t *testing.T, f archiveFixture) []byte { return makeTarGz(t, f.std[:len(f.std)-1]) }, "no ark at the top", false},
		{"binary in a directory", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, append(f.std[:len(f.std)-1:len(f.std)-1], entry{name: "dist/ark", body: f.bin, mode: 0o755}))
		}, "no ark at the top", false},
		{"binary as ./ark", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, append(f.std[:len(f.std)-1:len(f.std)-1], entry{name: "./ark", body: f.bin, mode: 0o755}))
		}, `an empty or "." part`, false},
		// another spelling of the same name: tars disagree on which one they extract
		{"ark and ./ark", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "./ark", body: []byte("#!/bin/sh\necho 'ark version v2.0.1'\n# evil\n"), mode: 0o755}))
		}, `an empty or "." part`, false},
		{"./ark and ark", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, append([]entry{{name: "./ark", body: []byte("#!/bin/sh\necho 'ark version v2.0.1'\n# evil\n"), mode: 0o755}}, f.std...))
		}, `an empty or "." part`, false},
		{"ark and .//ark", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: ".//ark", body: []byte("x"), mode: 0o755}))
		}, `an empty or "." part`, false},
		{"a//b", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "completions//x", body: []byte("x")}))
		}, `an empty or "." part`, false},
		{"a/./b", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "completions/./x", body: []byte("x")}))
		}, `an empty or "." part`, false},
		{"entries below ark/", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "ark/evil", body: []byte("x")}))
		}, "an entry below ark/", false},
		{"binary is a directory", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, append(f.std[:len(f.std)-1:len(f.std)-1], entry{name: "ark/", typ: tar.TypeDir, mode: 0o755}))
		}, "", false},
		{"newline in a name", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: "x\nark", body: []byte("x")}))
		}, "", false},
		{"backslash in a name", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, f.with(entry{name: `a\b`, body: []byte("x")}))
		}, "a name with a backslash", false},
		{"empty binary", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, append(f.std[:len(f.std)-1:len(f.std)-1], entry{name: "ark", body: nil, mode: 0o755}))
		}, "", false},
		{"binary of another version", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, append(f.std[:len(f.std)-1:len(f.std)-1], entry{name: "ark", body: p.fakeBinary("v9.0.0", f.goos, f.goarch), mode: 0o755}))
		}, "does not report v2.0.1", false},
		{"binary that does not run", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, append(f.std[:len(f.std)-1:len(f.std)-1], entry{name: "ark", body: []byte("\x7fELF garbage"), mode: 0o755}))
		}, "does not run", false},
		{"setuid binary", func(t *testing.T, f archiveFixture) []byte {
			return makeTarGz(t, append(f.std[:len(f.std)-1:len(f.std)-1], entry{name: "ark", body: f.bin, mode: 0o4755}))
		}, "Upgraded", true},
	}
	for _, u := range userlands(t) {
		for _, c := range cases {
			t.Run(u.name+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				e, gh := userlandEnv(t, u)
				target, before := withInstalled(t, e)
				f := fixtureFor(p, "v2.0.1", e.goos(), e.goarch())
				gh.replaceArchive(t, p, "v2.0.1", f.goos, f.goarch, c.data(t, f))
				r := e.run("--upgrade")
				if c.success {
					checkOK(t, r, c.want)
					e.checkInstalled(r, target, "v2.0.1")
					e.checkClean(r, e.defaultDir())
					return
				}
				if r.code != 1 {
					t.Fatalf("want exit status 1\n%s", r)
				}
				// "a OR b": tars differ in what they show of an unsafe name
				found := c.want == ""
				for _, w := range strings.Split(c.want, " OR ") {
					found = found || strings.Contains(r.stderr, w)
				}
				if !found {
					t.Fatalf("stderr does not say %q\n%s", c.want, r)
				}
				if strings.Count(strings.TrimSpace(r.stderr), "\n") != 0 && !strings.Contains(r.stderr, "tar reported") {
					t.Fatalf("more than one line of error\n%s", r)
				}
				if !strings.Contains(r.stderr, "rejected") && !strings.Contains(r.stderr, "not a valid") &&
					!strings.Contains(r.stderr, "cannot extract") && !strings.Contains(r.stderr, "nothing was replaced") {
					t.Fatalf("the failure does not say why\n%s", r)
				}
				e.checkUnchanged(r, target, before)
				e.checkClean(r, e.defaultDir())
				// nothing of the archive escaped into the home directory or /tmp
				for _, p := range []string{filepath.Join(e.home, "evil"), filepath.Join(filepath.Dir(e.tmp), "evil"), "/tmp/evil"} {
					if _, err := os.Lstat(p); err == nil {
						t.Fatalf("%s was created\n%s", p, r)
					}
				}
			})
		}
	}
}

// archiveFixture is the standard release archive of one platform, for
// TestArchives to change.
type archiveFixture struct {
	goos, goarch string
	bin          []byte  // the binary
	std          []entry // the archive GoReleaser makes
}

func fixtureFor(p project, tag, goos, goarch string) archiveFixture {
	return archiveFixture{goos: goos, goarch: goarch, bin: p.fakeBinary(tag, goos, goarch), std: p.standardEntries(tag, goos, goarch)}
}

// with is the standard archive with more entries after it.
func (f archiveFixture) with(es ...entry) []entry {
	return append(append([]entry{}, f.std...), es...)
}

func gz(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	must(t, w.Close())
	return buf.Bytes()
}

// TestInstallDirectory: where install.sh agrees to install.
func TestInstallDirectory(t *testing.T) {
	t.Run("custom directory with spaces", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		dir := filepath.Join(e.home, "my tools", "bin dir")
		r := e.run("--dir", dir)
		checkOK(t, r, "Installed ark v2.0.1")
		e.checkInstalled(r, filepath.Join(dir, "ark"), "v2.0.1")
		e.checkClean(r, dir)
		r = e.run("--dir="+dir, "--version", "v2.0.0")
		checkOK(t, r, "Downgraded ark v2.0.1 to v2.0.0")
	})
	t.Run("relative directory", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		r := e.run("--dir", "rel/bin")
		checkOK(t, r, "Installed ark v2.0.1")
		e.checkInstalled(r, filepath.Join(e.home, "rel", "bin", "ark"), "v2.0.1")
	})
	t.Run("not writable", func(t *testing.T) {
		if isRoot() {
			t.Skip("root ignores file permissions")
		}
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		target, before := withInstalled(t, e)
		must(t, os.Chmod(e.defaultDir(), 0o555))
		t.Cleanup(func() { _ = os.Chmod(e.defaultDir(), 0o755) })
		r := e.run("--upgrade")
		checkFails(t, r, 1, "is not writable (this installer does not use sudo)")
		e.checkUnchanged(r, target, before)
		e.checkClean(r)
	})
	t.Run("cannot be created", func(t *testing.T) {
		if isRoot() {
			t.Skip("root ignores file permissions")
		}
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		ro := filepath.Join(e.home, "ro")
		must(t, os.Mkdir(ro, 0o555))
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		r := e.run("--dir", filepath.Join(ro, "bin"))
		checkFails(t, r, 1, "cannot create")
		e.checkClean(r)
	})
	t.Run("not a directory", func(t *testing.T) {
		t.Parallel()
		e, gh := testEnv(t, toolset{})
		f := filepath.Join(e.home, "file")
		must(t, os.WriteFile(f, []byte("x"), 0o644))
		r := e.run("--dir", f)
		checkFails(t, r, 1, "is not a directory")
		if _, c := gh.snapshot(); len(c) != 0 {
			t.Fatalf("it connected first: %v", c)
		}
	})
	t.Run("world-writable", func(t *testing.T) {
		t.Parallel()
		e, gh := testEnv(t, toolset{})
		d := filepath.Join(e.home, "shared")
		must(t, os.Mkdir(d, 0o777))
		must(t, os.Chmod(d, 0o777|os.ModeSticky))
		r := e.run("--dir", d)
		checkFails(t, r, 1, "is writable by every user")
		if _, c := gh.snapshot(); len(c) != 0 {
			t.Fatalf("it connected first: %v", c)
		}
	})
	t.Run("target is a symlink", func(t *testing.T) {
		t.Parallel()
		e, gh := testEnv(t, toolset{})
		real := e.install(filepath.Join(e.home, "Cellar", "ark", "2.0.0", "bin"), "v2.0.0")
		dir := e.defaultDir()
		must(t, os.MkdirAll(dir, 0o755))
		link := filepath.Join(dir, "ark")
		must(t, os.Symlink(real, link))
		before, beforeReal := stateOf(t, link), stateOf(t, real)
		r := e.run("--upgrade")
		checkFails(t, r, 1, "is a symbolic link")
		e.checkUnchanged(r, link, before)
		e.checkUnchanged(r, real, beforeReal)
		if _, c := gh.snapshot(); len(c) != 0 {
			t.Fatalf("it connected first: %v", c)
		}
	})
	t.Run("dangling symlink target", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		dir := e.defaultDir()
		must(t, os.MkdirAll(dir, 0o755))
		must(t, os.Symlink(filepath.Join(e.home, "nowhere"), filepath.Join(dir, "ark")))
		checkFails(t, e.run(), 1, "is a symbolic link")
		if _, err := os.Lstat(filepath.Join(e.home, "nowhere")); err == nil {
			t.Fatal("the link target was created")
		}
	})
	t.Run("target is a directory", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		must(t, os.MkdirAll(filepath.Join(e.defaultDir(), "ark"), 0o755))
		checkFails(t, e.run(), 1, "is not a regular file")
	})
	t.Run("directory is a symlink", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		real := filepath.Join(e.home, "dotfiles", "bin")
		must(t, os.MkdirAll(real, 0o755))
		must(t, os.MkdirAll(filepath.Join(e.home, ".local"), 0o755))
		must(t, os.Symlink(real, e.defaultDir()))
		r := e.run()
		checkOK(t, r, "Installed ark v2.0.1")
		e.checkInstalled(r, filepath.Join(real, "ark"), "v2.0.1")
		if fi, err := os.Lstat(e.defaultDir()); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatal("the directory link was replaced")
		}
	})
	t.Run("default needs HOME", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		e.vars = append(e.vars, "HOME=")
		checkFails(t, e.run(), 1, "HOME is not set; use --dir DIR")
	})
	t.Run("relative TMPDIR", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		must(t, os.Mkdir(filepath.Join(e.home, "reltmp"), 0o700))
		e.vars = append(e.vars, "TMPDIR=reltmp")
		r := e.run()
		checkOK(t, r, "Installed ark v2.0.1")
		if left, _ := os.ReadDir(filepath.Join(e.home, "reltmp")); len(left) != 0 {
			t.Fatalf("left in TMPDIR: %v", names(left))
		}
	})
	t.Run("new directory below a world-writable one", func(t *testing.T) {
		t.Parallel()
		e, gh := testEnv(t, toolset{})
		shared := filepath.Join(e.home, "shared")
		must(t, os.Mkdir(shared, 0o777))
		must(t, os.Chmod(shared, 0o777))
		r := e.run("--dir", filepath.Join(shared, "a", "bin"))
		checkFails(t, r, 1, "is writable by every user")
		if _, c := gh.snapshot(); len(c) != 0 {
			t.Fatalf("it connected first: %v", c)
		}
		if _, err := os.Stat(filepath.Join(shared, "a")); err == nil {
			t.Fatal("it created a directory")
		}
	})
	t.Run("new directory through ..", func(t *testing.T) {
		t.Parallel()
		e, gh := testEnv(t, toolset{})
		shared := filepath.Join(e.home, "ww")
		must(t, os.Mkdir(shared, 0o777))
		must(t, os.Chmod(shared, 0o777))
		r := e.run("--dir", filepath.Join(e.home, "nope")+"/../ww/z")
		checkFails(t, r, 1, "must not contain '..'")
		if _, c := gh.snapshot(); len(c) != 0 {
			t.Fatalf("it connected first: %v", c)
		}
		if _, err := os.Stat(filepath.Join(shared, "z")); err == nil {
			t.Fatal("it created a directory")
		}
	})
	t.Run("existing directory below a world-writable one", func(t *testing.T) {
		t.Parallel()
		e, gh := testEnv(t, toolset{})
		shared := filepath.Join(e.home, "ww")
		must(t, os.MkdirAll(filepath.Join(shared, "z"), 0o755))
		must(t, os.Chmod(shared, 0o777))
		r := e.run("--dir", filepath.Join(shared, "z"))
		checkFails(t, r, 1, "the directory above")
		if _, c := gh.snapshot(); len(c) != 0 {
			t.Fatalf("it connected first: %v", c)
		}
	})
	t.Run("new directory below a sticky one", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		shared := filepath.Join(e.home, "sticky")
		must(t, os.Mkdir(shared, 0o777))
		must(t, os.Chmod(shared, 0o777|os.ModeSticky))
		dir := filepath.Join(shared, "me", "bin")
		r := e.run("--dir", dir)
		checkOK(t, r, "Installed ark v2.0.1")
		e.checkInstalled(r, filepath.Join(dir, "ark"), "v2.0.1")
	})
	t.Run("bad TMPDIR", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		target, before := withInstalled(t, e)
		e.vars = append(e.vars, "TMPDIR="+filepath.Join(e.home, "missing"))
		r := e.run("--upgrade")
		checkFails(t, r, 1, "cannot create a temporary directory")
		e.checkUnchanged(r, target, before)
	})
}

// TestHomebrew: a directory of Homebrew is refused.
func TestHomebrew(t *testing.T) {
	setups := map[string]func(e *env, prefix string){
		"HOMEBREW_PREFIX": func(e *env, prefix string) { e.vars = append(e.vars, "HOMEBREW_PREFIX="+prefix) },
		"brew --prefix":   func(e *env, prefix string) {},
	}
	for _, name := range sortedKeys(setups) {
		for _, sub := range []string{"bin", "Cellar/ark/2.0.0/bin", "opt/ark/bin"} {
			t.Run(name+"/"+sub, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				prefix := filepath.Join(root, "homebrew")
				ts := toolset{}
				if name == "brew --prefix" {
					ts.brewPrefix = prefix
				}
				e, gh := testEnv(t, ts)
				setups[name](e, prefix)
				dir := filepath.Join(prefix, sub)
				target := e.install(dir, "v2.0.0")
				before := stateOf(t, target)
				r := e.run("--upgrade", "--dir", dir)
				checkFails(t, r, 1, "inside the Homebrew prefix")
				e.checkUnchanged(r, target, before)
				if _, c := gh.snapshot(); len(c) != 0 {
					t.Fatalf("it connected first: %v", c)
				}
				// through a symbolic link to the prefix, too
				link := filepath.Join(root, "link")
				must(t, os.Symlink(dir, link))
				checkFails(t, e.run("--dir", link), 1, "inside the Homebrew prefix")
			})
		}
	}
	t.Run("new directory inside the prefix", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		prefix := filepath.Join(root, "homebrew")
		must(t, os.MkdirAll(prefix, 0o755))
		e, gh := testEnv(t, toolset{brewPrefix: prefix})
		r := e.run("--dir", filepath.Join(prefix, "new", "bin"))
		checkFails(t, r, 1, "inside the Homebrew prefix")
		if _, c := gh.snapshot(); len(c) != 0 {
			t.Fatalf("it connected first: %v", c)
		}
		if _, err := os.Stat(filepath.Join(prefix, "new")); err == nil {
			t.Fatal("it created a directory in the prefix")
		}
	})
	t.Run("new Cellar directory", func(t *testing.T) {
		t.Parallel()
		e, gh := testEnv(t, toolset{})
		r := e.run("--dir", filepath.Join(e.home, "x", "Cellar", "bin"))
		checkFails(t, r, 1, "inside a Homebrew Cellar")
		if _, c := gh.snapshot(); len(c) != 0 {
			t.Fatalf("it connected first: %v", c)
		}
		if _, err := os.Stat(filepath.Join(e.home, "x")); err == nil {
			t.Fatal("it created a directory")
		}
	})
	t.Run("a Cellar without brew", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		dir := filepath.Join(e.home, "usr-local", "Cellar", "ark", "2.0.0", "bin")
		target := e.install(dir, "v2.0.0")
		before := stateOf(t, target)
		r := e.run("--dir", dir)
		checkFails(t, r, 1, "inside a Homebrew Cellar")
		e.checkUnchanged(r, target, before)
	})
	t.Run("next to the prefix", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		e, _ := testEnv(t, toolset{brewPrefix: filepath.Join(root, "homebrew")})
		must(t, os.MkdirAll(filepath.Join(root, "homebrew"), 0o755))
		dir := filepath.Join(root, "homebrew-other", "bin") // shares a name prefix only
		r := e.run("--dir", dir)
		checkOK(t, r, "Installed ark v2.0.1")
	})
}

// TestInstallFailures: failures while putting the binary in place keep the
// installed one and leave no temporary file.
func TestInstallFailures(t *testing.T) {
	cases := []struct {
		name  string
		fakes map[string]string
		want  string
	}{
		{"rename fails", map[string]string{"mv": `echo "mv: cannot move" >&2; exit 1`}, "cannot move the new binary"},
		{"copy fails", map[string]string{"cat": `case "$1" in */x/*) exit 1 ;; esac; exec /bin/cat "$@"`}, "cannot write to"},
		{"copy is short", map[string]string{"cat": `case "$1" in */x/*) /bin/cat "$1" | head -c 10; exit 0 ;; esac; exec /bin/cat "$@"`}, "is incomplete"},
		{"chmod fails", map[string]string{"chmod": `exit 1`}, "cannot make"},
		{"temporary file fails", map[string]string{"mktemp": `case "$1" in -d) exec ` + mustTool("mktemp") + ` "$@" ;; esac; exit 1`}, "cannot create a temporary file in"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e, _ := testEnv(t, toolset{fakes: c.fakes})
			if strings.Contains(c.fakes["cat"], "head") {
				if p, ok := lookTool("head"); ok {
					must(t, os.Symlink(p, filepath.Join(e.bin, "head")))
				}
			}
			target, before := withInstalled(t, e)
			r := e.run("--upgrade")
			checkFails(t, r, 1, c.want)
			e.checkUnchanged(r, target, before)
			e.checkClean(r, e.defaultDir())
		})
	}
	t.Run("target becomes a directory before the rename", func(t *testing.T) {
		t.Parallel()
		// chmod (of the copy, just before the rename) also swaps the target for
		// a directory: the check right before the rename must notice
		e, _ := testEnv(t, toolset{})
		target, _ := withInstalled(t, e)
		e.tools.fakes = map[string]string{"chmod": mustTool("chmod") + ` "$@" && ` + mustTool("rm") + ` -f "` + target + `" && ` + mustTool("mkdir") + ` "` + target + `"`}
		must(t, os.Remove(filepath.Join(e.bin, "chmod")))
		e.buildFake("chmod")
		r := e.run("--upgrade")
		checkFails(t, r, 1, "exists and is not a regular file")
		if es, _ := os.ReadDir(target); len(es) != 0 {
			t.Fatalf("something was moved into %s: %v\n%s", target, names(es), r)
		}
		e.checkClean(r, e.defaultDir())
	})
	t.Run("noexec directory", func(t *testing.T) {
		t.Parallel()
		// a binary that cannot run where it is installed: the copy is checked
		// before it replaces anything
		e, gh := testEnv(t, toolset{})
		target, before := withInstalled(t, e)
		p := selfProject
		std := p.standardEntries("v2.0.1", "linux", "amd64")
		std[len(std)-1].body = []byte("#!/nonexistent/interpreter\n")
		gh.replaceArchive(t, p, "v2.0.1", "linux", "amd64", makeTarGz(t, std))
		r := e.run("--upgrade")
		checkFails(t, r, 1, "nothing was replaced")
		e.checkUnchanged(r, target, before)
		e.checkClean(r, e.defaultDir())
	})
}

func mustTool(name string) string {
	p, _ := lookTool(name)
	return p
}

// TestExistingInstallations: what install.sh says about what it replaces.
func TestExistingInstallations(t *testing.T) {
	t.Run("unknown version", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		dir := e.defaultDir()
		must(t, os.MkdirAll(dir, 0o755))
		writeExecutable(t, filepath.Join(dir, "ark"), []byte("#!/bin/sh\necho something else\n"))
		r := e.run()
		checkOK(t, r, "(unknown version) with ark v2.0.1")
		e.checkInstalled(r, filepath.Join(dir, "ark"), "v2.0.1")
		if !strings.Contains(r.stderr, "did not report an ark version (or was not run: not executable, or not yours); it is replaced") {
			t.Fatalf("no warning\n%s", r)
		}
	})
	t.Run("not executable", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		dir := e.defaultDir()
		must(t, os.MkdirAll(dir, 0o755))
		must(t, os.WriteFile(filepath.Join(dir, "ark"), []byte("data"), 0o644))
		r := e.run()
		checkOK(t, r, "(unknown version)")
		e.checkInstalled(r, filepath.Join(dir, "ark"), "v2.0.1")
	})
	t.Run("same file, not executable", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		dir := e.defaultDir()
		must(t, os.MkdirAll(dir, 0o755))
		must(t, os.WriteFile(filepath.Join(dir, "ark"), e.wantBinary("v2.0.1"), 0o644))
		r := e.run()
		checkOK(t, r, "(unknown version) with ark v2.0.1")
		e.checkInstalled(r, filepath.Join(dir, "ark"), "v2.0.1")
	})
	t.Run("same version, different file", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		dir := e.defaultDir()
		must(t, os.MkdirAll(dir, 0o755))
		writeExecutable(t, filepath.Join(dir, "ark"), []byte("#!/bin/sh\necho 'ark version v2.0.1'\n# modified\n"))
		r := e.run()
		checkOK(t, r, "Reinstalled ark v2.0.1")
		e.checkInstalled(r, filepath.Join(dir, "ark"), "v2.0.1")
	})
	t.Run("file of another user is not run", func(t *testing.T) {
		t.Parallel()
		// id -u names someone else: the installed file is not run for its version
		e, _ := testEnv(t, toolset{fakes: map[string]string{"id": "echo 4242424"}})
		dir := e.defaultDir()
		must(t, os.MkdirAll(dir, 0o755))
		marker := filepath.Join(e.home, "ran")
		writeExecutable(t, filepath.Join(dir, "ark"), []byte("#!/bin/sh\ntouch '"+marker+"'\necho 'ark version v2.0.0'\n"))
		r := e.run()
		checkOK(t, r, "(unknown version) with ark v2.0.1")
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("it ran a file it does not own\n%s", r)
		}
	})
	t.Run("upgrade with nothing installed", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		r := e.run("--upgrade")
		checkOK(t, r, "No ark at")
		checkOK(t, r, "Installed ark v2.0.1")
	})
	t.Run("another ark first in PATH", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{fakes: map[string]string{"ark": "echo 'ark version v1.0.0'"}})
		e.path = append(e.path, e.defaultDir())
		r := e.run()
		checkOK(t, r, "Installed ark v2.0.1")
		if !strings.Contains(r.stderr, "comes first in PATH") {
			t.Fatalf("no warning about the other ark\n%s", r)
		}
		if strings.Contains(r.stdout, "is not in PATH") {
			t.Fatalf("PATH hint although the directory is in PATH\n%s", r)
		}
	})
	t.Run("APP_NAME of a function of the script", func(t *testing.T) {
		t.Parallel()
		// "say" is also a function of install.sh: it is not "another say in PATH"
		p := project{app: "say", owner: "magicdrive", repo: "say", latest: "v1.0.0", tags: []string{"v1.0.0"}}
		gh := newFakeGitHub(t)
		gh.publish(t, p)
		e := newEnv(t, gh, p, pathShell(t), toolset{})
		e.path = append(e.path, e.defaultDir())
		r := e.run()
		checkOK(t, r, "Installed say v1.0.0")
		if strings.Contains(r.stderr, "comes first in PATH") {
			t.Fatalf("warning about the function say\n%s", r)
		}
	})
	t.Run("in PATH, no other", func(t *testing.T) {
		t.Parallel()
		e, _ := testEnv(t, toolset{})
		e.path = append(e.path, e.defaultDir())
		r := e.run()
		checkOK(t, r, "Installed ark v2.0.1")
		if strings.Contains(r.stdout, "is not in PATH") || strings.Contains(r.stderr, "comes first in PATH") {
			t.Fatalf("PATH message although it is first in PATH\n%s", r)
		}
	})
}

// TestConcurrentRuns: runs at the same time each leave a complete binary; the
// last rename wins.
func TestConcurrentRuns(t *testing.T) {
	t.Parallel()
	e, _ := testEnv(t, toolset{})
	tags := []string{"v2.0.1", "v2.0.0", "v1.1.7", "v2.0.1", "v2.0.0", "v1.1.7"}
	results := make([]result, len(tags))
	var wg sync.WaitGroup
	for i, tag := range tags {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = e.run("--version", tag)
		}()
	}
	wg.Wait()
	for _, r := range results {
		if r.code != 0 {
			t.Fatalf("a concurrent run failed\n%s", r)
		}
	}
	target := filepath.Join(e.defaultDir(), "ark")
	got := stateOf(t, target)
	ok := false
	for _, tag := range tags {
		ok = ok || bytes.Equal(got.data, e.wantBinary(tag))
	}
	if !ok || got.mode.Perm() != 0o755 {
		t.Fatalf("after concurrent runs %s is %q (%v)", target, got.data, got.mode)
	}
	e.checkClean(results[0], e.defaultDir())
}

// TestInterrupted: a run stopped by a signal while downloading changes
// nothing and removes its temporary files.
func TestInterrupted(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			t.Parallel()
			e, gh := testEnv(t, toolset{})
			target, before := withInstalled(t, e)
			p := selfProject
			gh.setFault(gh.assetPath(p, "v2.0.1", p.archiveName("v2.0.1", "linux", "amd64")), fault{stall: true})
			cmd := e.command(t.Context(), "--upgrade")
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			// its own process group, so the signal reaches curl too, as from a terminal
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			// wait until the archive download has started (no sleep: poll the server)
			deadline := time.Now().Add(30 * time.Second)
			for {
				reqs, _ := gh.snapshot()
				started := false
				for _, q := range reqs {
					started = started || (strings.HasPrefix(q, assetHost) && strings.HasSuffix(q, ".tar.gz"))
				}
				if started {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("the download did not start\n%s", out.String())
				}
				time.Sleep(10 * time.Millisecond)
			}
			_ = syscall.Kill(-cmd.Process.Pid, sig) // it may have exited already
			err := cmd.Wait()
			if err == nil {
				t.Fatalf("an interrupted run succeeded\n%s", out.String())
			}
			r := result{code: -2, stdout: out.String()}
			e.checkUnchanged(r, target, before)
			e.checkClean(r, e.defaultDir())
		})
	}
}

// TestRealReleases installs the real releases of ark from github.com, with
// each downloader. It needs the network, so it runs only with
// ARK_INSTALLER_NETWORK=1.
func TestRealReleases(t *testing.T) {
	if os.Getenv("ARK_INSTALLER_NETWORK") != "1" {
		t.Skip("set ARK_INSTALLER_NETWORK=1 to install the real releases from github.com")
	}
	sh := pathShell(t)
	for _, dl := range downloaders(t) {
		for _, c := range []struct {
			p   project
			old string
		}{{selfProject, "v6.5.0"}} {
			t.Run(c.p.app+"/"+dl, func(t *testing.T) {
				e := newEnv(t, nil, c.p, sh, toolset{downloader: dl, real: true})
				run := func(args ...string) string {
					cmd := exec.Command(sh.argv[0], append(append(slices.Clone(sh.argv[1:]), e.script), args...)...)
					cmd.Env = []string{"PATH=" + e.bin, "HOME=" + e.home, "TMPDIR=" + e.tmp}
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("%v: %v\n%s", args, err, out)
					}
					return string(out)
				}
				t.Log(run())
				t.Log(run("--version", c.old))
				t.Log(run("--upgrade"))
				bin := filepath.Join(e.defaultDir(), c.p.app)
				out, err := exec.Command(bin, "--version").Output()
				if err != nil || !strings.HasPrefix(string(out), c.p.app+" version v") {
					t.Fatalf("%s --version: %v %q", bin, err, out)
				}
				t.Log(strings.TrimSpace(string(out)))
				e.checkClean(result{}, e.defaultDir())
			})
		}
	}
}

// TestReadmeCommands: the install.sh commands of the README run as written
// (in order, in one home directory).
func TestReadmeCommands(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	// and the translated README, when there is one
	if ja, err := os.ReadFile(filepath.Join("..", "..", "README_ja.md")); err == nil {
		b = append(append(b, '\n'), ja...)
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	var cmds []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "  #"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if strings.HasPrefix(line, "sh install.sh") {
			cmds = append(cmds, line)
		}
	}
	if len(cmds) < 6 {
		t.Fatalf("README: only %d install.sh commands found: %q", len(cmds), cmds)
	}
	// the versions the README names are releases of the fake github.com too
	p := selfProject
	p.tags = slices.Clone(p.tags)
	for _, c := range cmds {
		f := strings.Fields(c)
		for i, w := range f {
			v := strings.TrimPrefix(w, "--version=")
			if w == "--version" && i+1 < len(f) {
				v = f[i+1]
			} else if v == w {
				continue
			}
			if !strings.HasPrefix(v, "v") {
				v = "v" + v
			}
			if !slices.Contains(p.tags, v) {
				p.tags = append([]string{v}, p.tags...)
			}
		}
	}
	gh := newFakeGitHub(t)
	gh.publish(t, p)
	e := newEnv(t, gh, p, pathShell(t), toolset{})
	for _, c := range cmds {
		// run the README line itself with the shell, install.sh being the test copy
		line := strings.Replace(c, "sh install.sh", `"$SH" "$SCRIPT"`, 1)
		cmd := exec.Command(e.shell.argv[0], append(slices.Clone(e.shell.argv[1:]), "-c", line)...)
		cmd.Env = append(e.environ(), "SCRIPT="+e.script, "SH="+e.shell.argv[0])
		cmd.Dir = e.home
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("README: %s: %v\n%s", c, err, out)
		}
	}
	e.checkClean(result{}, e.defaultDir())
}

// TestProbeAppletsFirst: the probe tells a shell that runs the commands of
// PATH from one that does not (here a "shell" that resets PATH, standing in
// for a BusyBox that runs its own applets first).
func TestProbeAppletsFirst(t *testing.T) {
	if probeAppletsFirst(t, []string{"/bin/sh"}) {
		t.Fatal("/bin/sh reported as running its own commands before PATH")
	}
	own := filepath.Join(t.TempDir(), "own-sh")
	writeExecutable(t, own, []byte("#!/bin/sh\nPATH=/usr/bin:/bin\nexport PATH\nexec /bin/sh \"$@\"\n"))
	if !probeAppletsFirst(t, []string{own}) {
		t.Fatal("a shell that ignores PATH reported as running the commands of PATH")
	}
	for _, sh := range testShells(t) {
		t.Logf("%s (%v): runs its own commands first: %v", sh.name, sh.argv, sh.appletsFirst)
	}
	if bb, ok := busyboxPath(t); ok {
		t.Logf("busybox sh (%s): runs its own commands first: %v", bb, probeAppletsFirst(t, []string{bb, "sh"}))
	}
}
