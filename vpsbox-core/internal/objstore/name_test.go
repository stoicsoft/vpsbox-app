package objstore

import "testing"

func TestValidateBucketNameAccepts(t *testing.T) {
	valid := []string{
		"backups",
		"my-backups",
		"db1",
		"a1b",
		"pg-dumps-2024",
		"aaa",
		"a-very-long-but-still-legal-bucket-name-under-sixty-three-chars",
	}

	for _, name := range valid {
		if err := ValidateBucketName(name); err != nil {
			t.Errorf("ValidateBucketName(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateBucketNameRejects(t *testing.T) {
	cases := []struct {
		name   string
		bucket string
	}{
		{"empty", ""},
		{"too short", "ab"},
		{"too long", "this-bucket-name-is-far-too-long-to-be-accepted-by-any-s3-implementation"},
		{"uppercase", "Backups"},
		{"underscore", "my_backups"},
		{"space", "my backups"},
		{"dot", "my.backups"},
		{"leading hyphen", "-backups"},
		{"trailing hyphen", "backups-"},
		{"ip address", "192.168.1.1"},
		{"reserved prefix", "xn--backups"},
		{"reserved suffix", "backups-s3alias"},
		{"slash", "backups/db"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateBucketName(tc.bucket); err == nil {
				t.Errorf("ValidateBucketName(%q) = nil, want an error", tc.bucket)
			}
		})
	}
}

func TestValidateBucketNameLengthBoundaries(t *testing.T) {
	if err := ValidateBucketName("abc"); err != nil {
		t.Errorf("3 characters should be legal: %v", err)
	}

	sixtyThree := ""
	for len(sixtyThree) < 63 {
		sixtyThree += "a"
	}
	if err := ValidateBucketName(sixtyThree); err != nil {
		t.Errorf("63 characters should be legal: %v", err)
	}
	if err := ValidateBucketName(sixtyThree + "a"); err == nil {
		t.Error("64 characters should be rejected")
	}
}

func TestGenerateCredentialsAreUniqueAndLongEnough(t *testing.T) {
	firstAccess, firstSecret, err := generateCredentials()
	if err != nil {
		t.Fatalf("generate credentials: %v", err)
	}
	secondAccess, secondSecret, err := generateCredentials()
	if err != nil {
		t.Fatalf("generate credentials: %v", err)
	}

	if firstAccess == secondAccess || firstSecret == secondSecret {
		t.Error("credentials repeated across calls")
	}
	// The endpoint listens on all interfaces, so the secret is the only barrier.
	if len(firstSecret) < 32 {
		t.Errorf("secret key length = %d, want at least 32", len(firstSecret))
	}
	// MinIO rejects a root user shorter than 3 characters.
	if len(firstAccess) < 3 {
		t.Errorf("access key length = %d, want at least 3", len(firstAccess))
	}
}
