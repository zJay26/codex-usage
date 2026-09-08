package updater

import "testing"

func TestReleaseAssetsIncludeBothMacArchitectures(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		if got := AssetName("darwin", arch); got != "codex-usage-darwin-"+arch {
			t.Fatalf("Darwin asset=%s", got)
		}
	}
	if AssetName("darwin", "386") != "" {
		t.Fatal("unsupported Darwin architecture accepted")
	}
}
