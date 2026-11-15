// Package buildtags computes the build tags netdefense-connect is released
// with. The tunnel library's dependency registers every optional feature it
// has and a tag that leaves each one out; the companion needs three of them,
// so every other one is left out. That takes about a megabyte off the binary
// and, more to the point, keeps code it never runs (an SSH server, a port
// mapper, captive-portal detection, remote logging) out of it.
package buildtags

import (
	"slices"
	"strings"

	"tailscale.com/feature/featuretags"
)

// keep lists the features the companion links: the userspace network stack
// its connections run on, receive offload for throughput on Linux, and a
// built-in set of CA roots so the relay's certificate still verifies on a
// machine whose own store is missing.
var keep = []featuretags.FeatureTag{
	"netstack",
	"gro",
	"bakedroots",
}

// Release returns the comma-separated value of -tags for a release build,
// sorted so one source tree always gives the same list.
func Release() string {
	kept := map[featuretags.FeatureTag]bool{}
	for _, feature := range keep {
		for dependency := range featuretags.Requires(feature) {
			kept[dependency] = true
		}
	}
	var tags []string
	for feature := range featuretags.Features {
		if feature == "" || !feature.IsOmittable() || kept[feature] {
			continue
		}
		tags = append(tags, feature.OmitTag())
	}
	slices.Sort(tags)
	return strings.Join(tags, ",")
}
