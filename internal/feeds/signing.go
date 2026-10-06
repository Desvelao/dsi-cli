package feeds

import (
	"crypto/ed25519"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Desvelao/dsipy/internal/crypto"
	"github.com/Desvelao/dsipy/internal/pyutil"
)

// Verification statuses of feed items.
const (
	StatusValid    = "valid"
	StatusInvalid  = "invalid"
	StatusUnsigned = "unsigned"
)

// ItemResult is the verification outcome of one RSS item.
type ItemResult struct {
	Title  string
	Guid   *string
	Status string
	Reason string
}

// node is a minimal element tree (like ElementTree: text is the text before the first child).
type node struct {
	name     xml.Name
	attrs    []xml.Attr
	text     string
	hasChild bool
	children []*node
}

func (n *node) attr(name string) string {
	for _, a := range n.attrs {
		if a.Name.Local == name && a.Name.Space == "" {
			return a.Value
		}
	}
	return ""
}

func (n *node) child(local string) *node {
	for _, c := range n.children {
		if c.name.Local == local && c.name.Space == "" {
			return c
		}
	}
	return nil
}

func parseXML(text string) (*node, error) {
	dec := xml.NewDecoder(strings.NewReader(text))
	dec.Strict = true
	var root *node
	var stack []*node
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{name: t.Name, attrs: t.Attr}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.hasChild = true
				parent.children = append(parent.children, n)
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				if cur := stack[len(stack)-1]; !cur.hasChild {
					cur.text += string(t)
				}
			}
		}
	}
	if root == nil {
		return nil, errors.New("no element found")
	}
	return root, nil
}

func iterItems(n *node, out *[]*node) {
	if n.name.Local == "item" && n.name.Space == "" {
		*out = append(*out, n)
	}
	for _, c := range n.children {
		iterItems(c, out)
	}
}

// VerifyFeedItems verifies every item of an RSS document against the public
// keys (by key-id, their Base64 DER value). It fails if the document is not
// well-formed or declares a DTD.
func VerifyFeedItems(xmlText string, keys map[string]ed25519.PublicKey) ([]ItemResult, error) {
	if strings.Contains(xmlText, "<!DOCTYPE") || strings.Contains(xmlText, "<!ENTITY") {
		return nil, errors.New("DTD and entity declarations are not allowed in feeds")
	}
	root, err := parseXML(xmlText)
	if err != nil {
		return nil, fmt.Errorf("Invalid XML: %v", err)
	}
	var items []*node
	iterItems(root, &items)

	results := []ItemResult{}
	for _, item := range items {
		title := ""
		if c := item.child("title"); c != nil {
			title = c.text
		}
		var guid *string
		if c := item.child("guid"); c != nil {
			g := c.text
			guid = &g
		}
		sig := item.child("signature")
		if sig == nil || pyutil.Strip(sig.text) == "" {
			results = append(results, ItemResult{Title: title, Guid: guid, Status: StatusUnsigned})
			continue
		}
		keyID := sig.attr("key-id")
		if keyID == "" {
			keyID = sig.attr("keyId")
		}
		if alg := sig.attr("alg"); alg != "" && alg != "ed25519" {
			results = append(results, ItemResult{title, guid, StatusInvalid, fmt.Sprintf("unsupported alg '%s'", alg)})
			continue
		}
		var key ed25519.PublicKey
		if keyID != "" {
			key = keys[keyID]
		}
		if key == nil {
			shown := keyID
			if shown == "" {
				shown = "None"
			}
			results = append(results, ItemResult{title, guid, StatusInvalid, fmt.Sprintf("no matching key for key-id '%s'", shown)})
			continue
		}
		pubDate, description := "", ""
		if c := item.child("pubDate"); c != nil {
			pubDate = c.text
		}
		if c := item.child("description"); c != nil {
			description = c.text
		}
		if crypto.VerifyFeedSignature(key, pubDate, title, description, pyutil.Strip(sig.text)) {
			results = append(results, ItemResult{Title: title, Guid: guid, Status: StatusValid})
		} else {
			results = append(results, ItemResult{title, guid, StatusInvalid, "bad signature"})
		}
	}
	return results, nil
}
