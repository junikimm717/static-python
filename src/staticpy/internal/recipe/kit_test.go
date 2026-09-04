package recipe

import (
	"strings"
	"testing"
)

func TestPlanKitSmokeFansInPacks(t *testing.T) {
	cfg := loadEmbedded(t)
	bindFakeToolchain(t, testTriple)
	jobs, err := Plan(cfg, defaultsAssets(t), PlanOptions{
		Kit:     "smoke",
		Host:    testTriple,
		Targets: []string{testTriple},
		Verify:  "core",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Name() != "kit" {
		t.Fatalf("roots = %v, want one kit job", slugs(jobs))
	}
	if jobs[0].Slug() != "kit:smoke:"+testTriple {
		t.Fatalf("slug = %s", jobs[0].Slug())
	}
	want := map[string]bool{
		"pack:default:" + testTriple:   false,
		"pack:reference:" + testTriple: false,
	}
	for _, d := range jobs[0].Deps() {
		if _, ok := want[d.Slug()]; ok {
			want[d.Slug()] = true
		}
	}
	for slug, found := range want {
		if !found {
			t.Errorf("kit deps missing %s (have %s)", slug, strings.Join(slugs(jobs[0].Deps()), " "))
		}
	}
}

func TestKitKeyInputsHashVendor(t *testing.T) {
	cfg := loadEmbedded(t)
	bindFakeToolchain(t, testTriple)
	jobs, err := Plan(cfg, defaultsAssets(t), PlanOptions{
		Kit:     "smoke",
		Host:    testTriple,
		Targets: []string{testTriple},
	})
	if err != nil {
		t.Fatal(err)
	}
	in := jobs[0].KeyInputs()
	st := cfg.Bench.Vendor["setuptools"]
	want := "setuptools=" + st.File + ":" + st.SHA256
	if !strings.Contains(in["vendor"], want) {
		t.Fatalf("vendor key %q does not contain %q", in["vendor"], want)
	}
}
