package share

import "testing"

func TestNormalizeTarget(t *testing.T) {
	tests := map[string]string{
		"localhost:3000":             "http://localhost:3000",
		"dev-1.vpsbox.local":         "https://dev-1.vpsbox.local",
		"http://127.0.0.1:8080":      "http://127.0.0.1:8080",
		"https://myapp.example.test": "https://myapp.example.test",
	}

	for in, want := range tests {
		got, err := NormalizeTarget(in)
		if err != nil {
			t.Fatalf("NormalizeTarget(%q) returned error: %v", in, err)
		}
		if got != want {
			t.Fatalf("NormalizeTarget(%q) = %q, want %q", in, got, want)
		}
	}
}
