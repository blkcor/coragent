package dataproj

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func fakeOpenAIKey() string {
	return strings.Join([]string{"sk", "0123456789abcdefghij012345"}, "-")
}

func TestVersionedCredentialCorpusProjectionMatrix(t *testing.T) {
	data, err := os.ReadFile("testdata/credential-corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	const expectedDigest = "e21ec4eb1b0aa221cfb67075d7790a235c9fa6ac8659c18ef4537941945ee6f6"
	if got := hex.EncodeToString(digest[:]); got != expectedDigest {
		t.Fatalf("credential corpus digest = %s, want %s", got, expectedDigest)
	}
	var corpus struct {
		Version string `json:"version"`
		Cases   []struct {
			Rule  string   `json:"rule"`
			Parts []string `json:"parts"`
			Join  string   `json:"join"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Version != "credential-corpus-v2" || len(corpus.Cases) != 9 {
		t.Fatalf("corpus metadata = %+v", corpus)
	}
	projector := New()
	for _, testCase := range corpus.Cases {
		t.Run(testCase.Rule, func(t *testing.T) {
			secret := strings.Join(testCase.Parts, testCase.Join)
			if !projector.Detector().Contains(secret) {
				t.Fatalf("detector did not match rule %s", testCase.Rule)
			}
			projected := projector.ProjectText("prefix " + secret + " suffix")
			if projected.RedactedCount != 1 || strings.Contains(projected.Content, secret) {
				t.Fatalf("unsafe text projection for %s", testCase.Rule)
			}
			stream := NewStreamRedactor(projector.Detector())
			mid := len(secret) / 2
			streamed := stream.Write("prefix "+secret[:mid]) + stream.Write(secret[mid:]+" suffix ") + stream.Close()
			if strings.Contains(streamed, secret) || !strings.Contains(streamed, "REDACTED") {
				t.Fatalf("unsafe stream projection for %s", testCase.Rule)
			}
		})
	}
}

func TestDetectorRedactsWithoutEcho(t *testing.T) {
	p := New()
	secret := fakeOpenAIKey()
	raw := "before " + secret + " after"
	got := p.ProjectText(raw)
	if got.Class != ClassSensitive || got.RedactedCount != 1 {
		t.Fatalf("projection metadata = %+v", got)
	}
	if strings.Contains(got.Content, secret) || got.Content != "before [REDACTED:CREDENTIAL] after" {
		t.Fatalf("unsafe projection %q", got.Content)
	}
	if err := p.ValidatePrompt(raw); err != ErrSensitivePrompt {
		t.Fatalf("ValidatePrompt = %v", err)
	}
}

func TestStreamRedactorHandlesChunkBoundary(t *testing.T) {
	secret := fakeOpenAIKey()
	r := NewStreamRedactor(nil)
	var got string
	got += r.Write("safe " + secret[:8])
	got += r.Write(secret[8:] + " tail ")
	got += r.Close()
	if strings.Contains(got, secret) {
		t.Fatalf("stream leaked credential: %q", got)
	}
	if got != "safe [REDACTED:CREDENTIAL] tail " {
		t.Fatalf("stream projection = %q", got)
	}
}

func TestPrivateKeyBlockIsRedactedAcrossLinesAndChunks(t *testing.T) {
	secretBody := "synthetic-private-body-line"
	raw := "before\n-----BEGIN PRIVATE KEY-----\n" + secretBody + "\n-----END PRIVATE KEY-----\nafter"
	projected := New().ProjectText(raw)
	if strings.Contains(projected.Content, secretBody) || !strings.Contains(projected.Content, "REDACTED") {
		t.Fatalf("whole text leaked private key body: %q", projected.Content)
	}
	stream := NewStreamRedactor(nil)
	got := stream.Write("before\n-----BEGIN PRIVATE ")
	got += stream.Write("KEY-----\n" + secretBody[:10])
	got += stream.Write(secretBody[10:] + "\n-----END PRIVATE KEY-----\nafter")
	got += stream.Close()
	if strings.Contains(got, secretBody) || !strings.Contains(got, "REDACTED") || !strings.Contains(got, "after") {
		t.Fatalf("stream leaked private key body: %q", got)
	}
}

func TestProtectedPathPolicy(t *testing.T) {
	protected := []string{".env", "config/.env.local", ".ssh/id_ed25519", "tls/server.pem", ".git-credentials", ".npmrc", ".pypirc", ".docker/config.json", ".config/gh/hosts.yml"}
	for _, name := range protected {
		if !ProtectedPath(name) {
			t.Errorf("%q is not protected", name)
		}
	}
	ordinary := []string{".env.example", "docs/key.md", "main.go"}
	for _, name := range ordinary {
		if ProtectedPath(name) {
			t.Errorf("%q unexpectedly protected", name)
		}
	}
}
