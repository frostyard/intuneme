// Package imagesig verifies the cosign signature of the container image with
// the Go standard library only.
//
// intuneme extracts the image as root, so the image is a trust boundary: an
// unsigned image, or one signed by another key, installs whatever it carries
// under container root, and the container runs without --private-users, so
// container root is host root. Verify runs before any file is written, and the
// caller pulls the digest that was verified, never the tag: a tag can be moved
// to another manifest between the check and the pull.
//
// The verifier reads the legacy cosign format (cosign sign
// --new-bundle-format=false, which the image workflow uses): the signature is a
// manifest at tag sha256-<hex>.sig in the same repository, holding
// simplesigning layers. The Rekor bundle annotation is ignored, so a signature
// is trusted only because it verifies with the pinned key.
package imagesig

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	containerimage "github.com/frostyard/intuneme/ubuntu-intune"
)

// ErrUnsigned reports that the registry holds no signature for the image: no
// sha256-<hex>.sig manifest, or no layer that carries a signature.
var ErrUnsigned = errors.New("image is not signed")

// ErrInvalidSignature reports that signatures exist, but none of them verifies
// with the pinned key for this manifest digest and this repository.
var ErrInvalidSignature = errors.New("image signature is not valid")

const (
	simpleSigningMediaType = "application/vnd.dev.cosign.simplesigning.v1+json"
	signatureAnnotation    = "dev.cosignproject.cosign/signature"

	ociManifestMediaType         = "application/vnd.oci.image.manifest.v1+json"
	ociIndexMediaType            = "application/vnd.oci.image.index.v1+json"
	dockerManifestMediaType      = "application/vnd.docker.distribution.manifest.v2+json"
	dockerManifestListMediaType  = "application/vnd.docker.distribution.manifest.list.v2+json"
	signatureTypeCosignContainer = "cosign container image signature"

	// maxManifestBytes caps a manifest. A real one is a few kilobytes; the cap
	// stops a hostile registry from streaming without end.
	maxManifestBytes = 4 << 20
	// maxPayloadBytes caps a signature payload. A real simplesigning payload is
	// a few hundred bytes.
	maxPayloadBytes = 64 << 10
	// maxDistinctBlobs caps the payloads 1 call fetches. A hostile signature
	// manifest can name thousands of distinct digests, and each is a request;
	// layers past the cap are treated as failed candidates.
	maxDistinctBlobs = 32
	// defaultTimeout bounds 1 request when the caller supplies no client.
	defaultTimeout = 60 * time.Second
	// verifyTimeout bounds 1 whole Verify call, so a registry that answers every
	// request slowly cannot hold the command open: each request stays under the
	// client timeout, and their total stays under this.
	verifyTimeout = 2 * time.Minute
)

// manifestMediaTypes are the manifest types a tag may answer with.
var manifestMediaTypes = []string{
	ociManifestMediaType,
	ociIndexMediaType,
	dockerManifestMediaType,
	dockerManifestListMediaType,
}

// signatureMediaTypes are the manifest types the .sig manifest may answer with.
var signatureMediaTypes = []string{ociManifestMediaType, dockerManifestMediaType}

// Verifier checks image signatures against 1 public key. A nil Client uses a
// client with a 60-second timeout; a nil or empty PublicKey uses the key that
// ships in the repository (containerimage.CosignPublicKey).
type Verifier struct {
	Client    *http.Client
	PublicKey []byte
}

// Verify checks the signature of ref, which must be host/repo:tag, and returns
// the verified reference host/repo@sha256:<hex>. The digest comes from the
// manifest bytes that were read and hashed here, never from the
// Docker-Content-Digest header, which the registry chooses.
func (v *Verifier) Verify(ctx context.Context, ref string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	repo, tag, err := splitTaggedRef(ref)
	if err != nil {
		return "", err
	}
	pub, err := parsePublicKey(v.publicKey())
	if err != nil {
		return "", err
	}

	f := &fetcher{client: v.client(), repo: repo, base: registryBase(repo)}
	manifest, err := f.manifest(ctx, tag, manifestMediaTypes, manifestMediaTypes, nil)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(manifest)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	sigTag := strings.Replace(digest, ":", "-", 1) + ".sig"
	signed, err := f.manifest(ctx, sigTag, signatureMediaTypes, signatureMediaTypes, ErrUnsigned)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ref, err)
	}

	var sigManifest struct {
		Layers []struct {
			MediaType   string            `json:"mediaType"`
			Digest      string            `json:"digest"`
			Size        int64             `json:"size"`
			Annotations map[string]string `json:"annotations"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(signed, &sigManifest); err != nil {
		return "", fmt.Errorf("parse the signature manifest of %s: %w", ref, err)
	}

	var reasons []string
	signatures := 0
	for _, layer := range sigManifest.Layers {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("check the image signature of %s: %w", ref, err)
		}
		if layer.MediaType != simpleSigningMediaType {
			continue
		}
		encoded := layer.Annotations[signatureAnnotation]
		if encoded == "" {
			reasons = append(reasons, fmt.Sprintf("layer %s carries no %s annotation", layer.Digest, signatureAnnotation))
			continue
		}
		// A layer that carries a signature is 1 candidate: a failure here is a
		// bad signature, not a missing one.
		signatures++
		if err := f.verifyLayer(ctx, layer.Digest, layer.Size, encoded, pub, digest, repo); err != nil {
			reasons = append(reasons, err.Error())
			continue
		}
		return repo + "@" + digest, nil
	}

	if signatures == 0 {
		return "", fmt.Errorf("%w: %s has no signature layer%s", ErrUnsigned, ref, detail(reasons))
	}
	return "", fmt.Errorf("%w: %s%s", ErrInvalidSignature, ref, detail(reasons))
}

// verifyLayer fetches 1 signed payload, checks that the blob matches its
// declared size and digest, and checks the signature and its claims.
func (f *fetcher) verifyLayer(ctx context.Context, digest string, size int64, encoded string, pub *ecdsa.PublicKey, wantDigest, wantRepo string) error {
	if size <= 0 || size > maxPayloadBytes {
		return fmt.Errorf("signature layer %s has the unusable size %d", digest, size)
	}
	hexDigest, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || len(hexDigest) != 64 {
		return fmt.Errorf("signature layer has the unsupported digest %q", digest)
	}
	if _, err := hex.DecodeString(hexDigest); err != nil {
		return fmt.Errorf("signature layer has the unsupported digest %q", digest)
	}
	if !f.blobWithinLimit(digest) {
		return errors.New("too many distinct signature payloads")
	}
	payload, err := f.blob(ctx, digest, size)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("signature of layer %s is not base64: %w", digest, err)
	}
	if err := verifyPayload(pub, payload, sig, wantDigest, wantRepo); err != nil {
		return fmt.Errorf("layer %s: %w", digest, err)
	}
	return nil
}

// verifyPayload checks 1 simplesigning layer: the ECDSA signature over the
// payload, and the payload's own claims. The claims are load-bearing. Without
// them a valid signature over any other image, made with the same key, would be
// accepted for this one.
func verifyPayload(pub *ecdsa.PublicKey, payload, sig []byte, wantDigest, wantRepo string) error {
	sum := sha256.Sum256(payload)
	if !ecdsa.VerifyASN1(pub, sum[:], sig) {
		return errors.New("the signature does not verify with the pinned public key")
	}
	var claim struct {
		Critical struct {
			Identity struct {
				DockerReference string `json:"docker-reference"`
			} `json:"identity"`
			Image struct {
				DockerManifestDigest string `json:"docker-manifest-digest"`
			} `json:"image"`
			Type string `json:"type"`
		} `json:"critical"`
	}
	if err := json.Unmarshal(payload, &claim); err != nil {
		return fmt.Errorf("parse the signature payload: %w", err)
	}
	if claim.Critical.Type != signatureTypeCosignContainer {
		return fmt.Errorf("the payload type is %q, not a cosign image signature", claim.Critical.Type)
	}
	if got := claim.Critical.Image.DockerManifestDigest; got != wantDigest {
		return fmt.Errorf("the payload signs %s, not %s", got, wantDigest)
	}
	if got := claim.Critical.Identity.DockerReference; got != wantRepo {
		return fmt.Errorf("the payload names the repository %s, not %s", got, wantRepo)
	}
	return nil
}

// Verify checks ref with a Verifier that uses the default client and the key
// that ships in the repository.
func Verify(ctx context.Context, ref string) (string, error) {
	return (&Verifier{}).Verify(ctx, ref)
}

func (v *Verifier) client() *http.Client {
	base := v.Client
	if base == nil {
		base = &http.Client{Timeout: defaultTimeout}
	}
	// A shallow copy keeps the caller's client untouched while replacing the
	// redirect policy for this call only.
	client := *base
	client.CheckRedirect = httpsOnly(base.CheckRedirect)
	return &client
}

// httpsOnly refuses a redirect to any scheme but https, then applies next, or
// the standard limit of 10 redirects when next is nil. The default policy
// follows an https to http redirect, so a token or a payload could travel in
// clear text and the Authorization header could ride a downgrade.
func httpsOnly(next func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing to follow a redirect to %s: only https redirects are allowed", req.URL.Host)
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
}

func (v *Verifier) publicKey() []byte {
	if len(v.PublicKey) > 0 {
		return v.PublicKey
	}
	return containerimage.CosignPublicKey
}

// parsePublicKey reads a PEM PUBLIC KEY block and requires an ECDSA P-256 key,
// the only key type cosign signs images with here.
func parsePublicKey(data []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("the cosign public key is not a PEM PUBLIC KEY block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse the cosign public key: %w", err)
	}
	pub, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("the cosign public key is a %T, not an ECDSA key", parsed)
	}
	if pub.Curve != elliptic.P256() {
		return nil, fmt.Errorf("the cosign public key uses the curve %s, not P-256", pub.Curve.Params().Name)
	}
	return pub, nil
}

// splitTaggedRef splits host/repo:tag into host/repo and tag. The host may carry
// a port, so the tag is the text after the last colon that follows the last
// slash. A reference by digest is rejected: the digest is what the check
// produces, so it cannot be the thing that is checked.
func splitTaggedRef(ref string) (string, string, error) {
	if strings.Contains(ref, "@") {
		return "", "", fmt.Errorf("image reference %q must name a tag, not a digest", ref)
	}
	slash := strings.LastIndex(ref, "/")
	colon := strings.LastIndex(ref, ":")
	if colon <= slash || colon == len(ref)-1 {
		return "", "", fmt.Errorf("image reference %q has no tag: expected host/repo:tag", ref)
	}
	repo, tag := ref[:colon], ref[colon+1:]
	if slash <= 0 {
		return "", "", fmt.Errorf("image reference %q has no repository after the registry host", ref)
	}
	return repo, tag, nil
}

// fetcher talks to 1 registry repository. It keeps the bearer token of the
// challenge so a single Verify call asks for 1 token, and it caches blobs
// because a signature manifest often holds the same payload digest once for
// every tag that was signed.
type fetcher struct {
	client *http.Client
	// repo is host/path, the identity the payload must name.
	repo string
	// base is the distribution API base URL, https://host/v2/path.
	base  string
	token string
	blobs map[string][]byte
	// attempted records every payload digest this call tried to fetch, so the
	// cap counts requests even when a fetch fails.
	attempted map[string]struct{}
}

// registryBase returns the distribution API base URL of a host/repo reference.
// The API lives under /v2/, so the manifests and blobs of
// ghcr.io/frostyard/ubuntu-intune are under
// https://ghcr.io/v2/frostyard/ubuntu-intune: without /v2 a registry answers
// 404, and the check would report every image as unsigned.
func registryBase(repo string) string {
	host, path, ok := strings.Cut(repo, "/")
	if !ok {
		return "https://" + repo + "/v2"
	}
	return "https://" + host + "/v2/" + path
}

// get sends 1 GET, answers a Bearer challenge once, and returns the response
// with its body unread. The caller closes the body.
func (f *fetcher) get(ctx context.Context, rawURL string, accept []string) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		if len(accept) > 0 {
			req.Header.Set("Accept", strings.Join(accept, ", "))
		}
		if f.token != "" {
			req.Header.Set("Authorization", "Bearer "+f.token)
		}
		resp, err := f.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request %s: %w", rawURL, err)
		}
		if resp.StatusCode != http.StatusUnauthorized || attempt > 0 {
			return resp, nil
		}
		challenge := resp.Header.Get("WWW-Authenticate")
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		token, err := f.fetchToken(ctx, challenge)
		if err != nil {
			return nil, err
		}
		f.token = token
	}
}

// fetchToken answers a Bearer challenge. Only https realms are accepted: a
// plain-http realm would hand the token, and so the image, to anyone on the
// path.
func (f *fetcher) fetchToken(ctx context.Context, challenge string) (string, error) {
	params, err := parseChallenge(challenge)
	if err != nil {
		return "", err
	}
	realm, err := url.Parse(params["realm"])
	if err != nil || !strings.EqualFold(realm.Scheme, "https") || realm.Host == "" {
		return "", fmt.Errorf("the registry token realm %q is not an https URL", params["realm"])
	}
	query := url.Values{}
	if service := params["service"]; service != "" {
		query.Set("service", service)
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + repoPath(f.repo) + ":pull"
	}
	query.Set("scope", scope)

	// The token request answers no challenge of its own: a realm that asks for
	// a token would otherwise recurse without end.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String()+"?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request a registry token from %s: %w", realm, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", statusError("get a registry token", resp)
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	data, err := readLimited(resp.Body, maxManifestBytes)
	if err != nil {
		return "", fmt.Errorf("read the registry token: %w", err)
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return "", fmt.Errorf("parse the registry token: %w", err)
	}
	token := body.Token
	if token == "" {
		token = body.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("the registry token response holds no token")
	}
	return token, nil
}

// manifest fetches a manifest, requires 1 of the allowed media types, and caps
// its size. when notFound is set, a 404 wraps it instead of being an error.
func (f *fetcher) manifest(ctx context.Context, ref string, accept, allowed []string, notFound error) ([]byte, error) {
	resp, err := f.get(ctx, f.base+"/manifests/"+ref, accept)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound && notFound != nil {
		return nil, fmt.Errorf("%w: the registry holds no manifest %s", notFound, ref)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, statusError("get the manifest "+ref, resp)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("manifest %s has the unusable Content-Type %q", ref, resp.Header.Get("Content-Type"))
	}
	if !slices.Contains(allowed, mediaType) {
		return nil, fmt.Errorf("manifest %s has the unsupported Content-Type %q", ref, mediaType)
	}
	data, err := readLimited(resp.Body, maxManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("read the manifest %s: %w", ref, err)
	}
	return data, nil
}

// blobWithinLimit reports whether 1 more distinct payload may be fetched, and
// records the attempt.
func (f *fetcher) blobWithinLimit(digest string) bool {
	if _, ok := f.attempted[digest]; ok {
		return true
	}
	if len(f.attempted) >= maxDistinctBlobs {
		return false
	}
	if f.attempted == nil {
		f.attempted = make(map[string]struct{})
	}
	f.attempted[digest] = struct{}{}
	return true
}

// blob fetches a signature payload and requires it to match its declared size
// and digest, so the bytes that are verified are the bytes the manifest names.
func (f *fetcher) blob(ctx context.Context, digest string, size int64) ([]byte, error) {
	data, cached := f.blobs[digest]
	if !cached {
		resp, err := f.get(ctx, f.base+"/blobs/"+digest, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return nil, statusError("get the blob "+digest, resp)
		}
		data, err = readLimited(resp.Body, size)
		if err != nil {
			return nil, fmt.Errorf("read the blob %s: %w", digest, err)
		}
		sum := sha256.Sum256(data)
		if got := "sha256:" + hex.EncodeToString(sum[:]); got != digest {
			return nil, fmt.Errorf("blob content is %s, not the declared %s", got, digest)
		}
		if f.blobs == nil {
			f.blobs = make(map[string][]byte)
		}
		f.blobs[digest] = data
	}
	// A cached payload still has to match this layer's declared size: 2 layers
	// can name 1 digest with different sizes, and the size is part of the check.
	if int64(len(data)) != size {
		return nil, fmt.Errorf("blob %s is %d bytes, not the %d the manifest declares", digest, len(data), size)
	}
	return data, nil
}

// parseChallenge reads the parameters of a Bearer WWW-Authenticate challenge.
// Any other scheme is an error: the registry must offer a token, and it must
// offer it over https.
func parseChallenge(challenge string) (map[string]string, error) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(challenge), " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return nil, fmt.Errorf("the registry challenge %q does not use the Bearer scheme", challenge)
	}
	params := make(map[string]string)
	for rest != "" {
		rest = strings.TrimLeft(rest, " ,")
		key, value, found := strings.Cut(rest, "=")
		if !found {
			break
		}
		key = strings.TrimSpace(key)
		rest = value
		if strings.HasPrefix(rest, `"`) {
			end := strings.Index(rest[1:], `"`)
			if end < 0 {
				return nil, fmt.Errorf("the registry challenge %q has an unterminated value", challenge)
			}
			params[strings.ToLower(key)] = rest[1 : end+1]
			rest = rest[end+2:]
			continue
		}
		token, remainder, _ := strings.Cut(rest, ",")
		params[strings.ToLower(key)] = strings.TrimSpace(token)
		rest = remainder
	}
	if params["realm"] == "" {
		return nil, fmt.Errorf("the registry challenge %q names no realm", challenge)
	}
	return params, nil
}

// maxReasons caps the reasons in 1 error. A signature manifest gains a layer
// for every signed tag, so it can hold hundreds of layers.
const maxReasons = 5

// detail renders the distinct reasons of failed signature layers, prefixed so
// the error reads as 1 sentence. Layers often share 1 payload, so their
// reasons repeat.
func detail(reasons []string) string {
	var distinct []string
	for _, r := range reasons {
		if !slices.Contains(distinct, r) {
			distinct = append(distinct, r)
		}
	}
	if len(distinct) == 0 {
		return ""
	}
	if len(distinct) > maxReasons {
		more := len(distinct) - maxReasons
		distinct = append(distinct[:maxReasons:maxReasons], fmt.Sprintf("%d more", more))
	}
	return ": " + strings.Join(distinct, "; ")
}

// repoPath is the repository name without the registry host, which is what a
// registry token scope uses.
func repoPath(repo string) string {
	if _, path, ok := strings.Cut(repo, "/"); ok {
		return path
	}
	return repo
}

// readLimited reads at most limit bytes. A longer body is an error, never a
// truncation: a digest computed from a prefix would cover bytes the caller
// never saw.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("the body is over the %d byte limit", limit)
	}
	return data, nil
}

// statusError names the request, the HTTP status and, when the registry sends
// one, its short explanation. The body is capped: it is a diagnostic, not data.
func statusError(what string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if detail := strings.TrimSpace(string(body)); detail != "" {
		return fmt.Errorf("%s: %s: %s", what, resp.Status, detail)
	}
	return fmt.Errorf("%s: %s", what, resp.Status)
}
