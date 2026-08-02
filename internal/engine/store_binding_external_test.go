package engine_test

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/blkcor/coragent/internal/prompt"
	"github.com/blkcor/coragent/internal/store"
)

func testStoreBinding() store.ProviderBinding {
	endpoint := sha256.Sum256([]byte("scripted-offline-provider"))
	credential := sha256.Sum256([]byte("scripted-offline-credential"))
	preferences := sha256.Sum256(nil)
	binding := store.ProviderBinding{
		Adapter: "scripted-offline", WireProtocol: "scripted-v1", EndpointSHA256: hex.EncodeToString(endpoint[:]),
		CredentialSourceSHA256: hex.EncodeToString(credential[:]),
		Model:                  "scripted-offline", ContextWindow: 32000, MaxOutputTokens: 8000, ToolChoice: "auto",
		UserPreferencesSHA256: hex.EncodeToString(preferences[:]), PromptVersion: prompt.PromptVersion,
	}
	binding.Digest = binding.ComputeDigest()
	return binding
}
