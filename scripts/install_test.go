package scripts_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerMapsSupportedPlatformsAndInstalls(t *testing.T) {
	for _, test := range []struct {
		name, unameS, unameM, platform string
	}{
		{"linux amd64", "Linux", "x86_64", "linux-amd64"},
		{"linux arm64", "Linux", "aarch64", "linux-arm64"},
		{"darwin amd64", "Darwin", "amd64", "darwin-amd64"},
		{"darwin arm64", "Darwin", "arm64", "darwin-arm64"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newInstallerFixture(t, test.unameS, test.unameM, "amux-"+test.platform+".tar.gz")
			output, err := fixture.run()
			if err != nil {
				t.Fatalf("installer failed: %v\n%s", err, output)
			}
			installed, err := os.ReadFile(fixture.installPath())
			if err != nil || string(installed) != fixture.binary {
				t.Fatalf("installed binary = %q, err=%v", installed, err)
			}
			if mode := fileMode(t, fixture.installPath()); mode&0o111 == 0 {
				t.Fatalf("installed mode %v is not executable", mode)
			}
			requests := readFile(t, fixture.log)
			if !strings.Contains(requests, "amux-"+test.platform+".tar.gz\n") || !strings.Contains(requests, "amux-"+test.platform+".tar.gz.sha256\n") {
				t.Fatalf("download requests:\n%s", requests)
			}
			if !strings.Contains(output, fixture.installPath()+" install doctor") {
				t.Fatalf("success output did not provide doctor command:\n%s", output)
			}
		})
	}
}

func TestInstallerRejectsUnsupportedPlatform(t *testing.T) {
	for _, test := range []struct{ unameS, unameM, want string }{
		{"FreeBSD", "x86_64", "unsupported operating system: FreeBSD"},
		{"Linux", "riscv64", "unsupported architecture: riscv64"},
	} {
		fixture := newInstallerFixture(t, test.unameS, test.unameM, "amux-linux-amd64.tar.gz")
		output, err := fixture.run()
		if err == nil || !strings.Contains(output, test.want) {
			t.Fatalf("output=%q err=%v, want %q", output, err, test.want)
		}
	}
}

func TestInstallerVersionOverrideUsesVersionedAssets(t *testing.T) {
	fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-v1.2.3-linux-amd64.tar.gz")
	fixture.env = append(fixture.env, "AMUX_VERSION=v1.2.3")
	if output, err := fixture.run(); err != nil {
		t.Fatalf("installer failed: %v\n%s", err, output)
	}
	requests := readFile(t, fixture.log)
	if !strings.Contains(requests, "/releases/download/v1.2.3/amux-v1.2.3-linux-amd64.tar.gz\n") {
		t.Fatalf("versioned request missing:\n%s", requests)
	}
}

func TestInstallerLinksBundledSkillsAndPreservesExistingInstalls(t *testing.T) {
	fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
	source := filepath.Join(fixture.root, "amux source")
	for _, skill := range []string{"amux", "amux-claude", "amux-pi"} {
		writeFile(t, filepath.Join(source, "skills", skill, "SKILL.md"), "name: "+skill+"\n", 0o644)
		writeFile(t, filepath.Join(fixture.home, ".agents", "skills", skill, "old"), "preserved\n", 0o600)
	}
	writeFile(t, filepath.Join(fixture.home, ".config", "amp", "skills", "amux", "old"), "legacy amp\n", 0o600)
	writeFile(t, filepath.Join(fixture.home, ".config", "agents", "skills", "amux-pi", "old"), "legacy agents\n", 0o600)
	fixture.env = append(fixture.env, "AMUX_SKILLS_SOURCE="+source)

	output, err := fixture.run()
	if err != nil {
		t.Fatalf("installer failed: %v\n%s", err, output)
	}
	for _, skill := range []string{"amux", "amux-claude", "amux-pi"} {
		destination := filepath.Join(fixture.home, ".agents", "skills", skill)
		info, statErr := os.Lstat(destination)
		if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("skill %s is not a symlink: mode=%v err=%v", skill, info, statErr)
		}
		target, readErr := os.Readlink(destination)
		if readErr != nil || target != filepath.Join(source, "skills", skill) {
			t.Fatalf("skill %s target=%q err=%v", skill, target, readErr)
		}
		backups, globErr := filepath.Glob(destination + ".backup-*")
		if globErr != nil || len(backups) != 1 || readFile(t, filepath.Join(backups[0], "old")) != "preserved\n" {
			t.Fatalf("skill %s backups=%v err=%v", skill, backups, globErr)
		}
	}
	for _, legacy := range []string{
		filepath.Join(fixture.home, ".config", "amp", "skills", "amux"),
		filepath.Join(fixture.home, ".config", "agents", "skills", "amux-pi"),
	} {
		if _, statErr := os.Lstat(legacy); !os.IsNotExist(statErr) {
			t.Fatalf("legacy skill remains active at %s: %v", legacy, statErr)
		}
		backups, globErr := filepath.Glob(legacy + ".backup-*")
		if globErr != nil || len(backups) != 1 {
			t.Fatalf("legacy backups=%v err=%v", backups, globErr)
		}
	}
	if !strings.Contains(output, "Linked bundled skills from "+source) || !strings.Contains(output, "Reload Amp") {
		t.Fatalf("skill installation output:\n%s", output)
	}
}

func TestInstallerSkillLinksAreIdempotent(t *testing.T) {
	fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
	source := filepath.Join(fixture.root, "amux-source")
	for _, skill := range []string{"amux", "amux-claude", "amux-pi"} {
		writeFile(t, filepath.Join(source, "skills", skill, "SKILL.md"), "name: "+skill+"\n", 0o644)
	}
	fixture.env = append(fixture.env, "AMUX_SKILLS_SOURCE="+source)
	if output, err := fixture.run(); err != nil {
		t.Fatalf("first installer run failed: %v\n%s", err, output)
	}
	output, err := fixture.run()
	if err != nil {
		t.Fatalf("second installer run failed: %v\n%s", err, output)
	}
	if strings.Count(output, "already links to") != 3 {
		t.Fatalf("idempotent output:\n%s", output)
	}
	backups, globErr := filepath.Glob(filepath.Join(fixture.home, ".agents", "skills", "*.backup-*"))
	if globErr != nil || len(backups) != 0 {
		t.Fatalf("idempotent install created backups=%v err=%v", backups, globErr)
	}
}

func TestInstallerReplacesWrongAndBrokenSkillSymlinks(t *testing.T) {
	fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
	source := filepath.Join(fixture.root, "amux-source")
	for _, skill := range []string{"amux", "amux-claude", "amux-pi"} {
		writeFile(t, filepath.Join(source, "skills", skill, "SKILL.md"), "name: "+skill+"\n", 0o644)
	}
	destinationRoot := filepath.Join(fixture.home, ".agents", "skills")
	if err := os.MkdirAll(destinationRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(fixture.root, "wrong"), filepath.Join(destinationRoot, "amux")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(fixture.root, "missing"), filepath.Join(destinationRoot, "amux-claude")); err != nil {
		t.Fatal(err)
	}
	fixture.env = append(fixture.env, "AMUX_SKILLS_SOURCE="+source)

	if output, err := fixture.run(); err != nil {
		t.Fatalf("installer failed: %v\n%s", err, output)
	}
	for _, skill := range []string{"amux", "amux-claude", "amux-pi"} {
		target, err := os.Readlink(filepath.Join(destinationRoot, skill))
		if err != nil || target != filepath.Join(source, "skills", skill) {
			t.Fatalf("skill %s target=%q err=%v", skill, target, err)
		}
	}
	backups, err := filepath.Glob(filepath.Join(destinationRoot, "*.backup-*"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("replaced symlinks created backups=%v err=%v", backups, err)
	}
}

func TestInstallerRejectsInvalidSkillSourceBeforeDownload(t *testing.T) {
	fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
	fixture.env = append(fixture.env, "AMUX_SKILLS_SOURCE=relative/source")
	output, err := fixture.run()
	if err == nil || !strings.Contains(output, "AMUX_SKILLS_SOURCE must be an absolute path") {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if _, statErr := os.Stat(fixture.log); !os.IsNotExist(statErr) {
		t.Fatalf("invalid skill source reached download: %v", statErr)
	}
}

func TestInstallerPreflightsEverySkillBeforeMutation(t *testing.T) {
	fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
	source := filepath.Join(fixture.root, "amux-source")
	for _, skill := range []string{"amux", "amux-claude"} {
		writeFile(t, filepath.Join(source, "skills", skill, "SKILL.md"), "name: "+skill+"\n", 0o644)
	}
	existing := filepath.Join(fixture.home, ".agents", "skills", "amux", "old")
	writeFile(t, existing, "preserved\n", 0o600)
	fixture.env = append(fixture.env, "AMUX_SKILLS_SOURCE="+source)

	output, err := fixture.run()
	if err == nil || !strings.Contains(output, "missing bundled skill directory") {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if got := readFile(t, existing); got != "preserved\n" {
		t.Fatalf("preflight changed existing skill to %q", got)
	}
	if _, statErr := os.Stat(fixture.log); !os.IsNotExist(statErr) {
		t.Fatalf("incomplete skill source reached download: %v", statErr)
	}
}

func TestInstallerRejectsSkillSourceOverlappingManagedDestinations(t *testing.T) {
	for _, relativeSource := range []string{
		filepath.Join(".agents", "skills", "amux"),
		filepath.Join(".config", "amp", "skills", "amux"),
	} {
		t.Run(relativeSource, func(t *testing.T) {
			fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
			source := filepath.Join(fixture.home, relativeSource)
			for _, skill := range []string{"amux", "amux-claude", "amux-pi"} {
				writeFile(t, filepath.Join(source, "skills", skill, "SKILL.md"), "name: "+skill+"\n", 0o644)
			}
			fixture.env = append(fixture.env, "AMUX_SKILLS_SOURCE="+source)

			output, err := fixture.run()
			if err == nil || !strings.Contains(output, "must not overlap") {
				t.Fatalf("output=%q err=%v", output, err)
			}
			if got := readFile(t, filepath.Join(source, "skills", "amux", "SKILL.md")); got != "name: amux\n" {
				t.Fatalf("overlap preflight changed source to %q", got)
			}
			if _, statErr := os.Stat(fixture.log); !os.IsNotExist(statErr) {
				t.Fatalf("overlap reached download: %v", statErr)
			}
		})
	}
}

func TestInstallerRejectsInvalidSkillDestinationParentsBeforeDownload(t *testing.T) {
	for _, relativeParent := range []string{".agents", filepath.Join(".agents", "skills")} {
		t.Run(relativeParent, func(t *testing.T) {
			fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
			source := filepath.Join(fixture.root, "amux-source")
			for _, skill := range []string{"amux", "amux-claude", "amux-pi"} {
				writeFile(t, filepath.Join(source, "skills", skill, "SKILL.md"), "name: "+skill+"\n", 0o644)
			}
			writeFile(t, filepath.Join(fixture.home, relativeParent), "not a directory\n", 0o600)
			fixture.env = append(fixture.env, "AMUX_SKILLS_SOURCE="+source)

			output, err := fixture.run()
			if err == nil || !strings.Contains(output, "is not a directory") {
				t.Fatalf("output=%q err=%v", output, err)
			}
			if _, statErr := os.Stat(fixture.log); !os.IsNotExist(statErr) {
				t.Fatalf("invalid destination parent reached download: %v", statErr)
			}
		})
	}
}

func TestInstallerWithoutSkillSourceDoesNotInspectAgentSkillPaths(t *testing.T) {
	fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
	writeFile(t, filepath.Join(fixture.home, ".agents"), "unrelated file\n", 0o600)
	if output, err := fixture.run(); err != nil {
		t.Fatalf("binary-only installer changed behavior: %v\n%s", err, output)
	}
}

func TestInstallerReportsSequentialSkillFailureAndPreservesBackup(t *testing.T) {
	fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
	source := filepath.Join(fixture.root, "amux-source")
	for _, skill := range []string{"amux", "amux-claude", "amux-pi"} {
		writeFile(t, filepath.Join(source, "skills", skill, "SKILL.md"), "name: "+skill+"\n", 0o644)
	}
	destination := filepath.Join(fixture.home, ".agents", "skills", "amux")
	writeFile(t, filepath.Join(destination, "old"), "recoverable\n", 0o600)
	writeFile(t, filepath.Join(fixture.bin, "ln"), "#!/bin/sh\nexit 1\n", 0o755)
	fixture.env = append(fixture.env, "AMUX_SKILLS_SOURCE="+source)

	output, err := fixture.run()
	if err == nil || !strings.Contains(output, "Preserved existing skill") || strings.Contains(output, "Linked bundled skills") {
		t.Fatalf("sequential failure output=%q err=%v", output, err)
	}
	backups, globErr := filepath.Glob(destination + ".backup-*")
	if globErr != nil || len(backups) != 1 || readFile(t, filepath.Join(backups[0], "old")) != "recoverable\n" {
		t.Fatalf("recoverable backups=%v err=%v", backups, globErr)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("failed link unexpectedly exists: %v", statErr)
	}
}

func TestInstallerChecksumFailurePreservesExistingBinary(t *testing.T) {
	fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
	writeFile(t, fixture.installPath(), "working old binary\n", 0o755)
	writeFile(t, filepath.Join(fixture.assets, fixture.archiveName+".sha256"), strings.Repeat("0", 64)+"  "+fixture.archiveName+"\n", 0o644)

	output, err := fixture.run()
	if err == nil || !strings.Contains(output, "checksum verification failed") {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if got := readFile(t, fixture.installPath()); got != "working old binary\n" {
		t.Fatalf("existing binary changed to %q", got)
	}
	fixture.assertNoInstallTemps(t)
}

func TestInstallerInterruptedStagingPreservesExistingBinaryAndCleansUp(t *testing.T) {
	fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
	writeFile(t, fixture.installPath(), "working old binary\n", 0o755)
	writeFile(t, filepath.Join(fixture.bin, "cp"), `#!/bin/sh
printf 'partial' >"$2"
kill -TERM "$PPID"
exit 1
`, 0o755)

	if output, err := fixture.run(); err == nil {
		t.Fatalf("interrupted installer succeeded:\n%s", output)
	}
	if got := readFile(t, fixture.installPath()); got != "working old binary\n" {
		t.Fatalf("existing binary changed to %q", got)
	}
	fixture.assertNoInstallTemps(t)
}

func TestInstallerExtractionFailurePreservesExistingBinary(t *testing.T) {
	fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
	writeFile(t, fixture.installPath(), "working old binary\n", 0o755)
	writeFile(t, filepath.Join(fixture.bin, "tar"), "#!/bin/sh\nexit 1\n", 0o755)

	output, err := fixture.run()
	if err == nil || !strings.Contains(output, "could not extract") {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if got := readFile(t, fixture.installPath()); got != "working old binary\n" {
		t.Fatalf("existing binary changed to %q", got)
	}
	fixture.assertNoInstallTemps(t)
}

func TestInstallerRejectsSymlinkedCanonicalParent(t *testing.T) {
	for _, parent := range []string{".local", filepath.Join(".local", "bin")} {
		t.Run(parent, func(t *testing.T) {
			fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
			referent := filepath.Join(fixture.root, "referent")
			if err := os.MkdirAll(referent, 0o755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(fixture.home, parent)
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(referent, link); err != nil {
				t.Fatal(err)
			}

			output, err := fixture.run()
			if err == nil || !strings.Contains(output, "must not contain symlinked") {
				t.Fatalf("output=%q err=%v", output, err)
			}
			if _, statErr := os.Stat(filepath.Join(referent, "amux")); !os.IsNotExist(statErr) {
				t.Fatalf("installer wrote through canonical-parent symlink: %v", statErr)
			}
		})
	}
}

func TestInstallerPATHMessages(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
		output, err := fixture.run()
		if err != nil || !strings.Contains(output, "Add "+filepath.Join(fixture.home, ".local", "bin")+" to PATH") {
			t.Fatalf("output=%q err=%v", output, err)
		}
	})

	t.Run("real duplicate shadows", func(t *testing.T) {
		fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
		shadow := filepath.Join(fixture.bin, "amux")
		writeFile(t, shadow, "#!/bin/sh\n", 0o755)
		installDir := filepath.Join(fixture.home, ".local", "bin")
		fixture.env[1] = "PATH=" + fixture.bin + ":" + installDir + ":/usr/bin:/bin"
		output, err := fixture.run()
		if err != nil || !strings.Contains(output, shadow+" currently shadows") {
			t.Fatalf("output=%q err=%v", output, err)
		}
	})

	t.Run("symlink alias is not shadowing", func(t *testing.T) {
		fixture := newInstallerFixture(t, "Linux", "x86_64", "amux-linux-amd64.tar.gz")
		installDir := filepath.Join(fixture.home, ".local", "bin")
		if err := os.MkdirAll(installDir, 0o755); err != nil {
			t.Fatal(err)
		}
		aliasDir := filepath.Join(fixture.root, "alias-bin")
		if err := os.MkdirAll(aliasDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(installDir, "amux"), filepath.Join(aliasDir, "amux")); err != nil {
			t.Fatal(err)
		}
		fixture.env[1] = "PATH=" + fixture.bin + ":" + aliasDir + ":" + installDir + ":/usr/bin:/bin"
		output, err := fixture.run()
		if err != nil || strings.Contains(output, "currently shadows") {
			t.Fatalf("output=%q err=%v", output, err)
		}
	})
}

type installerFixture struct {
	root, home, bin, assets, log, archiveName, binary string
	env                                               []string
}

func newInstallerFixture(t *testing.T, unameS, unameM, archiveName string) *installerFixture {
	t.Helper()
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	root = canonicalRoot
	f := &installerFixture{
		root: root, home: filepath.Join(root, "home"), bin: filepath.Join(root, "bin"),
		assets: filepath.Join(root, "assets"), log: filepath.Join(root, "curl.log"),
		archiveName: archiveName, binary: "#!/bin/sh\necho amux fixture\n",
	}
	for _, dir := range []string{f.home, f.bin, f.assets} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(f.bin, "uname"), fmt.Sprintf("#!/bin/sh\ncase \"$1\" in -s) printf '%%s\\n' %q ;; -m) printf '%%s\\n' %q ;; esac\n", unameS, unameM), 0o755)
	writeFile(t, filepath.Join(f.bin, "curl"), `#!/bin/sh
destination=
url=
while [ "$#" -gt 0 ]; do
	case "$1" in
		-o) destination=$2; shift 2 ;;
		*) url=$1; shift ;;
	esac
done
printf '%s\n' "$url" >>"$INSTALLER_LOG"
/bin/cp "$INSTALLER_ASSETS/${url##*/}" "$destination"
`, 0o755)
	f.writeArchive(t)
	f.env = []string{
		"HOME=" + f.home,
		"PATH=" + f.bin + ":/usr/bin:/bin",
		"TMPDIR=" + root,
		"INSTALLER_ASSETS=" + f.assets,
		"INSTALLER_LOG=" + f.log,
	}
	return f
}

func (f *installerFixture) writeArchive(t *testing.T) {
	t.Helper()
	dir := strings.TrimSuffix(f.archiveName, ".tar.gz")
	staging := filepath.Join(f.root, "staging")
	writeFile(t, filepath.Join(staging, dir, "amux"), f.binary, 0o755)
	archive := filepath.Join(f.assets, f.archiveName)
	cmd := exec.Command("tar", "-czf", archive, "-C", staging, dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create fixture archive: %v\n%s", err, output)
	}
	contents, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	writeFile(t, archive+".sha256", fmt.Sprintf("%x  %s\n", sum, f.archiveName), 0o644)
}

func (f *installerFixture) run() (string, error) {
	root, _ := os.Getwd()
	cmd := exec.Command("/bin/sh", filepath.Join(root, "..", "docs", "install.sh"))
	cmd.Dir = root
	cmd.Env = append([]string{}, f.env...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (f *installerFixture) installPath() string {
	return filepath.Join(f.home, ".local", "bin", "amux")
}

func (f *installerFixture) assertNoInstallTemps(t *testing.T) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(f.home, ".local", "bin", ".amux-install.*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary install files=%v err=%v", matches, err)
	}
}

func writeFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}
