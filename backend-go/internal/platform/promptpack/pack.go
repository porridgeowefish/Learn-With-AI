// Package promptpack defines signed, allowlisted teaching resources shared by
// the desktop and the cloud service.
package promptpack

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"unicode/utf8"
)

const MaxResponseBytes = 1 << 20

var names = []string{
	"teaching/teacher", "modes/general", "modes/history", "modes/book", "modes/concept",
	"auxiliary/section-summary", "practice", "auxiliary/notes",
}

func ResourceNames() []string { return append([]string(nil), names...) }

type Content struct {
	PackDigest string            `json:"-"`
	Version    string            `json:"version"`
	Scene      string            `json:"scene,omitempty"`
	Experiment string            `json:"experiment,omitempty"`
	Variant    string            `json:"variant,omitempty"`
	Resources  map[string]string `json:"resources"`
}

type Reference struct {
	Scene  string `json:"scene"`
	Digest string `json:"digest"`
}

func (c Content) Reference() *Reference {
	if c.PackDigest == "" || c.Scene == "" {
		return nil
	}
	return &Reference{Scene: c.Scene, Digest: c.PackDigest}
}

type Pack struct {
	Format    int     `json:"format"`
	Content   Content `json:"content"`
	Digest    string  `json:"digest"`
	Signature []byte  `json:"signature"`
}

func (p Pack) signedPayload() ([]byte, error) {
	if p.Format != 1 || p.Content.Version == "" || len(p.Content.Version) > 80 ||
		len(p.Content.Scene) > 128 || len(p.Content.Experiment) > 80 || len(p.Content.Variant) > 40 ||
		len(p.Content.Resources) == 0 || len(p.Content.Resources) > len(names) {
		return nil, errors.New("invalid prompt pack metadata")
	}
	total := 0
	for id, text := range p.Content.Resources {
		allowed := false
		for _, name := range names {
			allowed = allowed || name == id
		}
		if !allowed || text == "" || len(text) > 128<<10 || !utf8.ValidString(text) || strings.Contains(text, "{{") {
			return nil, errors.New("invalid prompt pack resource")
		}
		total += len(text)
	}
	if total > 128<<10 {
		return nil, errors.New("prompt pack exceeds content limit")
	}
	return json.Marshal(struct {
		Format  int     `json:"format"`
		Content Content `json:"content"`
	}{p.Format, p.Content})
}

func Sign(content Content, private ed25519.PrivateKey) (Pack, error) {
	if len(private) != ed25519.PrivateKeySize {
		return Pack{}, errors.New("invalid prompt signing key")
	}
	content.Resources = maps.Clone(content.Resources)
	pack := Pack{Format: 1, Content: content}
	payload, err := pack.signedPayload()
	if err != nil {
		return Pack{}, err
	}
	hash := sha256.Sum256(payload)
	pack.Digest = hex.EncodeToString(hash[:])
	pack.Signature = ed25519.Sign(private, payload)
	return pack, nil
}

func (p Pack) Verify(public ed25519.PublicKey) error {
	payload, err := p.signedPayload()
	if err != nil {
		return err
	}
	hash := sha256.Sum256(payload)
	if len(public) != ed25519.PublicKeySize || p.Digest != hex.EncodeToString(hash[:]) ||
		!ed25519.Verify(public, payload, p.Signature) {
		return errors.New("prompt pack signature did not verify")
	}
	return nil
}

func (p Pack) ETag() string { return "\"" + p.Digest + "\"" }
