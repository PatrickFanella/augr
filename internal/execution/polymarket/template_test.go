package polymarket

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOrderTemplate_SignMatchesAdHoc(t *testing.T) {
	t.Parallel()

	secret := mustDecodeSecretBytes()
	body := []byte(`{"marketSlug":"btc-100k","type":"ORDER_TYPE_LIMIT","price":{"value":"0.5","currency":"USD"},"quantity":1,"tif":"TIME_IN_FORCE_GOOD_TILL_CANCEL","intent":"ORDER_INTENT_BUY_LONG"}`)
	tmpl, err := NewOrderTemplate(secret, http.MethodPost, "https://api.polymarket.us/v1/orders", body)
	if err != nil {
		t.Fatalf("NewOrderTemplate() error = %v", err)
	}
	ts := int64(1712000000)

	got := tmpl.SignAt(ts)

	want, err := polyL2Signature(base64URLSecret(secret), "1712000000", strings.ToUpper(http.MethodPost), "/v1/orders", body)
	if err != nil {
		t.Fatalf("polyL2Signature() error = %v", err)
	}

	if got != want {
		t.Fatalf("SignAt() = %q, want %q", got, want)
	}
}

func base64URLSecret(secret []byte) string {
	return base64.URLEncoding.EncodeToString(secret)
}

func BenchmarkOrderTemplate_SignAndBuild(b *testing.B) {
	secret := mustDecodeSecretBytes()
	body := []byte(`{"marketSlug":"btc-100k","type":"ORDER_TYPE_LIMIT","price":{"value":"0.5","currency":"USD"},"quantity":1,"tif":"TIME_IN_FORCE_GOOD_TILL_CANCEL","intent":"ORDER_INTENT_BUY_LONG"}`)
	tmpl, err := NewOrderTemplate(secret, http.MethodPost, "https://api.polymarket.us/v1/orders", body)
	if err != nil {
		b.Fatalf("NewOrderTemplate() error = %v", err)
	}
	rec := httptest.NewRecorder()
	ts := time.Now().UnixMilli()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sig := tmpl.SignAt(ts)
		req := httptest.NewRequest(http.MethodPost, tmpl.URL(), strings.NewReader(string(body)))
		req.Header.Set("X-PM-Signature", sig)
		req.Header.Set("X-PM-Timestamp", "1712000000123")
		rec.Header().Set("X-PM-Signature", sig)
		_, _ = rec.WriteString(req.Header.Get("X-PM-Signature"))
	}
	// Target: <50µs/op steady-state; check ns/op and allocs/op in benchmark output.
	_ = rec
}

func mustDecodeSecretBytes() []byte {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	return seed
}
