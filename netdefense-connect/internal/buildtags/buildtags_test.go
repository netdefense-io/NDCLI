package buildtags

import (
	"os"
	"strings"
	"testing"
)

// build-tags.txt is what the Makefile and the release build read, since
// neither can run this package. It must be the list this package computes: a
// dependency update that adds a feature would otherwise link it in unnoticed.
func TestTheTagFileIsCurrent(t *testing.T) {
	file, err := os.ReadFile("../../build-tags.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(file)), Release(); got != want {
		t.Errorf("build-tags.txt is out of date; regenerate it with\n\tgo run ./internal/buildtags/printtags > build-tags.txt\ngot:  %s\nwant: %s", got, want)
	}
	if strings.Count(string(file), "\n") != 1 || !strings.HasSuffix(string(file), "\n") {
		t.Error("build-tags.txt must be one line")
	}
}

func TestTheKeptFeaturesAreNotLeftOut(t *testing.T) {
	tags := "," + Release() + ","
	for _, feature := range keep {
		if strings.Contains(tags, ","+feature.OmitTag()+",") {
			t.Errorf("%s is needed and its tag %s leaves it out", feature, feature.OmitTag())
		}
	}
	for _, left := range []string{"ts_omit_ssh", "ts_omit_portmapper", "ts_omit_captiveportal", "ts_omit_logtail", "ts_omit_useproxy", "ts_omit_webclient"} {
		if !strings.Contains(tags, ","+left+",") {
			t.Errorf("%s is missing from the release tags", left)
		}
	}
}
