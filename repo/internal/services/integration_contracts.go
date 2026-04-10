package services

import "time"

// Event type constants for all named integration systems.
// Each constant maps to a webhook event_type that can be registered and dispatched.
const (
	// Booking events
	EventBookingCreated   = "booking.created"
	EventBookingConfirmed = "booking.confirmed"
	EventBookingCanceled  = "booking.canceled"

	// Encounter events
	EventEncounterCreated = "encounter.created"

	// User events
	EventUserCreated = "user.created"

	// Order events
	EventOrderCreated = "order.created"

	// E-signature integration
	EventESignatureRequest = "esignature.request"

	// Plagiarism check integration
	EventPlagiarismCheck = "plagiarism.check"

	// Competition platform integration
	EventCompetitionResult       = "competition.result"
	EventCompetitionRegistration = "competition.registration"
	EventCompetitionScoreUpdate  = "competition.score_update"

	// Data warehouse integration
	EventWarehouseExportReady  = "warehouse.export_ready"
	EventWarehouseSyncComplete = "warehouse.sync_complete"
	EventWarehouseSchemaChange = "warehouse.schema_change"
)

// AllEventTypes returns the full list of recognized event types for validation and UI display.
func AllEventTypes() []string {
	return []string{
		EventBookingCreated,
		EventBookingConfirmed,
		EventBookingCanceled,
		EventEncounterCreated,
		EventUserCreated,
		EventOrderCreated,
		EventESignatureRequest,
		EventPlagiarismCheck,
		EventCompetitionResult,
		EventCompetitionRegistration,
		EventCompetitionScoreUpdate,
		EventWarehouseExportReady,
		EventWarehouseSyncComplete,
		EventWarehouseSchemaChange,
	}
}

// --- Competition platform payload schemas ---

// CompetitionResultPayload is the payload dispatched when a competition result is finalized.
type CompetitionResultPayload struct {
	EventType     string    `json:"event_type"`
	CompetitionID uint      `json:"competition_id"`
	UserID        uint      `json:"user_id"`
	Placement     int       `json:"placement"`
	Score         float64   `json:"score"`
	Category      string    `json:"category"`
	CompletedAt   time.Time `json:"completed_at"`
}

// CompetitionRegistrationPayload is dispatched when a user registers for a competition.
type CompetitionRegistrationPayload struct {
	EventType     string    `json:"event_type"`
	CompetitionID uint      `json:"competition_id"`
	UserID        uint      `json:"user_id"`
	Category      string    `json:"category"`
	RegisteredAt  time.Time `json:"registered_at"`
}

// CompetitionScoreUpdatePayload is dispatched when a competition score is updated mid-event.
type CompetitionScoreUpdatePayload struct {
	EventType     string    `json:"event_type"`
	CompetitionID uint      `json:"competition_id"`
	UserID        uint      `json:"user_id"`
	OldScore      float64   `json:"old_score"`
	NewScore      float64   `json:"new_score"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// --- Data warehouse payload schemas ---

// WarehouseExportReadyPayload is dispatched when a data export is ready for warehouse ingestion.
type WarehouseExportReadyPayload struct {
	EventType  string    `json:"event_type"`
	ExportID   string    `json:"export_id"`
	DataType   string    `json:"data_type"` // e.g., "bookings", "encounters", "orders"
	RecordCount int      `json:"record_count"`
	FilePath   string    `json:"file_path"`
	ReadyAt    time.Time `json:"ready_at"`
}

// WarehouseSyncCompletePayload is dispatched when a warehouse sync cycle finishes.
type WarehouseSyncCompletePayload struct {
	EventType    string    `json:"event_type"`
	SyncID       string    `json:"sync_id"`
	TablesSync   []string  `json:"tables_synced"`
	RowsAffected int64     `json:"rows_affected"`
	Duration     string    `json:"duration"`
	CompletedAt  time.Time `json:"completed_at"`
}

// WarehouseSchemaChangePayload is dispatched when a schema migration affects warehouse tables.
type WarehouseSchemaChangePayload struct {
	EventType   string    `json:"event_type"`
	TableName   string    `json:"table_name"`
	ChangeType  string    `json:"change_type"` // "add_column", "drop_column", "alter_type"
	Description string    `json:"description"`
	AppliedAt   time.Time `json:"applied_at"`
}

// --- Typed dispatcher helpers ---

// DispatchCompetitionResult dispatches a competition result event for the given organization.
func (s *WebhookService) DispatchCompetitionResult(payload CompetitionResultPayload, orgID uint) {
	payload.EventType = EventCompetitionResult
	s.DispatchForOrg(EventCompetitionResult, payload, orgID)
}

// DispatchCompetitionRegistration dispatches a competition registration event.
func (s *WebhookService) DispatchCompetitionRegistration(payload CompetitionRegistrationPayload, orgID uint) {
	payload.EventType = EventCompetitionRegistration
	s.DispatchForOrg(EventCompetitionRegistration, payload, orgID)
}

// DispatchCompetitionScoreUpdate dispatches a score update event.
func (s *WebhookService) DispatchCompetitionScoreUpdate(payload CompetitionScoreUpdatePayload, orgID uint) {
	payload.EventType = EventCompetitionScoreUpdate
	s.DispatchForOrg(EventCompetitionScoreUpdate, payload, orgID)
}

// DispatchWarehouseExportReady dispatches a warehouse export ready event.
func (s *WebhookService) DispatchWarehouseExportReady(payload WarehouseExportReadyPayload, orgID uint) {
	payload.EventType = EventWarehouseExportReady
	s.DispatchForOrg(EventWarehouseExportReady, payload, orgID)
}

// DispatchWarehouseSyncComplete dispatches a warehouse sync complete event.
func (s *WebhookService) DispatchWarehouseSyncComplete(payload WarehouseSyncCompletePayload, orgID uint) {
	payload.EventType = EventWarehouseSyncComplete
	s.DispatchForOrg(EventWarehouseSyncComplete, payload, orgID)
}

// DispatchWarehouseSchemaChange dispatches a warehouse schema change event.
func (s *WebhookService) DispatchWarehouseSchemaChange(payload WarehouseSchemaChangePayload, orgID uint) {
	payload.EventType = EventWarehouseSchemaChange
	s.DispatchForOrg(EventWarehouseSchemaChange, payload, orgID)
}
