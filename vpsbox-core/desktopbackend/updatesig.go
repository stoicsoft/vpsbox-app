package desktopbackend

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// Release signing, end to end:
//
//	The release workflow hashes every asset into SHA256SUMS, then signs
//	"vpsbox-release:<version>\n" followed by SHA256SUMS with an Ed25519 key
//	held only in the UPDATE_SIGNING_KEY Actions secret. Both files are
//	attached to the release.
//
//	Before anything is staged, the app checks that signature against the public
//	key below and that the downloaded asset's hash is the one listed.
//
// Code signing alone can't cover this: Windows and Linux builds are unsigned,
// and on every platform the only other check is "came from the GitHub
// release", which anyone holding a token with release access can publish.
// Signing the version together with the sums means an older signed release
// can't be replayed under a newer tag to force a downgrade.

// updateSigningKey is the public half of UPDATE_SIGNING_KEY. The release
// workflow refuses to publish if the secret doesn't match it.
const updateSigningKey = "gpYDuhTD3xYpp3F3ljiDJeNKAvqfQnRDbcKwOGyJMFY="

// updatePublicKey is a variable so tests can sign with a throwaway key.
var updatePublicKey = mustDecodePublicKey(updateSigningKey)

const (
	checksumsAssetName = "SHA256SUMS"
	signatureAssetName = "SHA256SUMS.sig"

	// Both files are a few hundred bytes; the cap only stops a hostile
	// server from streaming something enormous into memory.
	maxChecksumsSize = 64 << 10
)

// releaseVersionPattern is the only tag shape the updater accepts: x.y.z with
// an optional dotted pre-release. The version ends up in file names and in the
// helper scripts, so anything a shell or cmd.exe would interpret is refused
// here rather than escaped later.
var releaseVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

func validReleaseVersion(version string) bool {
	return releaseVersionPattern.MatchString(version)
}

// releaseChecksums locates the signed checksum files on a release.
type releaseChecksums struct {
	SumsURL      string
	SignatureURL string
}

// findReleaseChecksums returns the checksum files, or ok=false when the
// release predates signing (or someone uploaded assets by hand).
func findReleaseChecksums(assets []releaseAsset) (releaseChecksums, bool) {
	var found releaseChecksums
	for _, asset := range assets {
		switch asset.Name {
		case checksumsAssetName:
			found.SumsURL = asset.URL
		case signatureAssetName:
			found.SignatureURL = asset.URL
		}
	}
	return found, found.SumsURL != "" && found.SignatureURL != ""
}

func mustDecodePublicKey(encoded string) ed25519.PublicKey {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != ed25519.PublicKeySize {
		panic("desktopbackend: updateSigningKey is not a base64 Ed25519 public key")
	}
	return ed25519.PublicKey(key)
}

// releaseSignedMessage is exactly what the release workflow signs.
func releaseSignedMessage(version string, sums []byte) []byte {
	message := []byte("vpsbox-release:" + version + "\n")
	return append(message, sums...)
}

// verifyReleaseSignature checks sums against its detached signature.
func verifyReleaseSignature(key ed25519.PublicKey, version string, sums, signature []byte) error {
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("the release signature is malformed")
	}
	if !ed25519.Verify(key, releaseSignedMessage(version, sums), signature) {
		return fmt.Errorf("the release signature does not match VPSBox %s — it was not published by the VPSBox release workflow", version)
	}
	return nil
}

// checksumFor finds assetName in a sha256sum-format listing.
func checksumFor(sums []byte, assetName string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		// sha256sum writes "<hash>  <name>" in text mode and
		// "<hash> *<name>" in binary mode.
		sum, name, ok := strings.Cut(strings.TrimSpace(scanner.Text()), " ")
		if !ok || name == "" {
			continue
		}
		if name[0] == ' ' || name[0] == '*' {
			name = name[1:]
		}
		if name != assetName {
			continue
		}
		sum = strings.ToLower(sum)
		if decoded, err := hex.DecodeString(sum); err != nil || len(decoded) != sha256.Size {
			return "", fmt.Errorf("the release checksum for %s is malformed", assetName)
		}
		return sum, nil
	}
	return "", fmt.Errorf("the signed release checksums do not list %s", assetName)
}

// fileSHA256 hashes a downloaded file.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// verifyDownloadedAsset fetches the signed checksums for version and checks
// the file at path, named assetName on the release, against them.
func verifyDownloadedAsset(ctx context.Context, key ed25519.PublicKey, checksums releaseChecksums, version, assetName, path string) error {
	if checksums.SumsURL == "" || checksums.SignatureURL == "" {
		return fmt.Errorf("VPSBox %s is not signed for in-app install — download it from the release page", version)
	}
	sums, err := fetchSmallAsset(ctx, checksums.SumsURL)
	if err != nil {
		return fmt.Errorf("could not download the release checksums: %w", err)
	}
	signature, err := fetchSmallAsset(ctx, checksums.SignatureURL)
	if err != nil {
		return fmt.Errorf("could not download the release signature: %w", err)
	}
	if err := verifyReleaseSignature(key, version, sums, signature); err != nil {
		return err
	}

	want, err := checksumFor(sums, assetName)
	if err != nil {
		return err
	}
	got, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("the downloaded %s does not match the signed checksum — it may have been corrupted or tampered with", assetName)
	}
	return nil
}

// fetchSmallAsset downloads a checksum or signature file into memory.
func fetchSmallAsset(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "vpsbox-desktop")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumsSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxChecksumsSize {
		return nil, fmt.Errorf("the file is larger than a checksum list should be")
	}
	return body, nil
}
