package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"campus-portal/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDispatchForOrg_OrgIsolation verifies that DispatchForOrg only delivers
// webhook events to endpoints belonging to the specified organization.
func TestDispatchForOrg_OrgIsolation(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	var org1Received, org2Received atomic.Int32

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org1Received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org2Received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv2.Close()

	svc := &WebhookService{
		DB:     db,
		client: &http.Client{Timeout: 5 * time.Second},
	}

	// Register endpoints for two different orgs.
	// httptest servers listen on 127.0.0.1 which passes IsInternalURL.
	db.Create(&models.WebhookEndpoint{
		OrganizationID: 100,
		URL:            srv1.URL,
		EventType:      "test.event",
		Secret:         "secret1",
		Active:         true,
	})
	db.Create(&models.WebhookEndpoint{
		OrganizationID: 200,
		URL:            srv2.URL,
		EventType:      "test.event",
		Secret:         "secret2",
		Active:         true,
	})

	// Dispatch scoped to org 100 only
	svc.DispatchForOrg("test.event", map[string]string{"key": "value"}, 100)

	// Give goroutines time to deliver
	time.Sleep(500 * time.Millisecond)

	assert.Equal(t, int32(1), org1Received.Load(), "org 100 endpoint should receive exactly 1 delivery")
	assert.Equal(t, int32(0), org2Received.Load(), "org 200 endpoint should NOT receive any delivery")

	// Now dispatch scoped to org 200
	svc.DispatchForOrg("test.event", map[string]string{"key": "value2"}, 200)

	time.Sleep(500 * time.Millisecond)

	assert.Equal(t, int32(1), org1Received.Load(), "org 100 endpoint count should still be 1")
	assert.Equal(t, int32(1), org2Received.Load(), "org 200 endpoint should now have received 1 delivery")
}

// TestDeliver_RetryBodyIntegrity verifies that webhook retries create fresh
// HTTP requests with the correct payload body and HMAC signature on each attempt.
// The deliver function retries only on connection errors (err != nil from client.Do),
// so we use http.Hijacker to close connections on the first N attempts.
func TestDeliver_RetryBodyIntegrity(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	expectedPayload := map[string]string{"booking_id": "42", "status": "confirmed"}
	payloadBytes, _ := json.Marshal(expectedPayload)

	secret := "test-webhook-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payloadBytes)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	type attemptRecord struct {
		body      string
		signature string
		event     string
	}

	var mu sync.Mutex
	var attempts []attemptRecord
	failCount := 2 // Cause connection errors on first 2 attempts, succeed on 3rd

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always read the body and headers for verification
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()

		mu.Lock()
		attempts = append(attempts, attemptRecord{
			body:      string(body),
			signature: r.Header.Get("X-Webhook-Signature"),
			event:     r.Header.Get("X-Webhook-Event"),
		})
		attemptNum := len(attempts)
		mu.Unlock()

		if attemptNum <= failCount {
			// Hijack the connection and close it to cause a client-side error,
			// which triggers the retry logic in deliver().
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server does not support hijacking")
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := &WebhookService{
		DB:     db,
		client: &http.Client{Timeout: 5 * time.Second},
	}

	endpoint := models.WebhookEndpoint{
		OrganizationID: 100,
		URL:            srv.URL,
		EventType:      "booking.confirmed",
		Secret:         secret,
		Active:         true,
	}
	require.NoError(t, db.Create(&endpoint).Error)

	// deliver is synchronous so we can assert immediately after
	svc.deliver(endpoint, payloadBytes)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, attempts, 3, "should have made exactly 3 attempts (2 conn errors + 1 success)")

	for i, a := range attempts {
		assert.Equal(t, string(payloadBytes), a.body,
			"attempt %d: payload body must match original", i+1)
		assert.Equal(t, expectedSig, a.signature,
			"attempt %d: HMAC signature must match expected", i+1)
		assert.Equal(t, "booking.confirmed", a.event,
			"attempt %d: event type header must be set", i+1)
	}

	// Verify the delivery was logged in DB (only the successful final attempt)
	var deliveries []models.WebhookDelivery
	db.Where("endpoint_id = ?", endpoint.ID).Find(&deliveries)
	require.Len(t, deliveries, 1, "should log exactly one delivery record for the successful attempt")
	assert.Equal(t, http.StatusOK, deliveries[0].Status, "logged status should be 200")
	assert.Equal(t, 3, deliveries[0].Attempts, "logged attempts should be 3")
}

// TestDeliver_AllRetriesFail verifies that when all retry attempts fail
// with connection errors, the final failure is logged correctly.
func TestDeliver_AllRetriesFail(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	payload := []byte(`{"test":"data"}`)
	secret := "fail-secret"

	// Server always closes the connection to simulate persistent network failures
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server does not support hijacking")
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	svc := &WebhookService{
		DB:     db,
		client: &http.Client{Timeout: 5 * time.Second},
	}

	endpoint := models.WebhookEndpoint{
		OrganizationID: 100,
		URL:            srv.URL,
		EventType:      "test.fail",
		Secret:         secret,
		Active:         true,
	}
	require.NoError(t, db.Create(&endpoint).Error)

	svc.deliver(endpoint, payload)

	// Only the final (3rd) failed attempt should be logged
	var deliveries []models.WebhookDelivery
	db.Where("endpoint_id = ?", endpoint.ID).Find(&deliveries)
	require.Len(t, deliveries, 1, "should log one delivery record for the final failed attempt")
	assert.Equal(t, 0, deliveries[0].Status, "status should be 0 for connection errors")
	assert.Equal(t, 3, deliveries[0].Attempts, "should record 3 attempts")
	assert.NotEmpty(t, deliveries[0].Response, "should contain error message")
}
