package edgar

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// Provider retrieves SEC filing data from EDGAR.
type Provider struct {
	client    *Client
	tickerMap *TickerMap
	logger    *slog.Logger
}

// submissionsResponse represents the top-level EDGAR submissions JSON.
type submissionsResponse struct {
	Filings submissionsFilings `json:"filings"`
}

// submissionsFilings holds the recent filings in columnar format.
type submissionsFilings struct {
	Recent recentFilings `json:"recent"`
}

// recentFilings represents the columnar arrays within filings.recent.
type recentFilings struct {
	AccessionNumber []string `json:"accessionNumber"`
	FilingDate      []string `json:"filingDate"`
	Form            []string `json:"form"`
	PrimaryDocument []string `json:"primaryDocument"`
	ReportDate      []string `json:"reportDate"`
}

// NewProvider constructs an EDGAR filing provider.
func NewProvider(client *Client, logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{
		client:    client,
		tickerMap: NewTickerMap(),
		logger:    logger,
	}
}

// LoadTickerMap initialises the CIK lookup table from the SEC.
func (p *Provider) LoadTickerMap(ctx context.Context) error {
	return p.tickerMap.Load(ctx, p.client)
}

// GetFilings fetches filing history from the EDGAR submissions API.
func (p *Provider) GetFilings(ctx context.Context, ticker, formType string, from, to time.Time) ([]domain.SECFiling, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if ticker == "" {
		return nil, fmt.Errorf("edgar: ticker is required")
	}

	cik, ok := p.tickerMap.GetCIK(ticker)
	if !ok {
		return nil, fmt.Errorf("edgar: unknown ticker %q", ticker)
	}

	url := fmt.Sprintf("%s/submissions/CIK%s.json", baseURL, cik)
	body, err := p.client.Get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("edgar: GetFilings: %w", err)
	}

	var resp submissionsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("edgar: decode submissions: %w", err)
	}

	recent := resp.Filings.Recent
	n := len(recent.AccessionNumber)

	var filings []domain.SECFiling
	for i := 0; i < n; i++ {
		// Filter by form type.
		if formType != "" && !strings.EqualFold(recent.Form[i], formType) {
			continue
		}

		filedDate, _ := time.Parse("2006-01-02", recent.FilingDate[i])
		if filedDate.Before(from) || filedDate.After(to) {
			continue
		}

		reportDate, _ := time.Parse("2006-01-02", recent.ReportDate[i])

		// Build the Archives URL: accession number with dashes removed for the directory.
		accession := recent.AccessionNumber[i]
		accessionNoDashes := strings.ReplaceAll(accession, "-", "")
		cikNum := strings.TrimLeft(cik, "0")
		if cikNum == "" {
			cikNum = "0"
		}

		fileURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/%s",
			cikNum, accessionNoDashes, recent.PrimaryDocument[i])

		filings = append(filings, domain.SECFiling{
			Symbol:       ticker,
			Form:         recent.Form[i],
			FiledDate:    filedDate,
			AcceptedDate: filedDate, // EDGAR submissions API only provides filing date
			ReportDate:   reportDate,
			URL:          fileURL,
			AccessNumber: accession,
		})
	}

	p.logger.Debug("edgar: GetFilings",
		slog.String("ticker", ticker),
		slog.String("form", formType),
		slog.Int("results", len(filings)),
	)

	return filings, nil
}
