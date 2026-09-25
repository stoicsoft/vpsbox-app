package desktopbackend

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateSigningKeyDecodes(t *testing.T) {
	if len(updatePublicKey) != ed25519.PublicKeySize {
		t.Fatalf("updatePublicKey has %d bytes, want %d", len(updatePublicKey), ed25519.PublicKeySize)
	}
}

func TestValidReleaseVersion(t *testing.T) {
	for _, version := range []string{"1.5.0", "10.20.30", "1.5.0-beta.1", "2.0.0-rc1"} {
		if !validReleaseVersion(version) {
			t.Fatalf("validReleaseVersion(%q) = false, want true", version)
		}
	}
	// parseSemver alone accepted every one of these: it drops everything
	// after '-' before checking, and the rest went into the helper scripts.
	for _, version := range []string{
		"",
		"1.5",
		"v1.5.0",
		"1.5.0-$(id)",
		"1.5.0-`id`",
		"1.5.0-x;reboot",
		"1.5.0-a|b",
		"1.5.0-&calc",
		"1.5.0-%PATH%",
		"1.5.0-a/../b",
		"1.5.0 ",
		"1.5.0-",
	} {
		if validReleaseVersion(version) {
			t.Fatalf("validReleaseVersion(%q) = true, want false", version)
		}
	}
}

func TestFindReleaseChecksums(t *testing.T) {
	assets := []releaseAsset{
		{Name: "VPSBox-1-5-0-macos.zip", URL: "https://example.test/mac.zip"},
		{Name: "SHA256SUMS", URL: "https://example.test/sums"},
		{Name: "SHA256SUMS.sig", URL: "https://example.test/sums.sig"},
	}
	found, ok := findReleaseChecksums(assets)
	if !ok || found.SumsURL != "https://example.test/sums" || found.SignatureURL != "https://example.test/sums.sig" {
		t.Fatalf("findReleaseChecksums() = %+v, %v", found, ok)
	}
	if _, ok := findReleaseChecksums(assets[:2]); ok {
		t.Fatal("findReleaseChecksums() without a signature = ok, want missing")
	}
}

func TestDescribeInstallabilityBlocksUnsignedReleases(t *testing.T) {
	info := UpdateInfo{Available: true, Latest: "9.9.9"}
	describeInstallability(&info, releaseAssets())
	if info.CanInstall {
		t.Fatal("describeInstallability() allowed installing a release without signed checksums")
	}
	if !strings.Contains(info.Blocker, "not signed") {
		t.Fatalf("Blocker = %q, want it to say the release is not signed", info.Blocker)
	}
}

func TestVerifyReleaseSignature(t *testing.T) {
	public, private := testSigningKey(t)
	sums := []byte("abc  VPSBox-1-5-0-macos.zip\n")
	signature := ed25519.Sign(private, releaseSignedMessage("1.5.0", sums))

	if err := verifyReleaseSignature(public, "1.5.0", sums, signature); err != nil {
		t.Fatalf("verifyReleaseSignature() error = %v", err)
	}
	// The same signed sums presented as a newer release: a replayed downgrade.
	if err := verifyReleaseSignature(public, "9.9.9", sums, signature); err == nil {
		t.Fatal("verifyReleaseSignature() accepted signed sums under a different version")
	}
	if err := verifyReleaseSignature(public, "1.5.0", []byte("abd  VPSBox-1-5-0-macos.zip\n"), signature); err == nil {
		t.Fatal("verifyReleaseSignature() accepted edited sums")
	}
	if err := verifyReleaseSignature(updatePublicKey, "1.5.0", sums, signature); err == nil {
		t.Fatal("verifyReleaseSignature() accepted a signature from a different key")
	}
	if err := verifyReleaseSignature(public, "1.5.0", sums, signature[:10]); err == nil {
		t.Fatal("verifyReleaseSignature() accepted a truncated signature")
	}
}

func TestChecksumFor(t *testing.T) {
	sum := strings.Repeat("ab", sha256.Size)
	sums := []byte(fmt.Sprintf("%s  VPSBox-1-5-0-linux-amd64.tar.gz\n%s *VPSBox-1-5-0-macos.zip\n", strings.Repeat("cd", sha256.Size), sum))

	got, err := checksumFor(sums, "VPSBox-1-5-0-macos.zip")
	if err != nil || got != sum {
		t.Fatalf("checksumFor() = %q, %v, want %q", got, err, sum)
	}
	if _, err := checksumFor(sums, "VPSBox-1-5-0-windows-setup.exe"); err == nil {
		t.Fatal("checksumFor() found an asset the listing does not contain")
	}
	if _, err := checksumFor([]byte("nothex  VPSBox-1-5-0-macos.zip\n"), "VPSBox-1-5-0-macos.zip"); err == nil {
		t.Fatal("checksumFor() accepted a malformed checksum")
	}
}

func TestVerifyDownloadedAsset(t *testing.T) {
	public, private := testSigningKey(t)
	const assetName = "VPSBox-1-5-0-macos.zip"
	payload := []byte("the real release build")

	digest := sha256.Sum256(payload)
	sums := []byte(hex.EncodeToString(digest[:]) + "  " + assetName + "\n")
	signature := ed25519.Sign(private, releaseSignedMessage("1.5.0", sums))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/SHA256SUMS":
			w.Write(sums)
		case "/SHA256SUMS.sig":
			w.Write(signature)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	checksums := releaseChecksums{SumsURL: server.URL + "/SHA256SUMS", SignatureURL: server.URL + "/SHA256SUMS.sig"}

	dir := t.TempDir()
	good := filepath.Join(dir, "good.zip")
	if err := os.WriteFile(good, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyDownloadedAsset(context.Background(), public, checksums, "1.5.0", assetName, good); err != nil {
		t.Fatalf("verifyDownloadedAsset() error = %v", err)
	}

	swapped := filepath.Join(dir, "swapped.zip")
	if err := os.WriteFile(swapped, []byte("a build someone else uploaded"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyDownloadedAsset(context.Background(), public, checksums, "1.5.0", assetName, swapped); err == nil {
		t.Fatal("verifyDownloadedAsset() accepted a file that does not match the signed checksum")
	}

	missing := releaseChecksums{SumsURL: server.URL + "/SHA256SUMS", SignatureURL: server.URL + "/nope"}
	if err := verifyDownloadedAsset(context.Background(), public, missing, "1.5.0", assetName, good); err == nil {
		t.Fatal("verifyDownloadedAsset() succeeded without a signature")
	}
}

func testSigningKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return public, private
}
