package objstore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// emptyPayloadHash is the SHA-256 of the empty string, which every request this
// client makes sends as its body hash — all of them are bodyless.
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// client speaks just enough of the S3 REST API to manage buckets: create, list,
// delete, and check whether one still holds objects. It signs with SigV4 by hand
// rather than pulling in the AWS SDK, which would be a large dependency tree for
// four requests against a server running on this same machine.
//
// Addressing is always path-style (endpoint/bucket). Virtual-host style needs a
// wildcard DNS name for the endpoint, which the host does not have.
type client struct {
	endpoint  string // scheme://host:port, no trailing slash
	region    string
	accessKey string
	secretKey string
	http      *http.Client
	// now is overridable so signing is testable against a fixed timestamp.
	now func() time.Time
}

func newClient(endpoint, region, accessKey, secretKey string) *client {
	return &client{
		endpoint:  strings.TrimRight(endpoint, "/"),
		region:    region,
		accessKey: accessKey,
		secretKey: secretKey,
		http:      &http.Client{Timeout: 15 * time.Second},
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// s3Error is the XML body S3 returns on failure. The Code is the part worth
// branching on (BucketAlreadyOwnedByYou, NoSuchBucket, BucketNotEmpty).
type s3Error struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

// apiError carries an S3 failure with enough context to branch on the code and
// still print something useful when nothing matches.
type apiError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *apiError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("s3: %s (%s)", e.Message, e.Code)
	}
	return fmt.Sprintf("s3: unexpected status %d", e.StatusCode)
}

// do issues a signed request. path is the unescaped resource path ("/bucket" or
// "/bucket/some key"); escaping happens here so the signature and the wire form
// are guaranteed to agree.
func (c *client) do(ctx context.Context, method, path, rawQuery string) ([]byte, error) {
	escapedPath := s3EscapePath(path)

	target := c.endpoint + escapedPath
	if rawQuery != "" {
		target += "?" + rawQuery
	}

	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	// Go re-derives the wire path from URL.Path unless RawPath is set and is a
	// valid encoding of it. Pin both so our escaping is what actually goes out.
	req.URL.Path = path
	req.URL.RawPath = escapedPath

	c.sign(req, escapedPath, rawQuery)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		failure := &apiError{StatusCode: resp.StatusCode}
		var parsed s3Error
		if xml.Unmarshal(body, &parsed) == nil {
			failure.Code = parsed.Code
			failure.Message = parsed.Message
		}
		if failure.Message == "" {
			failure.Message = strings.TrimSpace(string(body))
		}
		return nil, failure
	}

	return body, nil
}

// sign adds the SigV4 Authorization header for a bodyless request. path must be
// the already-encoded URI path and rawQuery the already-encoded query, both
// exactly as sent, or the signature will not match what the server computes.
func (c *client) sign(req *http.Request, path, rawQuery string) {
	c.signWithPayload(req, path, rawQuery, nil)
}

// signWithPayload is sign for a request that carries a body. S3 signs the body
// hash, so it has to be known up front — which is why payload is a slice rather
// than a reader.
func (c *client) signWithPayload(req *http.Request, path, rawQuery string, payload []byte) {
	payloadHash := emptyPayloadHash
	if len(payload) > 0 {
		sum := sha256.Sum256(payload)
		payloadHash = hex.EncodeToString(sum[:])
	}

	now := c.now()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := strings.Join([]string{
		"host:" + req.URL.Host,
		"x-amz-content-sha256:" + payloadHash,
		"x-amz-date:" + amzDate,
	}, "\n") + "\n"

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(path),
		canonicalQuery(rawQuery),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{dateStamp, c.region, "s3", "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex(canonicalRequest),
	}, "\n")

	signingKey := hmacBytes(
		hmacBytes(
			hmacBytes(
				hmacBytes([]byte("AWS4"+c.secretKey), dateStamp),
				c.region),
			"s3"),
		"aws4_request")

	signature := hex.EncodeToString(hmacRaw(signingKey, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.accessKey, scope, signedHeaders, signature))
}

// canonicalURI normalises the path for signing. An empty path signs as "/".
func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	return path
}

// s3EscapePath percent-encodes a resource path the way SigV4 requires: RFC 3986
// unreserved characters pass through, "/" stays a separator, and everything else
// is encoded with uppercase hex. net/url is no help here — QueryEscape turns
// spaces into "+" and PathEscape leaves characters S3 expects encoded.
func s3EscapePath(path string) string {
	var out strings.Builder
	for i := 0; i < len(path); i++ {
		ch := path[i]
		switch {
		case ch >= 'A' && ch <= 'Z',
			ch >= 'a' && ch <= 'z',
			ch >= '0' && ch <= '9',
			ch == '-', ch == '_', ch == '.', ch == '~', ch == '/':
			out.WriteByte(ch)
		default:
			fmt.Fprintf(&out, "%%%02X", ch)
		}
	}
	return out.String()
}

// canonicalQuery re-emits the query string sorted by key with empty values kept
// as "key=", which is what SigV4 requires and what url.Values.Encode does not do.
func canonicalQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		vals := values[key]
		sort.Strings(vals)
		for _, val := range vals {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(val))
		}
	}
	return strings.Join(parts, "&")
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacRaw(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func hmacBytes(key []byte, data string) []byte {
	return hmacRaw(key, data)
}

// CreateBucket makes a bucket. A bucket this account already owns is treated as
// success, so creating twice is not an error.
func (c *client) CreateBucket(ctx context.Context, name string) error {
	_, err := c.do(ctx, http.MethodPut, "/"+name, "")
	if err == nil {
		return nil
	}
	var failure *apiError
	if errors.As(err, &failure) {
		switch failure.Code {
		case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
			return nil
		}
	}
	return err
}

// DeleteBucket removes an empty bucket. S3 refuses to delete a bucket with
// objects in it, which is the behaviour we want — callers that mean it empty the
// bucket first.
func (c *client) DeleteBucket(ctx context.Context, name string) error {
	_, err := c.do(ctx, http.MethodDelete, "/"+name, "")
	return err
}

type listAllBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Buckets []struct {
		Name         string    `xml:"Name"`
		CreationDate time.Time `xml:"CreationDate"`
	} `xml:"Buckets>Bucket"`
}

// ListBuckets returns the bucket names the server actually holds, which is the
// truth the local registry is only a cache of.
func (c *client) ListBuckets(ctx context.Context) ([]string, error) {
	body, err := c.do(ctx, http.MethodGet, "/", "")
	if err != nil {
		return nil, err
	}

	var parsed listAllBucketsResult
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse bucket list: %w", err)
	}

	names := make([]string, 0, len(parsed.Buckets))
	for _, bucket := range parsed.Buckets {
		names = append(names, bucket.Name)
	}
	sort.Strings(names)
	return names, nil
}

type listObjectsResult struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	KeyCount              int      `xml:"KeyCount"`
	IsTruncated           bool     `xml:"IsTruncated"`
	NextContinuationToken string   `xml:"NextContinuationToken"`
	Contents              []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
}

// listObjectPage fetches one page of keys. token is the continuation token from a
// previous page, or "" for the first.
func (c *client) listObjectPage(ctx context.Context, bucket, token string) ([]string, string, error) {
	query := url.Values{}
	query.Set("list-type", "2")
	query.Set("max-keys", "1000")
	if token != "" {
		query.Set("continuation-token", token)
	}

	body, err := c.do(ctx, http.MethodGet, "/"+bucket, query.Encode())
	if err != nil {
		return nil, "", err
	}

	var parsed listObjectsResult
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return nil, "", fmt.Errorf("parse object list: %w", err)
	}

	keys := make([]string, 0, len(parsed.Contents))
	for _, entry := range parsed.Contents {
		keys = append(keys, entry.Key)
	}

	next := ""
	if parsed.IsTruncated {
		next = parsed.NextContinuationToken
	}
	return keys, next, nil
}

// DeleteObject removes a single key.
func (c *client) DeleteObject(ctx context.Context, bucket, key string) error {
	_, err := c.do(ctx, http.MethodDelete, "/"+bucket+"/"+key, "")
	return err
}

// emptyBucket deletes every object in a bucket, page by page, so the bucket
// itself can then be removed. Only reached when the caller has explicitly said
// they want the contents gone.
func (c *client) emptyBucket(ctx context.Context, bucket string) error {
	token := ""
	for {
		keys, next, err := c.listObjectPage(ctx, bucket, token)
		if err != nil {
			return err
		}
		for _, key := range keys {
			if err := c.DeleteObject(ctx, bucket, key); err != nil {
				return fmt.Errorf("delete %s/%s: %w", bucket, key, err)
			}
		}
		if next == "" {
			// A truncated page with no token would loop forever; treat the
			// absence of a token as the end regardless of what keys remain.
			return nil
		}
		token = next
	}
}

// BucketEmpty reports whether a bucket holds no objects. It asks for a single key
// rather than listing everything, so it stays cheap on a bucket with a lot in it.
func (c *client) BucketEmpty(ctx context.Context, name string) (bool, error) {
	body, err := c.do(ctx, http.MethodGet, "/"+name, "list-type=2&max-keys=1")
	if err != nil {
		return false, err
	}

	var parsed listObjectsResult
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return false, fmt.Errorf("parse object list: %w", err)
	}
	// KeyCount is authoritative when present; fall back to the entries so a
	// server that omits it does not read as empty.
	return parsed.KeyCount == 0 && len(parsed.Contents) == 0, nil
}

// Reachable reports whether the endpoint answers a signed request at all. Used to
// wait for a freshly started server to come up.
func (c *client) Reachable(ctx context.Context) error {
	_, err := c.ListBuckets(ctx)
	return err
}
