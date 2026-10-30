//go:build !windows

package installer

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDistArchives runs install.sh against the archives a release build wrote
// (`make snapshot`, or the release workflow's build job), served by the fake
// github.com exactly as files of one GitHub release: the real ark_checksums.txt
// and the real archives, with the real ark binary for this machine. It checks
// that what Hephaestus builds follows the distribution contract of install.sh
// (docs/installer.md). ARK_INSTALLER_DIST names the dist directory; without
// it the test is skipped (CI sets it in the job that builds the snapshot).
func TestDistArchives(t *testing.T) {
	dist := os.Getenv("ARK_INSTALLER_DIST")
	if dist == "" {
		t.Skip("set ARK_INSTALLER_DIST to the dist directory of a release build (make snapshot)")
	}
	sums, err := os.ReadFile(filepath.Join(dist, "ark_checksums.txt"))
	if err != nil {
		t.Fatalf("ARK_INSTALLER_DIST=%s: %v", dist, err)
	}

	// every archive the checksum file lists, and the version they carry
	nameRE := regexp.MustCompile(`^ark_([0-9][0-9A-Za-z.-]*)_(darwin|linux|windows)_(386|amd64|arm64)\.(tar\.gz|zip)$`)
	version := ""
	var archives []string
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "  ")
		if len(f) != 2 || len(f[0]) != 64 {
			t.Fatalf("ark_checksums.txt: malformed line %q", sc.Text())
		}
		m := nameRE.FindStringSubmatch(f[1])
		if m == nil {
			t.Fatalf("ark_checksums.txt lists %q, which is not ark_<version>_<os>_<arch>.<tar.gz|zip>", f[1])
		}
		if version != "" && m[1] != version {
			t.Fatalf("ark_checksums.txt lists archives of two versions: %s and %s", version, m[1])
		}
		version = m[1]
		archives = append(archives, f[1])
	}
	if len(archives) == 0 {
		t.Fatal("ark_checksums.txt lists no archive")
	}
	tag := "v" + version

	// one release on the fake github.com, latest, with exactly these files
	gh := newFakeGitHub(t)
	p := selfProject
	p.tags, p.latest = []string{tag}, tag
	for _, name := range append([]string{"ark_checksums.txt"}, archives...) {
		data, err := os.ReadFile(filepath.Join(dist, name))
		if err != nil {
			t.Fatal(err)
		}
		gh.setAsset(p, tag, name, data)
	}
	gh.mu.Lock()
	gh.latest["/"+p.owner+"/"+p.repo] = tag
	gh.mu.Unlock()

	// install.sh runs the binary it installs: use this machine's platform
	e := newEnv(t, gh, p, pathShell(t), toolset{uname: hostUname(t)})
	target := filepath.Join(e.defaultDir(), "ark")
	wantArchive := "ark_" + version + "_" + e.goos() + "_" + e.goarch() + ".tar.gz"

	check := func(r result, want string) {
		t.Helper()
		checkOK(t, r, want)
		if !strings.Contains(r.stdout, "Verified the SHA-256 of "+wantArchive) {
			t.Fatalf("install.sh did not verify %s\n%s", wantArchive, r)
		}
		out, err := exec.Command(target, "--version").Output()
		if err != nil {
			t.Fatalf("%s --version: %v\n%s", target, err, r)
		}
		if got := strings.TrimSpace(string(out)); got != "ark version "+tag {
			t.Fatalf("%s --version printed %q, want %q", target, got, "ark version "+tag)
		}
		fi, err := os.Lstat(target)
		if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
			t.Fatalf("%s is not an executable regular file: %v %v", target, fi, err)
		}
		e.checkClean(r, e.defaultDir())
	}

	// the latest release (through the releases/latest redirect) ...
	check(e.run(), "Installed ark "+tag)
	// ... the same release named explicitly: nothing to change ...
	r := e.run("--version", tag)
	checkOK(t, r, "ark "+tag+" is already installed")
	// ... and a broken archive is refused (the checksum is checked)
	gh.setAsset(p, tag, wantArchive, []byte("not the archive"))
	before := stateOf(t, target)
	r = e.run("--version", tag)
	checkFails(t, r, 1, "SHA-256 mismatch for "+wantArchive)
	e.checkUnchanged(r, target, before)
}
