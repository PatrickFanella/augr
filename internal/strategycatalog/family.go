// Package strategycatalog owns immutable strategy families, versions,
// experiment declarations, and inert deployment proposals. It does not run or
// activate strategies.
package strategycatalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

const (
	FamilySchemaV1 = "strategy-family-v1"
	familyDomain   = "strategy-family"
)

var (
	slugPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type FamilyInput struct {
	Slug         string
	Name         string
	Thesis       string
	AssetClasses []instrument.AssetClass
}

type familyCanonical struct {
	Schema       string                  `json:"schema"`
	Slug         string                  `json:"slug"`
	Name         string                  `json:"name"`
	Thesis       string                  `json:"thesis"`
	AssetClasses []instrument.AssetClass `json:"asset_classes"`
}

type Family struct {
	canonical familyCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewFamily(input FamilyInput) (*Family, error) {
	if !slugPattern.MatchString(input.Slug) || len(input.Slug) > 96 ||
		!canonicalText(input.Name, 160) || !canonicalText(input.Thesis, 4096) {
		return nil, fmt.Errorf("strategy family identity is invalid")
	}
	assetClasses := append([]instrument.AssetClass(nil), input.AssetClasses...)
	sort.Slice(assetClasses, func(i, j int) bool { return assetClasses[i] < assetClasses[j] })
	if len(assetClasses) == 0 {
		return nil, fmt.Errorf("strategy family requires asset classes")
	}
	for index, assetClass := range assetClasses {
		if !validFamilyAssetClass(assetClass) || index > 0 && assetClasses[index-1] == assetClass {
			return nil, fmt.Errorf("strategy family asset classes are invalid")
		}
	}
	canonical := familyCanonical{
		Schema: FamilySchemaV1, Slug: input.Slug, Name: input.Name,
		Thesis: input.Thesis, AssetClasses: assetClasses,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal strategy family: %w", err)
	}
	return &Family{
		canonical: canonical, bytes: encoded, digest: hashBytes(encoded),
		id: economicid.DeterministicUUID(familyDomain, input.Slug),
	}, nil
}

func FamilyFromCanonical(id uuid.UUID, digest string, raw []byte) (*Family, error) {
	if id == uuid.Nil || !sha256Pattern.MatchString(digest) || hashBytes(raw) != digest {
		return nil, fmt.Errorf("strategy family envelope is invalid")
	}
	var canonical familyCanonical
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	family, err := NewFamily(FamilyInput{
		Slug: canonical.Slug, Name: canonical.Name, Thesis: canonical.Thesis,
		AssetClasses: canonical.AssetClasses,
	})
	if err != nil {
		return nil, err
	}
	if canonical.Schema != FamilySchemaV1 || family.ID() != id || family.Digest() != digest || !bytes.Equal(family.bytes, raw) {
		return nil, fmt.Errorf("strategy family canonical identity does not reconstruct")
	}
	return family, nil
}

func (family *Family) ID() uuid.UUID {
	if family == nil {
		return uuid.Nil
	}
	return family.id
}

func (family *Family) Digest() string {
	if family == nil {
		return ""
	}
	return family.digest
}

func (family *Family) CanonicalBytes() json.RawMessage {
	if family == nil {
		return nil
	}
	return append(json.RawMessage(nil), family.bytes...)
}

func (family *Family) Slug() string {
	if family == nil {
		return ""
	}
	return family.canonical.Slug
}

func (family *Family) Name() string {
	if family == nil {
		return ""
	}
	return family.canonical.Name
}

func (family *Family) Thesis() string {
	if family == nil {
		return ""
	}
	return family.canonical.Thesis
}

func (family *Family) AssetClasses() []instrument.AssetClass {
	if family == nil {
		return nil
	}
	return append([]instrument.AssetClass(nil), family.canonical.AssetClasses...)
}

func validFamilyAssetClass(value instrument.AssetClass) bool {
	switch value {
	case instrument.AssetClassEquity, instrument.AssetClassETF, instrument.AssetClassOption,
		instrument.AssetClassCryptoSpot, instrument.AssetClassPredictionContract, instrument.AssetClassFuture:
		return true
	default:
		return false
	}
}

func canonicalText(value string, maximum int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= maximum
}

func hashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("canonical JSON contains multiple values")
		}
		return err
	}
	return nil
}
