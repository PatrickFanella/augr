// Command augr-evidence records and inspects local Milestone 7 evidence.
// It has no provider, scheduler, deployment, account, or execution authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/evidenceprogram"
	postgresrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
)

type candidateStartInput struct {
	Key               string    `json:"key"`
	StrategyVersionID uuid.UUID `json:"strategy_version_id"`
}

type startInput struct {
	Key               string                `json:"key"`
	StartedAt         time.Time             `json:"started_at"`
	BenchmarkReportID uuid.UUID             `json:"benchmark_report_id"`
	Candidates        []candidateStartInput `json:"candidates"`
}

type recordInput struct {
	CampaignID uuid.UUID                   `json:"campaign_id"`
	Sequence   int                         `json:"sequence"`
	ObservedAt time.Time                   `json:"observed_at"`
	Candidates []candidateDayInput         `json:"candidates"`
	Source     evidenceprogram.EvidenceRef `json:"source"`
}

type paperInput struct {
	ShadowAssessmentID uuid.UUID                        `json:"shadow_assessment_id"`
	StartedAt          time.Time                        `json:"started_at"`
	EndedAt            time.Time                        `json:"ended_at"`
	Candidates         []evidenceprogram.CandidatePaper `json:"candidates"`
	Parents            []evidenceprogram.EvidenceRef    `json:"parents"`
}

type portfolioInput struct {
	PaperAssessmentID      uuid.UUID                     `json:"paper_assessment_id"`
	StartedAt              time.Time                     `json:"started_at"`
	EndedAt                time.Time                     `json:"ended_at"`
	CombinedRiskAdjusted   string                        `json:"combined_risk_adjusted"`
	BestSingleRiskAdjusted string                        `json:"best_single_risk_adjusted"`
	SameInterval           bool                          `json:"same_interval"`
	SameCostBasis          bool                          `json:"same_cost_basis"`
	Parents                []evidenceprogram.EvidenceRef `json:"parents"`
}

type readinessInput struct {
	PortfolioAssessmentID uuid.UUID                     `json:"portfolio_assessment_id"`
	Capabilities          []evidenceprogram.Capability  `json:"capabilities"`
	Parents               []evidenceprogram.EvidenceRef `json:"parents"`
}

type candidateDayInput struct {
	Key                string `json:"key"`
	CriticalDefects    int    `json:"critical_defects"`
	ExecutableSamples  int    `json:"executable_samples"`
	SimulatedFills     int    `json:"simulated_fills"`
	SlippageKnown      bool   `json:"slippage_known"`
	SlippageDivergence string `json:"slippage_divergence"`
}

type artifactOutput struct {
	Kind      string          `json:"kind"`
	ID        uuid.UUID       `json:"id"`
	SHA256    string          `json:"sha256"`
	Outcome   string          `json:"outcome,omitempty"`
	Blockers  []string        `json:"blockers,omitempty"`
	Canonical json.RawMessage `json:"canonical"`
}

type shadowBackend interface {
	BenchmarkReference(context.Context, uuid.UUID) (evidenceprogram.EvidenceRef, error)
	StrategyCandidate(context.Context, string, uuid.UUID) (evidenceprogram.ShadowCandidate, error)
	RegisterCampaign(context.Context, *evidenceprogram.ShadowCampaign) error
	GetCampaign(context.Context, uuid.UUID) (*evidenceprogram.ShadowCampaign, error)
	RegisterDay(context.Context, *evidenceprogram.ShadowDay) error
	ListDays(context.Context, *evidenceprogram.ShadowCampaign) ([]*evidenceprogram.ShadowDay, error)
	RecordAssessment(context.Context, *evidenceprogram.Assessment) error
	GetAssessment(context.Context, uuid.UUID) (*evidenceprogram.Assessment, error)
	Close()
}

type postgresShadowBackend struct {
	db          *postgresrepo.DB
	benchmarks  *postgresrepo.BenchmarkRepo
	strategies  *postgresrepo.StrategyCatalogRepo
	campaigns   *postgresrepo.ShadowCampaignRepo
	assessments *postgresrepo.MilestoneEvidenceRepo
}

func openPostgresBackend(ctx context.Context, databaseURL string) (shadowBackend, error) {
	db, err := postgresrepo.NewDB(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	version, err := postgresrepo.CurrentSchemaVersion(ctx, db.Pool)
	if err != nil {
		db.Close()
		return nil, err
	}
	if version != postgresrepo.RequiredSchemaVersion {
		db.Close()
		return nil, fmt.Errorf("augr-evidence: schema version %d does not match required version %d", version, postgresrepo.RequiredSchemaVersion)
	}
	return &postgresShadowBackend{
		db: db, benchmarks: postgresrepo.NewBenchmarkRepo(db.Pool),
		strategies: postgresrepo.NewStrategyCatalogRepo(db.Pool), campaigns: postgresrepo.NewShadowCampaignRepo(db.Pool),
		assessments: postgresrepo.NewMilestoneEvidenceRepo(db.Pool),
	}, nil
}

func (b *postgresShadowBackend) BenchmarkReference(ctx context.Context, id uuid.UUID) (evidenceprogram.EvidenceRef, error) {
	report, err := b.benchmarks.GetReport(ctx, id)
	if err != nil {
		return evidenceprogram.EvidenceRef{}, fmt.Errorf("load benchmark report %s: %w", id, err)
	}
	return evidenceprogram.EvidenceRef{Kind: "benchmark_opportunity_cost_report", ID: report.ID(), SHA256: report.Digest()}, nil
}

func (b *postgresShadowBackend) StrategyCandidate(ctx context.Context, key string, id uuid.UUID) (evidenceprogram.ShadowCandidate, error) {
	version, err := b.strategies.GetStrategyVersion(ctx, id)
	if err != nil {
		return evidenceprogram.ShadowCandidate{}, fmt.Errorf("load strategy version %s: %w", id, err)
	}
	return evidenceprogram.ShadowCandidate{Key: key, VersionID: version.ID(), SHA256: version.Digest()}, nil
}

func (b *postgresShadowBackend) RegisterCampaign(ctx context.Context, value *evidenceprogram.ShadowCampaign) error {
	return b.campaigns.RegisterCampaign(ctx, value)
}

func (b *postgresShadowBackend) GetCampaign(ctx context.Context, id uuid.UUID) (*evidenceprogram.ShadowCampaign, error) {
	return b.campaigns.GetCampaign(ctx, id)
}

func (b *postgresShadowBackend) RegisterDay(ctx context.Context, value *evidenceprogram.ShadowDay) error {
	return b.campaigns.RegisterDay(ctx, value)
}

func (b *postgresShadowBackend) ListDays(ctx context.Context, campaign *evidenceprogram.ShadowCampaign) ([]*evidenceprogram.ShadowDay, error) {
	return b.campaigns.ListDays(ctx, campaign)
}

func (b *postgresShadowBackend) RecordAssessment(ctx context.Context, value *evidenceprogram.Assessment) error {
	return b.assessments.RecordAssessment(ctx, value)
}

func (b *postgresShadowBackend) GetAssessment(ctx context.Context, id uuid.UUID) (*evidenceprogram.Assessment, error) {
	return b.assessments.GetAssessment(ctx, id)
}

func (b *postgresShadowBackend) Close() { b.db.Close() }

type backendOpener func(context.Context, string) (shadowBackend, error)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, openPostgresBackend); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, open backendOpener) error {
	if len(args) == 0 {
		return errors.New("usage: augr-evidence <shadow-start|shadow-record-day|shadow-assess|paper-assess|portfolio-assess|readiness-assess|assessment-get> [flags]")
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	databaseURL := flags.String("db-url", firstSet(os.Getenv("DB_URL"), os.Getenv("DATABASE_URL")), "schema-103 PostgreSQL connection URL")
	inputPath := flags.String("input", "-", "JSON input path, or - for stdin")
	campaignID := flags.String("campaign-id", "", "shadow campaign UUID")
	assessmentID := flags.String("assessment-id", "", "milestone assessment UUID")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("augr-evidence: parse %s flags: %w", command, err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("augr-evidence: %s accepts flags only", command)
	}
	if *databaseURL == "" {
		return errors.New("augr-evidence: --db-url, DB_URL, or DATABASE_URL is required")
	}
	backend, err := open(ctx, *databaseURL)
	if err != nil {
		return err
	}
	defer backend.Close()

	var output artifactOutput
	switch command {
	case "shadow-start":
		if *campaignID != "" || *assessmentID != "" {
			return errors.New("augr-evidence: shadow-start does not accept --campaign-id or --assessment-id")
		}
		var input startInput
		if err = decodeInput(*inputPath, stdin, &input); err != nil {
			return err
		}
		output, err = startCampaign(ctx, backend, input)
	case "shadow-record-day":
		if *assessmentID != "" {
			return errors.New("augr-evidence: shadow-record-day does not accept --assessment-id")
		}
		if *campaignID != "" {
			return errors.New("augr-evidence: shadow-record-day takes campaign_id from its JSON input")
		}
		var input recordInput
		if err = decodeInput(*inputPath, stdin, &input); err != nil {
			return err
		}
		output, err = recordDay(ctx, backend, input)
	case "shadow-assess":
		if *assessmentID != "" {
			return errors.New("augr-evidence: shadow-assess does not accept --assessment-id")
		}
		if *inputPath != "-" {
			return errors.New("augr-evidence: shadow-assess does not accept --input")
		}
		id, parseErr := uuid.Parse(*campaignID)
		if parseErr != nil {
			return fmt.Errorf("augr-evidence: valid --campaign-id is required: %w", parseErr)
		}
		output, err = assessCampaign(ctx, backend, id)
	case "paper-assess":
		var input paperInput
		if err = requireJSONInput(*campaignID, *assessmentID, *inputPath, stdin, &input); err != nil {
			return err
		}
		output, err = assessPaper(ctx, backend, input)
	case "portfolio-assess":
		var input portfolioInput
		if err = requireJSONInput(*campaignID, *assessmentID, *inputPath, stdin, &input); err != nil {
			return err
		}
		output, err = assessPortfolio(ctx, backend, input)
	case "readiness-assess":
		var input readinessInput
		if err = requireJSONInput(*campaignID, *assessmentID, *inputPath, stdin, &input); err != nil {
			return err
		}
		output, err = assessReadiness(ctx, backend, input)
	case "assessment-get":
		if *inputPath != "-" {
			return errors.New("augr-evidence: assessment-get does not accept --input")
		}
		if *campaignID != "" {
			return errors.New("augr-evidence: assessment-get does not accept --campaign-id")
		}
		id, parseErr := uuid.Parse(*assessmentID)
		if parseErr != nil {
			return fmt.Errorf("augr-evidence: valid --assessment-id is required: %w", parseErr)
		}
		assessment, loadErr := backend.GetAssessment(ctx, id)
		if loadErr != nil {
			return fmt.Errorf("augr-evidence: load assessment: %w", loadErr)
		}
		output = assessmentOutput(assessment)
	default:
		return fmt.Errorf("augr-evidence: unknown command %q", command)
	}
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(output)
}

func startCampaign(ctx context.Context, backend shadowBackend, input startInput) (artifactOutput, error) {
	benchmark, err := backend.BenchmarkReference(ctx, input.BenchmarkReportID)
	if err != nil {
		return artifactOutput{}, err
	}
	candidates := make([]evidenceprogram.ShadowCandidate, len(input.Candidates))
	for index, candidate := range input.Candidates {
		candidates[index], err = backend.StrategyCandidate(ctx, candidate.Key, candidate.StrategyVersionID)
		if err != nil {
			return artifactOutput{}, err
		}
	}
	campaign, err := evidenceprogram.NewShadowCampaign(evidenceprogram.ShadowCampaignInput{
		Key: input.Key, StartedAt: input.StartedAt, Benchmark: benchmark, Candidates: candidates,
	})
	if err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: construct shadow campaign: %w", err)
	}
	if err = backend.RegisterCampaign(ctx, campaign); err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: register shadow campaign: %w", err)
	}
	return artifactOutput{Kind: "shadow_campaign", ID: campaign.ID(), SHA256: campaign.Digest(), Canonical: campaign.CanonicalBytes()}, nil
}

func recordDay(ctx context.Context, backend shadowBackend, input recordInput) (artifactOutput, error) {
	campaign, err := backend.GetCampaign(ctx, input.CampaignID)
	if err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: load shadow campaign: %w", err)
	}
	candidates := make([]evidenceprogram.ShadowCandidateDayInput, len(input.Candidates))
	for index, candidate := range input.Candidates {
		candidates[index] = evidenceprogram.ShadowCandidateDayInput{
			Key: candidate.Key, CriticalDefects: candidate.CriticalDefects,
			ExecutableSamples: candidate.ExecutableSamples, SimulatedFills: candidate.SimulatedFills,
			SlippageKnown: candidate.SlippageKnown, SlippageDivergence: candidate.SlippageDivergence,
		}
	}
	day, err := evidenceprogram.NewShadowDay(evidenceprogram.ShadowDayInput{
		Campaign: campaign, Sequence: input.Sequence, ObservedAt: input.ObservedAt,
		Candidates: candidates, Source: input.Source,
	})
	if err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: construct shadow day: %w", err)
	}
	if err = backend.RegisterDay(ctx, day); err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: register shadow day: %w", err)
	}
	return artifactOutput{Kind: "shadow_campaign_day", ID: day.ID(), SHA256: day.Digest(), Canonical: day.CanonicalBytes()}, nil
}

func assessCampaign(ctx context.Context, backend shadowBackend, id uuid.UUID) (artifactOutput, error) {
	campaign, err := backend.GetCampaign(ctx, id)
	if err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: load shadow campaign: %w", err)
	}
	days, err := backend.ListDays(ctx, campaign)
	if err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: list shadow days: %w", err)
	}
	assessment, err := evidenceprogram.BuildShadowAssessment(campaign, days)
	if err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: build shadow assessment: %w", err)
	}
	return recordAssessment(ctx, backend, assessment)
}

func assessPaper(ctx context.Context, backend shadowBackend, input paperInput) (artifactOutput, error) {
	shadow, err := backend.GetAssessment(ctx, input.ShadowAssessmentID)
	if err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: load shadow assessment: %w", err)
	}
	assessment, err := evidenceprogram.AssessPaper(evidenceprogram.PaperInput{
		Shadow: shadow, StartedAt: input.StartedAt, EndedAt: input.EndedAt,
		Candidates: input.Candidates, Parents: input.Parents,
	})
	if err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: build paper assessment: %w", err)
	}
	return recordAssessment(ctx, backend, assessment)
}

func assessPortfolio(ctx context.Context, backend shadowBackend, input portfolioInput) (artifactOutput, error) {
	paper, err := backend.GetAssessment(ctx, input.PaperAssessmentID)
	if err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: load paper assessment: %w", err)
	}
	assessment, err := evidenceprogram.AssessPortfolio(evidenceprogram.PortfolioInput{
		Paper: paper, StartedAt: input.StartedAt, EndedAt: input.EndedAt,
		CombinedRiskAdjusted: input.CombinedRiskAdjusted, BestSingleRiskAdjusted: input.BestSingleRiskAdjusted,
		SameInterval: input.SameInterval, SameCostBasis: input.SameCostBasis, Parents: input.Parents,
	})
	if err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: build portfolio assessment: %w", err)
	}
	return recordAssessment(ctx, backend, assessment)
}

func assessReadiness(ctx context.Context, backend shadowBackend, input readinessInput) (artifactOutput, error) {
	portfolio, err := backend.GetAssessment(ctx, input.PortfolioAssessmentID)
	if err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: load portfolio assessment: %w", err)
	}
	assessment, err := evidenceprogram.AssessReadiness(evidenceprogram.ReadinessInput{
		Portfolio: portfolio, Capabilities: input.Capabilities, Parents: input.Parents,
	})
	if err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: build readiness assessment: %w", err)
	}
	return recordAssessment(ctx, backend, assessment)
}

func recordAssessment(ctx context.Context, backend shadowBackend, assessment *evidenceprogram.Assessment) (artifactOutput, error) {
	if err := backend.RecordAssessment(ctx, assessment); err != nil {
		return artifactOutput{}, fmt.Errorf("augr-evidence: record assessment: %w", err)
	}
	return assessmentOutput(assessment), nil
}

func assessmentOutput(assessment *evidenceprogram.Assessment) artifactOutput {
	return artifactOutput{
		Kind: assessment.Campaign(), ID: assessment.ID(), SHA256: assessment.Digest(),
		Outcome: string(assessment.Outcome()), Blockers: assessment.Blockers(), Canonical: assessment.CanonicalBytes(),
	}
}

func requireJSONInput(campaignID, assessmentID, path string, stdin io.Reader, target any) error {
	if campaignID != "" || assessmentID != "" {
		return errors.New("augr-evidence: assessment command takes its parent identity from JSON input")
	}
	return decodeInput(path, stdin, target)
}

func decodeInput(path string, stdin io.Reader, target any) error {
	reader := stdin
	var file *os.File
	var err error
	if path != "-" {
		file, err = os.Open(path)
		if err != nil {
			return fmt.Errorf("augr-evidence: open input: %w", err)
		}
		defer func() { _ = file.Close() }()
		reader = file
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return fmt.Errorf("augr-evidence: decode input: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("augr-evidence: input must contain exactly one JSON value")
	}
	return nil
}

func firstSet(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
