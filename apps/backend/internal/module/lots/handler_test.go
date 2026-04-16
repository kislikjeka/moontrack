package lots_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/module/lots"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
)

// --- mock service ---

type mockSvc struct {
	called    bool
	gotLotID  uuid.UUID
	gotPrice  string
	gotReason string
	retErr    error
}

func (m *mockSvc) SetManualPrice(_ context.Context, _ uuid.UUID, lotID uuid.UUID, priceUSD string, reason string) error {
	m.called = true
	m.gotLotID = lotID
	m.gotPrice = priceUSD
	m.gotReason = reason
	return m.retErr
}

// --- helpers ---

func newRequest(t *testing.T, lotID string, body interface{}) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}

	r := httptest.NewRequest(http.MethodPut, "/lots/"+lotID+"/manual-price", &buf)

	// Inject a fake user ID into context (simulating the JWT middleware)
	userID := uuid.New()
	ctx := context.WithValue(r.Context(), middleware.UserIDKey, userID)

	// Inject chi URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", lotID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	return r.WithContext(ctx)
}

// --- tests ---

func TestSetManualPrice_Success(t *testing.T) {
	lotID := uuid.New()
	svc := &mockSvc{}
	h := lots.NewHandler(svc)

	body := map[string]string{
		"price_usd": "123456",
		"reason":    "dex backfill manual",
	}

	r := newRequest(t, lotID.String(), body)
	w := httptest.NewRecorder()
	h.SetManualPrice(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !svc.called {
		t.Fatal("expected service to be called")
	}
	if svc.gotLotID != lotID {
		t.Errorf("expected lot ID %s, got %s", lotID, svc.gotLotID)
	}
	if svc.gotPrice != "123456" {
		t.Errorf("expected price 123456, got %s", svc.gotPrice)
	}
	if svc.gotReason != "dex backfill manual" {
		t.Errorf("expected reason 'dex backfill manual', got %s", svc.gotReason)
	}
}

func TestSetManualPrice_BadJSON(t *testing.T) {
	svc := &mockSvc{}
	h := lots.NewHandler(svc)

	lotID := uuid.New()
	r := httptest.NewRequest(http.MethodPut, "/lots/"+lotID.String()+"/manual-price",
		bytes.NewBufferString("{not valid json"))

	userID := uuid.New()
	ctx := context.WithValue(r.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", lotID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	h.SetManualPrice(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if svc.called {
		t.Fatal("service should NOT have been called")
	}
}

func TestSetManualPrice_MissingPriceField(t *testing.T) {
	svc := &mockSvc{}
	h := lots.NewHandler(svc)

	body := map[string]string{"reason": "test"}
	r := newRequest(t, uuid.New().String(), body)
	w := httptest.NewRecorder()
	h.SetManualPrice(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetManualPrice_NegativePrice(t *testing.T) {
	svc := &mockSvc{retErr: lots.ErrInvalidPrice}
	h := lots.NewHandler(svc)

	body := map[string]string{
		"price_usd": "-50",
		"reason":    "test negative",
	}
	r := newRequest(t, uuid.New().String(), body)
	w := httptest.NewRecorder()
	h.SetManualPrice(w, r)

	// The handler sends the price as-is to the service; service returns ErrInvalidPrice.
	// The handler maps ErrInvalidPrice → 400.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSetManualPrice_LotNotFound(t *testing.T) {
	svc := &mockSvc{retErr: lots.ErrLotNotFound}
	h := lots.NewHandler(svc)

	body := map[string]string{"price_usd": "1.00", "reason": "test"}
	r := newRequest(t, uuid.New().String(), body)
	w := httptest.NewRecorder()
	h.SetManualPrice(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestSetManualPrice_Forbidden(t *testing.T) {
	svc := &mockSvc{retErr: lots.ErrLotNotOwned}
	h := lots.NewHandler(svc)

	body := map[string]string{"price_usd": "1.00", "reason": "test"}
	r := newRequest(t, uuid.New().String(), body)
	w := httptest.NewRecorder()
	h.SetManualPrice(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestSetManualPrice_InvalidLotID(t *testing.T) {
	svc := &mockSvc{}
	h := lots.NewHandler(svc)

	r := httptest.NewRequest(http.MethodPut, "/lots/not-a-uuid/manual-price",
		bytes.NewBufferString(`{"price_usd":"1.00","reason":"test"}`))
	userID := uuid.New()
	ctx := context.WithValue(r.Context(), middleware.UserIDKey, userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-a-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	h.SetManualPrice(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
