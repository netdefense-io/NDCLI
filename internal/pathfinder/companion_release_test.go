package pathfinder

import (
	"errors"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// releaseConfig is the part of a GoReleaser configuration these tests read.
type releaseConfig struct {
	Builds []struct {
		ID      string              `yaml:"id"`
		Dir     string              `yaml:"dir"`
		Binary  string              `yaml:"binary"`
		Env     []string            `yaml:"env"`
		Goos    []string            `yaml:"goos"`
		Goarch  []string            `yaml:"goarch"`
		Ignore  []map[string]string `yaml:"ignore"`
		Flags   []string            `yaml:"flags"`
		Ldflags []string            `yaml:"ldflags"`
	} `yaml:"builds"`
	Archives []struct {
		Builds []string `yaml:"builds"`
		Files  []string `yaml:"files"`
	} `yaml:"archives"`
	Brews []struct {
		Install string `yaml:"install"`
	} `yaml:"brews"`
}

// readRelease reads a file of the release pipeline. The public mirror
// carries none of them, and the test is skipped there.
func readRelease(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("%s is not in this tree", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The launcher finds the companion beside the binary that starts it and
// refuses one whose version is not its own. A release therefore has to put
// the companion in every archive, build it for every platform the others are
// built for, and stamp it with the same version.
func TestAReleaseShipsTheCompanionWithEveryBinary(t *testing.T) {
	for _, path := range []string{"../../.goreleaser.yaml", "../../.goreleaser.dev.yaml"} {
		t.Run(path, func(t *testing.T) {
			var cfg releaseConfig
			if err := yaml.Unmarshal(readRelease(t, path), &cfg); err != nil {
				t.Fatal(err)
			}
			builds := map[string]int{}
			for i, b := range cfg.Builds {
				builds[b.ID] = i
			}
			for _, id := range []string{"ndcli", "netdefense-mcp", "netdefense", companionName} {
				if _, ok := builds[id]; !ok {
					t.Fatalf("no build %q", id)
				}
			}
			main, companion := cfg.Builds[builds["ndcli"]], cfg.Builds[builds[companionName]]

			if companion.Dir != companionName || companion.Binary != companionName {
				t.Errorf("the companion is built in %q as %q; it is a module in %q and is looked up as %q", companion.Dir, companion.Binary, companionName, companionName)
			}
			if !reflect.DeepEqual(companion.Goos, main.Goos) || !reflect.DeepEqual(companion.Goarch, main.Goarch) || !reflect.DeepEqual(companion.Ignore, main.Ignore) {
				t.Errorf("the companion is built for %v/%v (but %v) and ndcli for %v/%v (but %v): an archive would lack one of them",
					companion.Goos, companion.Goarch, companion.Ignore, main.Goos, main.Goarch, main.Ignore)
			}
			if !slices.Contains(companion.Env, "CGO_ENABLED=0") || !slices.Contains(companion.Flags, "-trimpath") {
				t.Errorf("the companion is not built as the Makefile and CI build it: env %v, flags %v", companion.Env, companion.Flags)
			}
			if !slices.Contains(companion.Flags, "-tags={{ .Env.NETDEFENSE_CONNECT_TAGS }}") {
				t.Errorf("the companion is built without its module's tags: flags %v", companion.Flags)
			}
			const version = "{{.Version}}"
			if !slices.Contains(main.Ldflags, "-X github.com/netdefense-io/NDCLI/internal/config.Version="+version) ||
				!slices.Contains(companion.Ldflags, "-X main.version="+version) {
				t.Errorf("ndcli and its companion are not stamped with the same version: %v and %v", main.Ldflags, companion.Ldflags)
			}

			if len(cfg.Archives) != 1 {
				t.Fatalf("%d archives, want one", len(cfg.Archives))
			}
			for _, id := range []string{"ndcli", "netdefense-mcp", "netdefense", companionName} {
				if !slices.Contains(cfg.Archives[0].Builds, id) {
					t.Errorf("the archive lacks %s", id)
				}
			}
			if !slices.Contains(cfg.Archives[0].Files, "THIRD_PARTY_NOTICES") {
				t.Errorf("the archive lacks THIRD_PARTY_NOTICES: files %v", cfg.Archives[0].Files)
			}
			if len(cfg.Brews) != 1 || !strings.Contains(cfg.Brews[0].Install, `bin.install "`+companionName+`"`) || !strings.Contains(cfg.Brews[0].Install, `"THIRD_PARTY_NOTICES"`) {
				t.Errorf("the Homebrew formula does not install the companion and the notices")
			}
		})
	}
}

// GoReleaser cannot read the companion's tags from a file, so each workflow
// that runs it reads them into the variable the configuration names, first.
func TestTheReleaseWorkflowsReadTheCompanionsTags(t *testing.T) {
	for _, path := range []string{"../../.github/workflows/release-please.yml", "../../.github/workflows/dev-release.yml"} {
		workflow := string(readRelease(t, path))
		read := strings.Index(workflow, `tags="$(cat netdefense-connect/build-tags.txt)"`)
		export := strings.Index(workflow, `echo "NETDEFENSE_CONNECT_TAGS=$tags" >> "$GITHUB_ENV"`)
		release := strings.Index(workflow, "goreleaser/goreleaser-action@")
		if read < 0 || export < read || release < export {
			t.Errorf("%s does not read the companion's tags into NETDEFENSE_CONNECT_TAGS before it runs GoReleaser", path)
		}
	}
}

// What a release needs and the public mirror builds from has to be on the
// mirror's list, or it is missing there without a word.
func TestTheMirrorAndTheBundleCarryTheCompanion(t *testing.T) {
	workflow := string(readRelease(t, "../../.github/workflows/release-please.yml"))
	for _, entry := range []string{`"` + companionName + `:` + companionName + `"`, `"THIRD_PARTY_NOTICES:THIRD_PARTY_NOTICES"`} {
		if !strings.Contains(workflow, entry) {
			t.Errorf("the public mirror's list lacks %s", entry)
		}
	}
	bundle, err := os.ReadFile("../../scripts/build-mcpb.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		`"$WORKDIR/server/darwin/` + companionName + `"`,
		`"$WORKDIR/server/linux/` + companionName + `"`,
		`"$WORKDIR/server/win32/` + companionName + `.exe"`,
		`"$WORKDIR/THIRD_PARTY_NOTICES"`,
	} {
		if !strings.Contains(string(bundle), line) {
			t.Errorf("the MCP bundle is built without %s", line)
		}
	}
}
