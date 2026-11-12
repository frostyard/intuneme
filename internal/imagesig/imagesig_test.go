package imagesig

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	containerimage "github.com/frostyard/intuneme/ubuntu-intune"
)

const (
	testRepo     = "frostyard/ubuntu-intune"
	testTag      = "v1.2.3"
	testToken    = "test-token"
	testManifest = `{"schemaVersion":2,"mediaType":"` + ociManifestMediaType + `","layers":[]}`
)

// testKey is a P-256 key pair: the private half signs for the fake registry,
// the public half is the verifier's pinned key.
type testKey struct {
	private *ecdsa.PrivateKey
	public  []byte
}

func newTestKey(t *testing.T) testKey {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return testKey{private: private, public: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})}
}

// sign returns the base64 signature that cosign keeps in the layer annotation.
func (k testKey) sign(t *testing.T, body []byte) string {
	t.Helper()
	sum := sha256.Sum256(body)
	sig, err := ecdsa.SignASN1(rand.Reader, k.private, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

// payload is a simplesigning payload that claims digest for repo.
func payload(digest, repo string) []byte {
	return fmt.Appendf(nil,
		`{"critical":{"identity":{"docker-reference":%q},"image":{"docker-manifest-digest":%q},"type":"cosign container image signature"},"optional":null}`,
		repo, digest)
}

func sha256Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// layer is 1 simplesigning layer of a signature manifest.
type layer struct {
	digest string
	size   int64
	sig    string
}

// signedLayer builds the layer for body, signed with k.
func signedLayer(t *testing.T, k testKey, body []byte) layer {
	t.Helper()
	return layer{digest: sha256Digest(body), size: int64(len(body)), sig: k.sign(t, body)}
}

// signatureManifest builds the OCI manifest that cosign writes at
// sha256-<hex>.sig.
func signatureManifest(t *testing.T, layers ...layer) []byte {
	t.Helper()
	entries := []map[string]any{}
	for _, l := range layers {
		entries = append(entries, map[string]any{
			"mediaType":   simpleSigningMediaType,
			"digest":      l.digest,
			"size":        l.size,
			"annotations": map[string]string{signatureAnnotation: l.sig},
		})
	}
	data, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     ociManifestMediaType,
		"layers":        entries,
	})
	if err != nil {
		t.Fatalf("marshal signature manifest: %v", err)
	}
	return data
}

// registry is a fake registry: a token endpoint, the tag manifest and the
// signature manifest of 1 repository.
type registry struct {
	host         string
	manifest     []byte
	manifestType string
	// sigStatus overrides the signature manifest status when it is not 0.
	sigStatus int
	sigBody   []byte
	// digestHeader is sent as Docker-Content-Digest.
	digestHeader string
	// realmScheme replaces the https scheme of the token realm.
	realmScheme string
	// blobRedirect makes every blob request answer a redirect to
	// <scheme>://<host>/v2/<repo>/blobs/<digest>?copy=1. Empty means no redirect.
	blobRedirect string
	blobs        map[string][]byte
	bearerSeen   bool
	// blobsServed counts the blob responses that carried a body.
	blobsServed int
	// paths records the path of every request, so a test can prove the
	// registry API was addressed under /v2/.
	paths []string
}

func (reg *registry) start(t *testing.T) *httptest.Server {
	t.Helper()
	if reg.manifest == nil {
		reg.manifest = []byte(testManifest)
	}
	if reg.blobs == nil {
		reg.blobs = map[string][]byte{}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"token":%q}`, testToken)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reg.paths = append(reg.paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			scheme := reg.realmScheme
			if scheme == "" {
				scheme = "https"
			}
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer realm="%s://%s/token",service="ghcr.io",scope="repository:%s:pull"`, scheme, r.Host, testRepo))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		reg.bearerSeen = true
		path := r.URL.Path
		// Only the distribution API under /v2/ answers; anything else is a 404, so
		// a client that forgets /v2/ fails these tests instead of passing them.
		switch {
		case strings.HasPrefix(path, "/v2/"+testRepo+"/manifests/"):
			ref := path[strings.LastIndex(path, "/")+1:]
			if strings.HasSuffix(ref, ".sig") {
				if reg.sigStatus != 0 {
					w.WriteHeader(reg.sigStatus)
					return
				}
				w.Header().Set("Content-Type", ociManifestMediaType)
				_, _ = w.Write(reg.sigBody)
				return
			}
			manifestType := reg.manifestType
			if manifestType == "" {
				manifestType = ociManifestMediaType
			}
			w.Header().Set("Content-Type", manifestType)
			if reg.digestHeader != "" {
				w.Header().Set("Docker-Content-Digest", reg.digestHeader)
			}
			_, _ = w.Write(reg.manifest)
		case strings.HasPrefix(path, "/v2/"+testRepo+"/blobs/"):
			digest := path[strings.LastIndex(path, "/")+1:]
			if reg.blobRedirect != "" && r.URL.RawQuery == "" {
				w.Header().Set("Location", fmt.Sprintf("%s://%s/v2/%s/blobs/%s?copy=1", reg.blobRedirect, r.Host, testRepo, digest))
				w.WriteHeader(http.StatusTemporaryRedirect)
				return
			}
			body, ok := reg.blobs[digest]
			if !ok {
				http.NotFound(w, r)
				return
			}
			reg.blobsServed++
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	reg.host = strings.TrimPrefix(srv.URL, "https://")
	return srv
}

// repoRef is the image repository as the verifier resolves it: the test
// registry address plus the repository path.
func (reg *registry) repoRef() string {
	return reg.host + "/" + testRepo
}

// digest is the digest of the served manifest, which the verifier computes
// from the bytes it reads.
func (reg *registry) digest(t *testing.T) string {
	t.Helper()
	return sha256Digest(reg.manifest)
}

// signWith signs every body and stores the layers and blobs, so the image has a
// signature that verifies unless the test breaks it afterwards.
func (reg *registry) signWith(t *testing.T, k testKey, bodies ...[]byte) {
	t.Helper()
	layers := make([]layer, 0, len(bodies))
	for _, body := range bodies {
		l := signedLayer(t, k, body)
		layers = append(layers, l)
		reg.blobs[l.digest] = body
	}
	reg.sigBody = signatureManifest(t, layers...)
}

// verify runs Verify against the started registry.
func (reg *registry) verify(t *testing.T, srv *httptest.Server, k testKey) (string, error) {
	t.Helper()
	v := &Verifier{Client: srv.Client(), PublicKey: k.public}
	return v.Verify(context.Background(), reg.repoRef()+":"+testTag)
}

// TestVerifyAcceptsValidSignature proves the happy path and that the check
// answers the registry's Bearer challenge.
func TestVerifyAcceptsValidSignature(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{}
	srv := reg.start(t)
	reg.signWith(t, k, payload(reg.digest(t), reg.repoRef()))

	got, err := reg.verify(t, srv, k)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	want := reg.repoRef() + "@" + reg.digest(t)
	if got != want {
		t.Errorf("Verify returned %q, want %q", got, want)
	}
	if !reg.bearerSeen {
		t.Error("the registry saw no Bearer token, so the challenge was not answered")
	}
	// The registry API lives under /v2/: a manifest request without it is a 404
	// at ghcr.io, so the path is part of the contract.
	wantPath := "/v2/" + testRepo + "/manifests/" + testTag
	if !slices.Contains(reg.paths, wantPath) {
		t.Errorf("the registry saw the paths %v, want %q", reg.paths, wantPath)
	}
}

func TestVerifyUnsigned(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{sigStatus: http.StatusNotFound}
	srv := reg.start(t)

	_, err := reg.verify(t, srv, k)
	if !errors.Is(err, ErrUnsigned) {
		t.Fatalf("error = %v, want ErrUnsigned", err)
	}
}

func TestVerifySignatureLayerMissing(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{}
	srv := reg.start(t)
	// A signature manifest whose layer is not a simplesigning layer.
	reg.sigBody = []byte(`{"schemaVersion":2,"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"sha256:` +
		strings.Repeat("ab", 32) + `","size":10}]}`)

	_, err := reg.verify(t, srv, k)
	if !errors.Is(err, ErrUnsigned) {
		t.Fatalf("error = %v, want ErrUnsigned", err)
	}
}

func TestVerifyRejectsAnotherKey(t *testing.T) {
	k, other := newTestKey(t), newTestKey(t)
	reg := &registry{}
	srv := reg.start(t)
	reg.signWith(t, other, payload(reg.digest(t), reg.repoRef()))

	_, err := reg.verify(t, srv, k)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("error = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsAnotherDigest(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{}
	srv := reg.start(t)
	reg.signWith(t, k, payload("sha256:"+strings.Repeat("11", 32), reg.repoRef()))

	_, err := reg.verify(t, srv, k)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("error = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsAnotherRepository(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{}
	srv := reg.start(t)
	reg.signWith(t, k, payload(reg.digest(t), "ghcr.io/someone/other-intune"))

	_, err := reg.verify(t, srv, k)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("error = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsBlobMismatch(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{}
	srv := reg.start(t)
	body := payload(reg.digest(t), reg.repoRef())
	reg.signWith(t, k, body)
	// The blob served for the layer digest is not the payload that was signed.
	reg.blobs[sha256Digest(body)] = []byte(`{"critical":{"type":"other"}}`)

	_, err := reg.verify(t, srv, k)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("error = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyAcceptsOneGoodLayerAmongBad(t *testing.T) {
	k, other := newTestKey(t), newTestKey(t)
	reg := &registry{}
	srv := reg.start(t)
	good := payload(reg.digest(t), reg.repoRef())
	bad := payload("sha256:"+strings.Repeat("22", 32), reg.repoRef())
	badLayer := signedLayer(t, other, bad)
	goodLayer := signedLayer(t, k, good)
	reg.blobs[badLayer.digest] = bad
	reg.blobs[goodLayer.digest] = good
	reg.sigBody = signatureManifest(t, badLayer, goodLayer)

	got, err := reg.verify(t, srv, k)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if want := reg.repoRef() + "@" + reg.digest(t); got != want {
		t.Errorf("Verify returned %q, want %q", got, want)
	}
}

func TestVerifyRejectsPlainTextTokenRealm(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{realmScheme: "http"}
	srv := reg.start(t)
	reg.signWith(t, k, payload(reg.digest(t), reg.repoRef()))

	_, err := reg.verify(t, srv, k)
	if err == nil || !strings.Contains(err.Error(), "not an https URL") {
		t.Fatalf("error = %v, want a rejection of the http realm", err)
	}
}

func TestVerifyRejectsOversizedManifest(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{manifest: []byte(strings.Repeat("a", maxManifestBytes+1))}
	srv := reg.start(t)

	_, err := reg.verify(t, srv, k)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("error = %v, want a size limit error", err)
	}
}

func TestVerifyRejectsUnsupportedManifestType(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{manifestType: "text/plain"}
	srv := reg.start(t)

	_, err := reg.verify(t, srv, k)
	if err == nil || !strings.Contains(err.Error(), "Content-Type") {
		t.Fatalf("error = %v, want an unsupported Content-Type error", err)
	}
}

// TestVerifyIgnoresDigestHeader proves the digest comes from the manifest bytes:
// a registry that announces another digest must not change what is pulled.
func TestVerifyIgnoresDigestHeader(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{digestHeader: "sha256:" + strings.Repeat("33", 32)}
	srv := reg.start(t)
	reg.signWith(t, k, payload(reg.digest(t), reg.repoRef()))

	got, err := reg.verify(t, srv, k)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if want := reg.repoRef() + "@" + reg.digest(t); got != want {
		t.Errorf("Verify returned %q, want %q", got, want)
	}
}

// The real fixture: a payload cosign produced for this repository and its
// signature, both from the registry, with the key that ships in the binary.
const (
	realPayload          = `{"critical":{"identity":{"docker-reference":"ghcr.io/frostyard/ubuntu-intune"},"image":{"docker-manifest-digest":"sha256:7ff676107cd00e35a6180efac9f19fe6e3804732662876eeae2e7811d4af0c1d"},"type":"cosign container image signature"},"optional":null}`
	realPayloadDigest    = "31b83063abc38648148a8150c3dd72bd51bef291753387366b3ce40abd5a9a79"
	realPayloadSignature = "MEUCIQDfAfzRQ6qv82U0y/ZHYxabCUbpM0g25YoQMolP+r7OMAIgGlniBfGvVSzFP5XsDQZisLa6ddZ5fqI8h1BB5ADhzM0="
	realManifestDigest   = "sha256:7ff676107cd00e35a6180efac9f19fe6e3804732662876eeae2e7811d4af0c1d"
	realRepo             = "ghcr.io/frostyard/ubuntu-intune"
)

func TestVerifyPayloadRealSignature(t *testing.T) {
	sum := sha256.Sum256([]byte(realPayload))
	if got := hex.EncodeToString(sum[:]); got != realPayloadDigest {
		t.Fatalf("the fixture payload hashes to %s, not %s: the text is not the real payload", got, realPayloadDigest)
	}
	pub, err := parsePublicKey(containerimage.CosignPublicKey)
	if err != nil {
		t.Fatalf("parse the shipped key: %v", err)
	}
	sig, err := base64.StdEncoding.DecodeString(realPayloadSignature)
	if err != nil {
		t.Fatalf("decode the fixture signature: %v", err)
	}
	if err := verifyPayload(pub, []byte(realPayload), sig, realManifestDigest, realRepo); err != nil {
		t.Fatalf("verifyPayload rejected the real signature: %v", err)
	}
	changed := []byte(realPayload)
	changed[len(changed)/2] ^= 0x01
	if err := verifyPayload(pub, changed, sig, realManifestDigest, realRepo); err == nil {
		t.Fatal("verifyPayload accepted a payload with 1 changed byte")
	}
}

func TestParsePublicKeyRejectsOtherKeys(t *testing.T) {
	if _, err := parsePublicKey([]byte("not a key")); err == nil {
		t.Error("parsePublicKey accepted bytes that are not a PEM block")
	}
	// A P-384 key is a valid PKIX PUBLIC KEY, but costign signs with P-256.
	private, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	if _, err := parsePublicKey(pemKey); err == nil {
		t.Error("parsePublicKey accepted a P-384 key")
	}
}

func TestSplitTaggedRef(t *testing.T) {
	tests := []struct {
		ref      string
		repo     string
		tag      string
		wantsErr bool
	}{
		{ref: "ghcr.io/frostyard/ubuntu-intune:v1.2.3", repo: "ghcr.io/frostyard/ubuntu-intune", tag: "v1.2.3"},
		{ref: "127.0.0.1:4321/frostyard/ubuntu-intune:insiders", repo: "127.0.0.1:4321/frostyard/ubuntu-intune", tag: "insiders"},
		{ref: "ghcr.io/frostyard/ubuntu-intune", wantsErr: true},
		{ref: "ghcr.io/frostyard/ubuntu-intune:", wantsErr: true},
		{ref: "ubuntu-intune:latest", wantsErr: true},
		{ref: "ghcr.io/frostyard/ubuntu-intune@sha256:" + strings.Repeat("00", 32), wantsErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			repo, tag, err := splitTaggedRef(tt.ref)
			if tt.wantsErr {
				if err == nil {
					t.Fatalf("splitTaggedRef(%q) = %q, %q, want an error", tt.ref, repo, tag)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitTaggedRef(%q): %v", tt.ref, err)
			}
			if repo != tt.repo || tag != tt.tag {
				t.Errorf("splitTaggedRef(%q) = %q, %q, want %q, %q", tt.ref, repo, tag, tt.repo, tt.tag)
			}
		})
	}
}

// TestDetailDedupesAndCaps keeps the error short: a signature manifest gains a
// layer for every signed tag, and those layers often share 1 payload.
func TestDetailDedupesAndCaps(t *testing.T) {
	same := []string{"bad", "bad", "bad"}
	if got := detail(same); got != ": bad" {
		t.Errorf("detail(%v) = %q, want %q", same, got, ": bad")
	}
	many := []string{"a", "b", "c", "d", "e", "f", "g"}
	if got, want := detail(many), ": a; b; c; d; e; 2 more"; got != want {
		t.Errorf("detail(%v) = %q, want %q", many, got, want)
	}
	if got := detail(nil); got != "" {
		t.Errorf("detail(nil) = %q, want empty", got)
	}
}

// TestVerifyRejectsCachedBlobSizeMismatch is the guard: 2 layers may name the
// same payload digest with different sizes, and a cache hit must still match
// the size the layer being checked declares.
func TestVerifyRejectsCachedBlobSizeMismatch(t *testing.T) {
	k, other := newTestKey(t), newTestKey(t)
	reg := &registry{}
	srv := reg.start(t)
	body := payload(reg.digest(t), reg.repoRef())
	bad := signedLayer(t, other, body)
	// Layer 1 caches the payload and fails on its signature. Layer 2 names the
	// same payload 1 byte shorter with a valid signature, so only the size check
	// can reject it.
	short := layer{digest: bad.digest, size: int64(len(body)) - 1, sig: k.sign(t, body)}
	reg.blobs[bad.digest] = body
	reg.sigBody = signatureManifest(t, bad, short)

	_, err := reg.verify(t, srv, k)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("error = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyCancelledContext(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{}
	srv := reg.start(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	v := &Verifier{Client: srv.Client(), PublicKey: k.public}
	_, err := v.Verify(ctx, reg.repoRef()+":"+testTag)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// TestVerifyCapsDistinctBlobs proves 1 signature manifest cannot make the check
// fetch an unbounded number of payloads: past the cap every layer fails, even a
// good one at the end.
func TestVerifyCapsDistinctBlobs(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{}
	srv := reg.start(t)
	layers := make([]layer, 0, maxDistinctBlobs+9)
	for i := range maxDistinctBlobs + 8 {
		// Each payload is distinct and names another digest, so every layer is a
		// distinct payload that fails on its claim.
		body := payload(fmt.Sprintf("sha256:%064x", i), reg.repoRef())
		reg.blobs[sha256Digest(body)] = body
		layers = append(layers, signedLayer(t, k, body))
	}
	good := payload(reg.digest(t), reg.repoRef())
	reg.blobs[sha256Digest(good)] = good
	layers = append(layers, signedLayer(t, k, good))
	reg.sigBody = signatureManifest(t, layers...)

	_, err := reg.verify(t, srv, k)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("error = %v, want ErrInvalidSignature", err)
	}
	if reg.blobsServed > maxDistinctBlobs {
		t.Errorf("the registry served %d payloads, want at most %d", reg.blobsServed, maxDistinctBlobs)
	}
}

func TestVerifyRejectsPlainTextBlobRedirect(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{blobRedirect: "http"}
	srv := reg.start(t)
	reg.signWith(t, k, payload(reg.digest(t), reg.repoRef()))

	_, err := reg.verify(t, srv, k)
	if err == nil || !strings.Contains(err.Error(), "only https redirects are allowed") {
		t.Fatalf("error = %v, want a refused downgrade", err)
	}
}

func TestVerifyFollowsHTTPSBlobRedirect(t *testing.T) {
	k := newTestKey(t)
	reg := &registry{blobRedirect: "https"}
	srv := reg.start(t)
	reg.signWith(t, k, payload(reg.digest(t), reg.repoRef()))

	got, err := reg.verify(t, srv, k)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if want := reg.repoRef() + "@" + reg.digest(t); got != want {
		t.Errorf("Verify returned %q, want %q", got, want)
	}
}
