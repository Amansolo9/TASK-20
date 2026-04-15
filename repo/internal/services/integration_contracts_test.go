package services

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"campus-portal/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllEventTypes_ContainsCompetitionAndWarehouse(t *testing.T) {
	events := AllEventTypes()

	expected := []string{
		EventCompetitionResult,
		EventCompetitionRegistration,
		EventCompetitionScoreUpdate,
		EventWarehouseExportReady,
		EventWarehouseSyncComplete,
		EventWarehouseSchemaChange,
	}

	eventSet := make(map[string]bool)
	for _, e := range events {
		eventSet[e] = true
	}

	for _, exp := range expected {
		assert.True(t, eventSet[exp], "AllEventTypes should include %s", exp)
	}

	// Also verify legacy events are present
	legacy := []string{
		EventBookingCreated, EventBookingConfirmed, EventBookingCanceled,
		EventEncounterCreated, EventUserCreated, EventOrderCreated,
		EventESignatureRequest, EventPlagiarismCheck,
	}
	for _, exp := range legacy {
		assert.True(t, eventSet[exp], "AllEventTypes should include legacy event %s", exp)
	}
}

func TestDispatchCompetitionResult_DispatchesToCorrectOrg(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := &WebhookService{
		DB:     db,
		client: &http.Client{Timeout: 5 * time.Second},
	}

	db.Create(&models.WebhookEndpoint{
		OrganizationID: 100,
		URL:            srv.URL,
		EventType:      EventCompetitionResult,
		Secret:         "secret",
		Active:         true,
	})

	svc.DispatchCompetitionResult(CompetitionResultPayload{
		CompetitionID: 1,
		UserID:        42,
		Placement:     1,
		Score:         98.5,
		Category:      "boxing",
		CompletedAt:   time.Now(),
	}, 100)

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, int32(1), received.Load(), "competition result should be dispatched")
}

func TestDispatchCompetitionResult_WrongOrg_NotDelivered(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := &WebhookService{
		DB:     db,
		client: &http.Client{Timeout: 5 * time.Second},
	}

	// Endpoint is for org 200
	db.Create(&models.WebhookEndpoint{
		OrganizationID: 200,
		URL:            srv.URL,
		EventType:      EventCompetitionResult,
		Secret:         "secret",
		Active:         true,
	})

	// Dispatch for org 100 — should NOT deliver to org 200 endpoint
	svc.DispatchCompetitionResult(CompetitionResultPayload{
		CompetitionID: 1, UserID: 42, Placement: 1, Score: 98.5,
		Category: "boxing", CompletedAt: time.Now(),
	}, 100)

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, int32(0), received.Load(), "should not deliver to wrong org")
}

func TestDispatchWarehouseExportReady(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := &WebhookService{
		DB:     db,
		client: &http.Client{Timeout: 5 * time.Second},
	}

	db.Create(&models.WebhookEndpoint{
		OrganizationID: 100,
		URL:            srv.URL,
		EventType:      EventWarehouseExportReady,
		Secret:         "secret",
		Active:         true,
	})

	svc.DispatchWarehouseExportReady(WarehouseExportReadyPayload{
		ExportID:    "exp-001",
		DataType:    "bookings",
		RecordCount: 150,
		FilePath:    "/exports/bookings_20260415.csv",
		ReadyAt:     time.Now(),
	}, 100)

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, int32(1), received.Load(), "warehouse export ready should be dispatched")
}

func TestDispatchWarehouseSyncComplete(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := &WebhookService{
		DB:     db,
		client: &http.Client{Timeout: 5 * time.Second},
	}

	db.Create(&models.WebhookEndpoint{
		OrganizationID: 100,
		URL:            srv.URL,
		EventType:      EventWarehouseSyncComplete,
		Secret:         "secret",
		Active:         true,
	})

	svc.DispatchWarehouseSyncComplete(WarehouseSyncCompletePayload{
		SyncID:       "sync-001",
		TablesSync:   []string{"bookings", "encounters"},
		RowsAffected: 300,
		Duration:     "2m30s",
		CompletedAt:  time.Now(),
	}, 100)

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, int32(1), received.Load(), "warehouse sync complete should be dispatched")
}

func TestWebhookService_InternalURLValidation(t *testing.T) {
	tests := []struct {
		url      string
		internal bool
	}{
		{"http://localhost:8080/hook", true},
		{"http://127.0.0.1:8080/hook", true},
		{"http://service.internal:8080/hook", true},
		{"http://service.local:8080/hook", true},
		{"http://10.0.1.5:8080/hook", true},
		{"http://192.168.1.1:8080/hook", true},
		{"http://google.com/hook", false},
		{"http://evil.example.com/hook", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.internal, IsInternalURL(tt.url))
		})
	}
}

func TestWebhookService_RegisterEndpointForOrg_RejectsExternalURL(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	svc := NewWebhookService(db)
	err := svc.RegisterEndpointForOrg("http://evil.example.com/hook", "booking.created", "secret", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "internal network address")
}

func TestWebhookService_DispatchForOrg_ZeroOrgIDPrevented(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := &WebhookService{
		DB:     db,
		client: &http.Client{Timeout: 5 * time.Second},
	}

	// Create an endpoint with orgID=0 (should never match)
	db.Create(&models.WebhookEndpoint{
		OrganizationID: 0,
		URL:            srv.URL,
		EventType:      "test.event",
		Secret:         "secret",
		Active:         true,
	})

	// Dispatch with orgID=0 — should be blocked
	svc.DispatchForOrg("test.event", map[string]string{"key": "value"}, 0)

	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, int32(0), received.Load(), "orgID=0 dispatch should be blocked")
}
