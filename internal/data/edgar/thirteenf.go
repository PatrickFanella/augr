package edgar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const archivesBaseURL = "https://www.sec.gov/Archives/edgar/data"

// ThirteenFFiling is a normalized Form 13F portfolio disclosure.
type ThirteenFFiling struct {
	CIK          string
	Accession    string
	Form         string
	ReportPeriod time.Time
	FiledAt      time.Time
	SourceURL    string
	ContentHash  string
	Holdings     []domain.CopyPortfolioHolding
}

type archiveIndex struct {
	Directory struct {
		Items []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"item"`
	} `json:"directory"`
}

type informationTableXML struct {
	Entries []informationTableEntryXML `xml:"infoTable"`
}

type informationTableEntryXML struct {
	IssuerName           string `xml:"nameOfIssuer"`
	TitleOfClass         string `xml:"titleOfClass"`
	CUSIP                string `xml:"cusip"`
	FIGI                 string `xml:"figi"`
	Value                string `xml:"value"`
	SharesOrPrincipal    string `xml:"shrsOrPrnAmt>sshPrnamt"`
	AmountType           string `xml:"shrsOrPrnAmt>sshPrnamtType"`
	PutCall              string `xml:"putCall"`
	InvestmentDiscretion string `xml:"investmentDiscretion"`
	VotingSole           string `xml:"votingAuthority>Sole"`
	VotingShared         string `xml:"votingAuthority>Shared"`
	VotingNone           string `xml:"votingAuthority>None"`
}

// Parse13FInformationTable parses the XML information table attached to a
// Form 13F filing. SEC-reported value is in thousands of dollars and is
// normalized here to dollars.
func Parse13FInformationTable(data []byte) ([]domain.CopyPortfolioHolding, error) {
	var table informationTableXML
	if err := xml.Unmarshal(data, &table); err != nil {
		return nil, fmt.Errorf("edgar: parse 13f information table: %w", err)
	}
	if len(table.Entries) == 0 {
		return nil, fmt.Errorf("edgar: 13f information table has no holdings")
	}

	holdings := make([]domain.CopyPortfolioHolding, 0, len(table.Entries))
	for i, entry := range table.Entries {
		cusip := strings.ToUpper(strings.TrimSpace(entry.CUSIP))
		issuer := strings.TrimSpace(entry.IssuerName)
		if cusip == "" || issuer == "" {
			return nil, fmt.Errorf("edgar: 13f holding %d missing issuer or cusip", i)
		}
		value, err := parseSECNumber(entry.Value)
		if err != nil {
			return nil, fmt.Errorf("edgar: 13f holding %d value: %w", i, err)
		}
		amount, err := parseSECNumber(entry.SharesOrPrincipal)
		if err != nil {
			return nil, fmt.Errorf("edgar: 13f holding %d shares: %w", i, err)
		}
		sole, err := parseOptionalSECNumber(entry.VotingSole)
		if err != nil {
			return nil, fmt.Errorf("edgar: 13f holding %d sole voting: %w", i, err)
		}
		shared, err := parseOptionalSECNumber(entry.VotingShared)
		if err != nil {
			return nil, fmt.Errorf("edgar: 13f holding %d shared voting: %w", i, err)
		}
		none, err := parseOptionalSECNumber(entry.VotingNone)
		if err != nil {
			return nil, fmt.Errorf("edgar: 13f holding %d none voting: %w", i, err)
		}
		holdings = append(holdings, domain.CopyPortfolioHolding{
			IssuerName: issuer, TitleOfClass: strings.TrimSpace(entry.TitleOfClass), CUSIP: cusip,
			FIGI: strings.ToUpper(strings.TrimSpace(entry.FIGI)), DisclosedValue: value * 1000,
			SharesOrPrincipal: amount, AmountType: strings.ToUpper(strings.TrimSpace(entry.AmountType)),
			PutCall: strings.ToUpper(strings.TrimSpace(entry.PutCall)), InvestmentDiscretion: strings.TrimSpace(entry.InvestmentDiscretion),
			VotingSole: sole, VotingShared: shared, VotingNone: none,
		})
	}

	sort.SliceStable(holdings, func(i, j int) bool {
		if holdings[i].DisclosedValue != holdings[j].DisclosedValue {
			return holdings[i].DisclosedValue > holdings[j].DisclosedValue
		}
		return holdings[i].CUSIP < holdings[j].CUSIP
	})
	return holdings, nil
}

func parseSECNumber(raw string) (float64, error) {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), ",", "")
	if raw == "" {
		return 0, fmt.Errorf("value is empty")
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid non-negative number %q", raw)
	}
	return v, nil
}

func parseOptionalSECNumber(raw string) (float64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return parseSECNumber(raw)
}

// FetchLatest13F fetches and normalizes the newest original or amended Form
// 13F information table listed in the manager's EDGAR submissions feed.
func (p *Provider) FetchLatest13F(ctx context.Context, cik string) (*ThirteenFFiling, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("edgar: provider client is required")
	}
	cik = domain.NormalizeSECCIK(cik)
	paddedCIK := fmt.Sprintf("%010s", cik)
	body, err := p.client.Get(ctx, fmt.Sprintf("%s/submissions/CIK%s.json", baseURL, paddedCIK))
	if err != nil {
		return nil, fmt.Errorf("edgar: fetch manager submissions: %w", err)
	}
	var submissions submissionsResponse
	if err := json.Unmarshal(body, &submissions); err != nil {
		return nil, fmt.Errorf("edgar: decode manager submissions: %w", err)
	}

	recent := submissions.Filings.Recent
	for i, form := range recent.Form {
		if form != "13F-HR" && form != "13F-HR/A" {
			continue
		}
		if i >= len(recent.AccessionNumber) || i >= len(recent.ReportDate) || i >= len(recent.FilingDate) {
			continue
		}
		accession := recent.AccessionNumber[i]
		filing, err := p.fetch13FInformationTable(ctx, cik, accession, valueAt(recent.PrimaryDocument, i))
		if err != nil {
			p.logger.Warn("edgar: skip unusable 13f filing", "cik", cik, "accession", accession, "error", err)
			continue
		}
		filing.Form = form
		filing.ReportPeriod, _ = time.Parse("2006-01-02", recent.ReportDate[i])
		filing.FiledAt = parseEDGARDateTime(valueAt(recent.AcceptanceDateTime, i), recent.FilingDate[i])
		return filing, nil
	}
	return nil, fmt.Errorf("edgar: no usable 13f filing found for CIK %s", cik)
}

func (p *Provider) fetch13FInformationTable(ctx context.Context, cik, accession, primaryDocument string) (*ThirteenFFiling, error) {
	accessionCompact := strings.ReplaceAll(accession, "-", "")
	base := fmt.Sprintf("%s/%s/%s", archivesBaseURL, cik, accessionCompact)
	indexBody, err := p.client.Get(ctx, base+"/index.json")
	if err != nil {
		return nil, err
	}
	var index archiveIndex
	if err := json.Unmarshal(indexBody, &index); err != nil {
		return nil, fmt.Errorf("decode archive index: %w", err)
	}

	candidates := make([]string, 0)
	for _, item := range index.Directory.Items {
		name := strings.TrimSpace(item.Name)
		if !strings.EqualFold(filepath.Ext(name), ".xml") {
			continue
		}
		if name != primaryDocument {
			candidates = append(candidates, name)
		}
	}
	if primaryDocument != "" && strings.EqualFold(filepath.Ext(primaryDocument), ".xml") {
		candidates = append(candidates, primaryDocument)
	}
	for _, name := range candidates {
		doc, err := p.client.Get(ctx, base+"/"+name)
		if err != nil {
			continue
		}
		holdings, err := Parse13FInformationTable(doc)
		if err != nil {
			continue
		}
		hash := sha256.Sum256(doc)
		return &ThirteenFFiling{CIK: cik, Accession: accession, SourceURL: base + "/" + name, ContentHash: hex.EncodeToString(hash[:]), Holdings: holdings}, nil
	}
	return nil, fmt.Errorf("no parseable information-table XML in filing")
}

func valueAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func parseEDGARDateTime(accepted, filed string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05.000Z", "2006-01-02T15:04:05Z", "2006-01-02T15:04:05.000"} {
		if parsed, err := time.Parse(layout, accepted); err == nil {
			return parsed.UTC()
		}
	}
	parsed, _ := time.Parse("2006-01-02", filed)
	return parsed.UTC()
}
