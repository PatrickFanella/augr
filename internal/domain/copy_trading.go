package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CopyLeaderEntityType string

const (
	CopyLeaderIndividual  CopyLeaderEntityType = "individual"
	CopyLeaderInstitution CopyLeaderEntityType = "institution"
)

type CopyIdentityStatus string

const (
	CopyIdentityUnverified   CopyIdentityStatus = "unverified"
	CopyIdentityPublicFiling CopyIdentityStatus = "public_filing_verified"
	CopyIdentityConnected    CopyIdentityStatus = "connected_verified"
)

type CopySourceType string

const (
	CopySourceSEC13F          CopySourceType = "sec_13f"
	CopySourceSECForm4        CopySourceType = "sec_form4"
	CopySourceConnectedBroker CopySourceType = "connected_broker"
	CopySourceKalshiConnected CopySourceType = "kalshi_connected"
)

type CopySubscriptionStatus string

const (
	CopySubscriptionDraft        CopySubscriptionStatus = "draft"
	CopySubscriptionPreviewed    CopySubscriptionStatus = "previewed"
	CopySubscriptionPaperActive  CopySubscriptionStatus = "paper_active"
	CopySubscriptionPaused       CopySubscriptionStatus = "paused"
	CopySubscriptionLiveEligible CopySubscriptionStatus = "live_eligible"
	CopySubscriptionLiveActive   CopySubscriptionStatus = "live_active"
	CopySubscriptionStopped      CopySubscriptionStatus = "stopped"
)

type CopySizingMethod string

const (
	CopySizingTargetWeight  CopySizingMethod = "target_weight"
	CopySizingFixedNotional CopySizingMethod = "fixed_notional"
	CopySizingSourceRatio   CopySizingMethod = "source_ratio"
)

type CopyLeader struct {
	ID             uuid.UUID            `json:"id"`
	EntityType     CopyLeaderEntityType `json:"entity_type"`
	DisplayName    string               `json:"display_name"`
	SECCIK         string               `json:"sec_cik,omitempty"`
	IdentityStatus CopyIdentityStatus   `json:"identity_status"`
	Metadata       json.RawMessage      `json:"metadata,omitempty"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
}

func (l *CopyLeader) Validate() error {
	if l == nil {
		return fmt.Errorf("leader is required")
	}
	l.DisplayName = strings.TrimSpace(l.DisplayName)
	l.SECCIK = NormalizeSECCIK(l.SECCIK)
	if l.DisplayName == "" {
		return fmt.Errorf("display_name is required")
	}
	if l.EntityType != CopyLeaderIndividual && l.EntityType != CopyLeaderInstitution {
		return fmt.Errorf("invalid entity_type %q", l.EntityType)
	}
	if l.IdentityStatus == "" {
		l.IdentityStatus = CopyIdentityUnverified
	}
	return nil
}

type CopyLeaderSource struct {
	ID             uuid.UUID       `json:"id"`
	LeaderID       uuid.UUID       `json:"leader_id"`
	Provider       string          `json:"provider"`
	SourceType     CopySourceType  `json:"source_type"`
	ExternalKey    string          `json:"external_key"`
	Status         string          `json:"status"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	Checkpoint     json.RawMessage `json:"checkpoint,omitempty"`
	LastObservedAt *time.Time      `json:"last_observed_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func (s *CopyLeaderSource) Validate() error {
	if s == nil || s.LeaderID == uuid.Nil {
		return fmt.Errorf("leader_id is required")
	}
	s.Provider = strings.ToLower(strings.TrimSpace(s.Provider))
	s.ExternalKey = strings.TrimSpace(s.ExternalKey)
	if s.Provider == "" || s.ExternalKey == "" {
		return fmt.Errorf("provider and external_key are required")
	}
	if s.SourceType != CopySourceSEC13F && s.SourceType != CopySourceSECForm4 && s.SourceType != CopySourceConnectedBroker && s.SourceType != CopySourceKalshiConnected {
		return fmt.Errorf("invalid source_type %q", s.SourceType)
	}
	if s.SourceType == CopySourceSEC13F || s.SourceType == CopySourceSECForm4 {
		s.ExternalKey = NormalizeSECCIK(s.ExternalKey)
	}
	if s.Status == "" {
		s.Status = "active"
	}
	return nil
}

type CopySourceObservation struct {
	ID                    uuid.UUID       `json:"id"`
	SourceID              uuid.UUID       `json:"source_id"`
	ProviderObservationID string          `json:"provider_observation_id"`
	ObservationKind       string          `json:"observation_kind"`
	SchemaVersion         int             `json:"schema_version"`
	EffectiveAt           time.Time       `json:"effective_at"`
	PublishedAt           time.Time       `json:"published_at"`
	ObservedAt            time.Time       `json:"observed_at"`
	AmendmentNumber       int             `json:"amendment_number"`
	SupersedesID          *uuid.UUID      `json:"supersedes_id,omitempty"`
	Status                string          `json:"status"`
	ContentHash           string          `json:"content_hash"`
	NormalizedPayload     json.RawMessage `json:"normalized_payload,omitempty"`
	SourceURL             string          `json:"source_url,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
}

type CopyPortfolioSnapshot struct {
	ID                  uuid.UUID              `json:"id"`
	ObservationID       uuid.UUID              `json:"observation_id"`
	ReportPeriod        time.Time              `json:"report_period"`
	TotalDisclosedValue float64                `json:"total_disclosed_value"`
	HoldingCount        int                    `json:"holding_count"`
	Holdings            []CopyPortfolioHolding `json:"holdings,omitempty"`
	CreatedAt           time.Time              `json:"created_at"`
}

type CopyPortfolioHolding struct {
	ID                   uuid.UUID `json:"id"`
	SnapshotID           uuid.UUID `json:"snapshot_id"`
	IssuerName           string    `json:"issuer_name"`
	TitleOfClass         string    `json:"title_of_class,omitempty"`
	CUSIP                string    `json:"cusip"`
	FIGI                 string    `json:"figi,omitempty"`
	DisclosedValue       float64   `json:"disclosed_value"`
	SharesOrPrincipal    float64   `json:"shares_or_principal"`
	AmountType           string    `json:"amount_type,omitempty"`
	PutCall              string    `json:"put_call,omitempty"`
	InvestmentDiscretion string    `json:"investment_discretion,omitempty"`
	VotingSole           float64   `json:"voting_sole"`
	VotingShared         float64   `json:"voting_shared"`
	VotingNone           float64   `json:"voting_none"`
	CreatedAt            time.Time `json:"created_at"`
}

type CopyInstrumentMapping struct {
	ID              uuid.UUID  `json:"id"`
	Provider        string     `json:"provider"`
	IdentifierType  string     `json:"identifier_type"`
	IdentifierValue string     `json:"identifier_value"`
	InstrumentKey   string     `json:"instrument_key"`
	Ticker          string     `json:"ticker"`
	Confidence      string     `json:"confidence"`
	MappingMethod   string     `json:"mapping_method"`
	ValidFrom       time.Time  `json:"valid_from"`
	ValidTo         *time.Time `json:"valid_to,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (m *CopyInstrumentMapping) Validate() error {
	if m == nil {
		return fmt.Errorf("mapping is required")
	}
	m.Provider = strings.ToLower(strings.TrimSpace(m.Provider))
	m.IdentifierType = strings.ToLower(strings.TrimSpace(m.IdentifierType))
	m.IdentifierValue = strings.ToUpper(strings.TrimSpace(m.IdentifierValue))
	m.InstrumentKey = strings.ToUpper(strings.TrimSpace(m.InstrumentKey))
	m.Ticker = strings.ToUpper(strings.TrimSpace(m.Ticker))
	if m.Provider == "" || m.IdentifierType == "" || m.IdentifierValue == "" || m.Ticker == "" {
		return fmt.Errorf("provider, identifier_type, identifier_value, and ticker are required")
	}
	if m.InstrumentKey == "" {
		m.InstrumentKey = m.Ticker
	}
	if m.Confidence == "" {
		m.Confidence = "manual_verified"
	}
	if m.MappingMethod == "" {
		m.MappingMethod = "manual"
	}
	return nil
}

type CopySubscription struct {
	ID                 uuid.UUID              `json:"id"`
	LeaderID           uuid.UUID              `json:"leader_id"`
	SourceID           uuid.UUID              `json:"source_id"`
	StrategyID         uuid.UUID              `json:"strategy_id"`
	Status             CopySubscriptionStatus `json:"status"`
	IsPaper            bool                   `json:"is_paper"`
	Method             CopySizingMethod       `json:"method"`
	CapitalBudget      float64                `json:"capital_budget"`
	CashBufferPct      float64                `json:"cash_buffer_pct"`
	TopN               int                    `json:"top_n"`
	MinSourceWeight    float64                `json:"min_source_weight"`
	MaxPositionWeight  float64                `json:"max_position_weight"`
	MaxTurnoverPct     float64                `json:"max_turnover_pct"`
	MinPrice           float64                `json:"min_price"`
	MinAvgDollarVolume float64                `json:"min_avg_dollar_volume"`
	MaxSpreadBPS       int                    `json:"max_spread_bps"`
	StockAllowlist     []string               `json:"stock_allowlist"`
	StockBlocklist     []string               `json:"stock_blocklist"`
	CreatedBy          string                 `json:"created_by"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
	StoppedAt          *time.Time             `json:"stopped_at,omitempty"`
}

func DefaultCopySubscription() CopySubscription {
	return CopySubscription{Status: CopySubscriptionDraft, IsPaper: true, Method: CopySizingTargetWeight, CapitalBudget: 10000, CashBufferPct: 0.10, TopN: 10, MinSourceWeight: 0.01, MaxPositionWeight: 0.15, MaxTurnoverPct: 0.25, MinPrice: 5, MinAvgDollarVolume: 1000000, MaxSpreadBPS: 100}
}

func (s *CopySubscription) Validate() error {
	if s == nil || s.LeaderID == uuid.Nil || s.SourceID == uuid.Nil {
		return fmt.Errorf("leader_id and source_id are required")
	}
	if !s.IsPaper {
		return fmt.Errorf("copy subscriptions are paper-only")
	}
	if s.CapitalBudget <= 0 || s.TopN <= 0 || s.TopN > 100 {
		return fmt.Errorf("capital_budget must be positive and top_n must be between 1 and 100")
	}
	if s.CashBufferPct < 0 || s.CashBufferPct >= 1 || s.MaxPositionWeight <= 0 || s.MaxPositionWeight > 1 || s.MaxTurnoverPct <= 0 || s.MaxTurnoverPct > 1 {
		return fmt.Errorf("invalid percentage policy")
	}
	if s.Method == "" {
		s.Method = CopySizingTargetWeight
	}
	if s.Method != CopySizingTargetWeight {
		return fmt.Errorf("MVP supports target_weight only")
	}
	if s.Status == "" {
		s.Status = CopySubscriptionDraft
	}
	return nil
}

type CopyTradeIntent struct {
	ID                     uuid.UUID       `json:"id"`
	SubscriptionID         uuid.UUID       `json:"subscription_id"`
	SourceObservationID    uuid.UUID       `json:"source_observation_id"`
	PipelineRunID          *uuid.UUID      `json:"pipeline_run_id,omitempty"`
	InstrumentKey          string          `json:"instrument_key"`
	Ticker                 string          `json:"ticker"`
	Side                   OrderSide       `json:"side"`
	TargetWeight           float64         `json:"target_weight"`
	TargetValue            float64         `json:"target_value"`
	AttributedCurrentValue float64         `json:"attributed_current_value"`
	RequestedNotional      float64         `json:"requested_notional"`
	ExecutablePrice        *float64        `json:"executable_price,omitempty"`
	CalculationVersion     int             `json:"calculation_version"`
	Calculation            json.RawMessage `json:"calculation,omitempty"`
	PolicyStatus           string          `json:"policy_status"`
	PolicyReasons          []string        `json:"policy_reasons"`
	RiskStatus             string          `json:"risk_status"`
	RiskReasons            []string        `json:"risk_reasons"`
	OrderID                *uuid.UUID      `json:"order_id,omitempty"`
	Status                 string          `json:"status"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

func NormalizeSECCIK(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "0"
	}
	return value
}
