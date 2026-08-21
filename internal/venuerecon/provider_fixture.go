package venuerecon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
)

// providerFixturePage is the bounded synthetic wire contract used to qualify
// the pure adapters without credentials or network access. Each page repeats
// its account header so contradictory pagination fails closed.
type providerFixturePage struct {
	AccountID    string         `json:"account_id"`
	Currency     string         `json:"currency"`
	ProviderAsOf string         `json:"provider_as_of"`
	Cash         string         `json:"cash"`
	Equity       string         `json:"equity"`
	Positions    []wirePosition `json:"positions"`
	Fills        []wireFill     `json:"fills"`
}

type wirePosition struct {
	ContractID string `json:"contract_id"`
	Quantity   string `json:"quantity"`
	Currency   string `json:"currency"`
	SourceAt   string `json:"source_at"`
}

type wireFill struct {
	SourceID                 string `json:"source_id"`
	OriginalSourceID         string `json:"original_source_id"`
	ObservationClass         string `json:"observation_class"`
	ObservationDiscriminator string `json:"observation_discriminator"`
	ExternalOrderID          string `json:"external_order_id"`
	ClientOrderID            string `json:"client_order_id"`
	ContractID               string `json:"contract_id"`
	Side                     string `json:"side"`
	Quantity                 string `json:"quantity"`
	Price                    string `json:"price"`
	Fee                      string `json:"fee"`
	Currency                 string `json:"currency"`
	SourceRevision           string `json:"source_revision"`
	SourceAt                 string `json:"source_at"`
}

func deriveCaptureFromRawPages(scope CaptureInput) (CaptureInput, error) {
	derived := CaptureInput{
		Provider: scope.Provider, Namespace: scope.Namespace, AccountID: scope.AccountID, Currency: scope.Currency,
		HorizonStart: scope.HorizonStart, HorizonEnd: scope.HorizonEnd,
		CaptureStart: scope.CaptureStart, CaptureEnd: scope.CaptureEnd, Pages: cloneRawPages(scope.Pages),
	}
	for index, page := range scope.Pages {
		var wire providerFixturePage
		decoder := json.NewDecoder(bytes.NewReader(page.Raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&wire); err != nil {
			return CaptureInput{}, fmt.Errorf("page %d: %w", index, err)
		}
		if err := requireJSONEOF(decoder); err != nil {
			return CaptureInput{}, fmt.Errorf("page %d: %w", index, err)
		}
		providerAsOf, err := parseWireTime(wire.ProviderAsOf)
		if err != nil {
			return CaptureInput{}, fmt.Errorf("page %d provider time: %w", index, err)
		}
		if index == 0 {
			derived.ProviderAsOf, derived.Cash, derived.Equity = providerAsOf, wire.Cash, wire.Equity
		} else if wire.AccountID != derived.AccountID || wire.Currency != derived.Currency ||
			wire.Cash != derived.Cash || wire.Equity != derived.Equity || !providerAsOf.Equal(derived.ProviderAsOf) {
			return CaptureInput{}, fmt.Errorf("page %d account header contradicts prior page", index)
		}
		if wire.AccountID != derived.AccountID || wire.Currency != derived.Currency {
			return CaptureInput{}, fmt.Errorf("page %d account scope mismatch", index)
		}
		for _, row := range wire.Positions {
			sourceAt, err := parseWireTime(row.SourceAt)
			if err != nil {
				return CaptureInput{}, fmt.Errorf("page %d position time: %w", index, err)
			}
			derived.Positions = append(derived.Positions, PositionInput{ContractID: row.ContractID, Quantity: row.Quantity, Currency: row.Currency, SourceAt: sourceAt})
		}
		for _, row := range wire.Fills {
			sourceAt, err := parseWireTime(row.SourceAt)
			if err != nil {
				return CaptureInput{}, fmt.Errorf("page %d fill time: %w", index, err)
			}
			derived.Fills = append(derived.Fills, FillInput{
				SourceID: row.SourceID, OriginalSourceID: row.OriginalSourceID,
				ObservationClass: lifecycle.ObservationClass(row.ObservationClass), ObservationDiscriminator: row.ObservationDiscriminator,
				ExternalOrderID: row.ExternalOrderID, ClientOrderID: row.ClientOrderID, ContractID: row.ContractID,
				Side: lifecycle.Side(row.Side), Quantity: row.Quantity, Price: row.Price, Fee: row.Fee,
				Currency: row.Currency, SourceRevision: row.SourceRevision, SourceAt: sourceAt,
			})
		}
	}
	return derived, nil
}

func parseWireTime(raw string) (time.Time, error) {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || !validEvidenceTime(value) || canonicalTime(value) != raw {
		return time.Time{}, fmt.Errorf("time must be canonical UTC microsecond form")
	}
	return value, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("page must contain exactly one JSON value")
	}
	return nil
}

func cloneRawPages(pages []RawPage) []RawPage {
	result := make([]RawPage, len(pages))
	for index, page := range pages {
		result[index] = RawPage{Cursor: page.Cursor, NextCursor: page.NextCursor, Terminal: page.Terminal, Raw: append(json.RawMessage(nil), page.Raw...)}
	}
	return result
}
