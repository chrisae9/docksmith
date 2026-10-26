package notify

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		cur, latest string
		want        Level
		ok          bool
	}{
		{"4.0.1-ls200", "4.0.1-ls201", LevelBuild, true},
		{"1.2.3", "1.2.4", LevelPatch, true},
		{"1.2.3", "1.3.0", LevelMinor, true},
		{"16.1.0", "17.0.0", LevelMajor, true},
		{"1.2.3", "1.2.3", 0, false},
		{"1.2.3", "1.2.2", 0, false},
		{"latest", "1.2.3", 0, false},
	}
	for _, tt := range tests {
		got, ok := Classify(tt.cur, tt.latest)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Classify(%q,%q) = (%v,%v), want (%v,%v)", tt.cur, tt.latest, got, ok, tt.want, tt.ok)
		}
	}
}

// For this tag pair the parser/comparator yields no usable classification
// (Classify returns ok=false), so notifications fall back to digest-only handling.
func TestClassify_ComplexTag(t *testing.T) {
	got, ok := Classify("14-vectorchord0.4.3-pgvectors0.2.0", "14-vectorchord0.4.4-pgvectors0.2.0")
	t.Logf("complex tag classify: (%v,%v)", got, ok)
	if ok || got != 0 {
		t.Errorf("got (%v,%v), want (%v,%v)", got, ok, Level(0), false)
	}
}

func TestParseLevel(t *testing.T) {
	for _, s := range []string{"build", "patch", "minor", "major"} {
		l, err := ParseLevel(s)
		if err != nil || l.String() != s {
			t.Errorf("ParseLevel(%q) = %v, %v", s, l, err)
		}
	}
	if _, err := ParseLevel("nope"); err == nil {
		t.Error("expected error")
	}
}
