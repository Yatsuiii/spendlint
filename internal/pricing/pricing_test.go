package pricing

import "testing"

func TestLookupNormalizes(t *testing.T) {
	cases := []string{
		"claude-3-haiku",
		"Claude-3-Haiku",
		"anthropic/claude-3-haiku",
		"claude-3-5-sonnet-20241022",
	}
	for _, in := range cases {
		r, ok := Lookup(in)
		if !ok {
			t.Fatalf("Lookup(%q) not found", in)
		}
		if r.Provider != "anthropic" {
			t.Errorf("Lookup(%q) provider = %q, want anthropic", in, r.Provider)
		}
	}
	if _, ok := Lookup("no-such-model"); ok {
		t.Error("Lookup(no-such-model) = ok, want not found")
	}
}

func TestCost(t *testing.T) {
	// 1M in + 1M out on haiku = its per-M rates summed.
	got, ok := Cost("claude-3-haiku", 1_000_000, 1_000_000)
	if !ok {
		t.Fatal("haiku not priced")
	}
	if want := 0.25 + 1.25; got != want {
		t.Errorf("Cost = %v, want %v", got, want)
	}
}

// The gold-path swap must be ~12x per token; this guards the pricing rows that
// make the demo land.
func TestHaikuToSonnetIsAbout12x(t *testing.T) {
	const in, out = 1397, 319
	haiku, _ := Cost("claude-3-haiku", in, out)
	sonnet, _ := Cost("claude-3-5-sonnet", in, out)
	ratio := sonnet / haiku
	if ratio < 11 || ratio > 13 {
		t.Errorf("sonnet/haiku ratio = %.2f, want ~12", ratio)
	}
}
