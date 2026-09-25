package desktopbackend

import "testing"

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name    string
		latest  string
		current string
		want    bool
	}{
		{name: "major", latest: "2.0.0", current: "1.9.9", want: true},
		{name: "minor", latest: "1.3.0", current: "1.2.9", want: true},
		{name: "patch", latest: "v1.2.4", current: "1.2.3", want: true},
		{name: "same", latest: "1.2.3", current: "1.2.3", want: false},
		{name: "older", latest: "1.1.9", current: "1.2.0", want: false},
		{name: "development suffix", latest: "1.0.1", current: "1.0.0-dev", want: true},
		{name: "invalid", latest: "latest", current: "1.0.0", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isNewer(test.latest, test.current); got != test.want {
				t.Fatalf("isNewer(%q, %q) = %v, want %v", test.latest, test.current, got, test.want)
			}
		})
	}
}
