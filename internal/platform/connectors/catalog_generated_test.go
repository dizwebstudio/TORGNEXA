package connectors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

func TestGeneratedCatalogMatchesConnectorManifests(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "connectors")
	paths, err := filepath.Glob(filepath.Join(root, "*", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	nested, err := filepath.Glob(filepath.Join(root, "*", "*", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, nested...)
	want := make([]Manifest, 0, len(paths))
	for _, path := range paths {
		data, readErr := os.ReadFile(path) // #nosec G304 -- paths come from a fixed repository glob.
		if readErr != nil {
			t.Fatal(readErr)
		}
		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		want = append(want, manifest.Canonical())
	}
	sort.Slice(want, func(i, j int) bool { return want[i].ID < want[j].ID })
	got, err := CatalogManifests()
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].ID < got[j].ID })
	if !reflect.DeepEqual(got, want) {
		t.Fatal("generated connector catalog drifted from manifests")
	}
}
