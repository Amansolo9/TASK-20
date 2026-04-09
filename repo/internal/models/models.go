package models

import (
	"crypto/sha256"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ===================== ENUMS =====================

type Role string

const (
	RoleStudent   Role = "student"
	RoleFaculty   Role = "faculty"
	RoleClinician Role = "clinician"
	RoleStaff     Role = "staff"
	RoleAdmin     Role = "admin"
)

type BookingStatus string

const (
	BookingInitiated BookingStatus = "initiated"
	BookingConfirmed BookingStatus = "confirmed"
	BookingCanceled  BookingStatus = "canceled"
	BookingRefunded  BookingStatus = "refunded"
)

type Department string

const (
	DeptLab      Department = "lab"
	DeptPharmacy Department = "pharmacy"
	DeptGeneral  Department = "general"
	DeptNursing  Department = "nursing"
)

// ===================== AUTH & USERS =====================

type Organization struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"uniqueIndex;not null" json:"name"`
}

type DepartmentRecord struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	Name           string `gorm:"not null" json:"name"`
	OrganizationID uint   `gorm:"not null" json:"organization_id"`
}

func (DepartmentRecord) TableName() string { return "departments" }

type User struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	Username       string     `gorm:"uniqueIndex;size:100;not null" json:"username"`
	PasswordHash   string     `gorm:"not null" json:"-"`
	FullName       string     `gorm:"not null" json:"full_name"`
	Email          string     `gorm:"size:255" json:"email"`
	Role           Role       `gorm:"type:varchar(20);not null;default:'student'" json:"role"`
	OrganizationID uint       `gorm:"not null" json:"organization_id"`
	DepartmentID   *uint      `json:"department_id"`
	Active         bool       `gorm:"default:true" json:"active"`
	SSN            string     `gorm:"size:255" json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

// SSN is stored encrypted at rest. Use services.EncryptField/DecryptField
// to handle encryption before write and decryption after read.

type TempAccess struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	UserID      uint      `gorm:"not null;index" json:"user_id"`
	GrantedRole Role      `gorm:"type:varchar(20);not null" json:"granted_role"`
	OriginalRole Role     `gorm:"type:varchar(20);not null" json:"original_role"`
	GrantedBy   uint      `gorm:"not null" json:"granted_by"`
	ExpiresAt   time.Time `gorm:"not null;index" json:"expires_at"`
	Reverted    bool      `gorm:"default:false" json:"reverted"`
	CreatedAt   time.Time `json:"created_at"`
}

type Session struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	UserID    uint      `gorm:"not null;index" json:"user_id"`
	ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ===================== HEALTH RECORDS =====================

type HealthRecord struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	UserID      uint      `gorm:"not null;index" json:"user_id"`
	Allergies   string    `gorm:"type:text" json:"allergies"`
	Conditions  string    `gorm:"type:text" json:"conditions"`
	Medications string    `gorm:"type:text" json:"medications"`
	BloodType   string    `gorm:"size:10" json:"blood_type"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Vital struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	UserID        uint      `gorm:"not null;index" json:"user_id"`
	WeightLb      float64   `json:"weight_lb"`
	BPSystolic    int       `json:"bp_systolic"`
	BPDiastolic   int       `json:"bp_diastolic"`
	TemperatureF  float64   `json:"temperature_f"`
	HeartRate     int       `json:"heart_rate"`
	RecordedBy    uint      `gorm:"not null" json:"recorded_by"`
	RecordedAt    time.Time `gorm:"not null;index" json:"recorded_at"`
	CreatedAt     time.Time `json:"created_at"`
}

func (Vital) TableName() string { return "vitals" }

type Encounter struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	UserID       uint       `gorm:"not null;index" json:"user_id"`
	ClinicianID  uint       `gorm:"not null" json:"clinician_id"`
	Department   Department `gorm:"type:varchar(20);not null" json:"department"`
	ChiefComplaint string   `gorm:"type:text" json:"chief_complaint"`
	Notes        string     `gorm:"type:text" json:"notes"`
	Diagnosis    string     `gorm:"type:text" json:"diagnosis"`
	Treatment    string     `gorm:"type:text" json:"treatment"`
	EncounterDate time.Time `gorm:"not null" json:"encounter_date"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type Attachment struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	UserID         uint      `gorm:"not null;index" json:"user_id"`
	FileName       string    `gorm:"not null" json:"file_name"`
	FilePath       string    `gorm:"not null" json:"-"`
	FileSize       int64     `gorm:"not null" json:"file_size"`
	ContentType    string    `gorm:"not null" json:"content_type"`
	SHA256         string    `gorm:"size:64;not null" json:"sha256"`
	UploadedBy     uint      `gorm:"not null" json:"uploaded_by"`
	CreatedAt      time.Time `json:"created_at"`
}

// ===================== AUDIT LOG =====================

type AuditLog struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TableName   string    `gorm:"size:100;not null;index" json:"table_name"`
	RecordID    uint      `gorm:"not null;index" json:"record_id"`
	Action      string    `gorm:"size:20;not null" json:"action"`
	EditorID    uint      `gorm:"not null" json:"editor_id"`
	Reason      string    `gorm:"type:text" json:"reason"`
	Fingerprint string    `gorm:"size:64;not null" json:"fingerprint"`
	Snapshot    string    `gorm:"type:text" json:"snapshot"`
	Timestamp   time.Time `gorm:"not null" json:"timestamp"`
}

func ComputeFingerprint(data string) string {
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h[:])
}

// ===================== ATHLETICS BOOKING =====================

type Venue struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Name     string `gorm:"not null" json:"name"`
	RoomType string `gorm:"size:50;not null" json:"room_type"` // "onsite" or "virtual"
	Capacity int    `json:"capacity"`
}

type TrainerProfile struct {
	ID          uint    `gorm:"primaryKey" json:"id"`
	UserID      uint    `gorm:"uniqueIndex;not null" json:"user_id"`
	SkillLevel  int     `gorm:"not null;default:1" json:"skill_level"` // 1-10
	WeightClass float64 `gorm:"not null" json:"weight_class"`          // in lb
	PrimaryStyle string `gorm:"size:50" json:"primary_style"`
}

type Booking struct {
	ID             uint          `gorm:"primaryKey" json:"id"`
	OrganizationID uint          `gorm:"not null;index" json:"organization_id"`
	RequesterID    uint          `gorm:"not null;index" json:"requester_id"`
	PartnerID      *uint         `gorm:"index" json:"partner_id"`
	VenueID        uint          `gorm:"not null" json:"venue_id"`
	SlotStart      time.Time     `gorm:"not null;index" json:"slot_start"`
	SlotEnd        time.Time     `gorm:"not null" json:"slot_end"`
	Status         BookingStatus `gorm:"type:varchar(20);not null;default:'initiated'" json:"status"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type BookingAudit struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	BookingID uint      `gorm:"not null;index" json:"booking_id"`
	ChangedBy uint      `gorm:"not null" json:"changed_by"`
	OldStatus string    `gorm:"size:20" json:"old_status"`
	NewStatus string    `gorm:"size:20;not null" json:"new_status"`
	Note      string    `gorm:"type:text" json:"note"`
	Timestamp time.Time `gorm:"not null" json:"timestamp"`
}

// ===================== MENU / DINING =====================

type MenuCategory struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	OrganizationID uint   `gorm:"not null;index" json:"organization_id"`
	Name           string `gorm:"not null" json:"name"`
	ParentID       *uint  `gorm:"index" json:"parent_id"`
	SortOrder      int    `json:"sort_order"`
}

type MenuItem struct {
	ID             uint    `gorm:"primaryKey" json:"id"`
	OrganizationID uint    `gorm:"not null;index" json:"organization_id"`
	CategoryID     uint    `gorm:"not null;index" json:"category_id"`
	SKU            string  `gorm:"uniqueIndex;size:50;not null" json:"sku"`
	Name        string  `gorm:"not null" json:"name"`
	Description string  `gorm:"type:text" json:"description"`
	ItemType    string  `gorm:"size:20;not null;default:'dish'" json:"item_type"` // dish, combo, addon
	BasePriceDineIn  float64 `gorm:"not null" json:"base_price_dine_in"`
	BasePriceTakeout float64 `gorm:"not null" json:"base_price_takeout"`
	MemberDiscount   float64 `gorm:"default:0" json:"member_discount"` // percentage
	SoldOut     bool    `gorm:"default:false" json:"sold_out"`
	Active      bool    `gorm:"default:true" json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type MenuItemChoice struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	MenuItemID uint   `gorm:"not null;index" json:"menu_item_id"`
	ChoiceType string `gorm:"size:20;not null" json:"choice_type"` // prep, flavor, size
	Name       string `gorm:"not null" json:"name"`
	ExtraPrice float64 `gorm:"default:0" json:"extra_price"`
}

type ItemSubstitute struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	MenuItemID     uint `gorm:"not null;index" json:"menu_item_id"`
	SubstituteID   uint `gorm:"not null" json:"substitute_id"`
}

type SellWindow struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	MenuItemID uint   `gorm:"not null;index" json:"menu_item_id"`
	DayOfWeek  int    `gorm:"not null" json:"day_of_week"` // 0=Sun, 6=Sat
	OpenTime   string `gorm:"size:10;not null" json:"open_time"`   // "06:30"
	CloseTime  string `gorm:"size:10;not null" json:"close_time"`  // "14:00"
}

type HolidayBlackout struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	OrganizationID uint      `gorm:"not null;index" json:"organization_id"`
	Date           time.Time `gorm:"not null" json:"date"`
	Description    string    `gorm:"type:text" json:"description"`
}

type Promotion struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	MenuItemID  uint      `gorm:"not null;index" json:"menu_item_id"`
	DiscountPct float64   `gorm:"not null" json:"discount_pct"`
	StartsAt    time.Time `gorm:"not null" json:"starts_at"`
	EndsAt      time.Time `gorm:"not null" json:"ends_at"`
	Active      bool      `gorm:"default:true" json:"active"`
}

type MenuOrder struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	OrganizationID uint      `gorm:"not null;index" json:"organization_id"`
	UserID         uint      `gorm:"not null;index" json:"user_id"`
	OrderType   string    `gorm:"size:20;not null" json:"order_type"` // dine_in, takeout
	TotalPrice  float64   `gorm:"not null" json:"total_price"`
	IsMember    bool      `gorm:"default:false" json:"is_member"`
	Status      string    `gorm:"size:20;not null;default:'pending'" json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type MenuOrderItem struct {
	ID          uint    `gorm:"primaryKey" json:"id"`
	OrderID     uint    `gorm:"not null;index" json:"order_id"`
	MenuItemID  uint    `gorm:"not null" json:"menu_item_id"`
	Quantity    int     `gorm:"not null;default:1" json:"quantity"`
	UnitPrice   float64 `gorm:"not null" json:"unit_price"`
	Choices     string  `gorm:"type:text" json:"choices"` // JSON of selected choices
}

// ===================== API TOKENS =====================

type APIToken struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Token          string    `gorm:"uniqueIndex;size:64;not null" json:"-"`
	UserID         uint      `gorm:"not null;index" json:"user_id"`
	OrganizationID uint      `gorm:"not null" json:"organization_id"`
	Description    string    `gorm:"size:255" json:"description"`
	ExpiresAt      time.Time `gorm:"not null" json:"expires_at"`
	Active         bool      `gorm:"default:true" json:"active"`
	CreatedAt      time.Time `json:"created_at"`
}

// ===================== INTEGRATION =====================

type WebhookEndpoint struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	OrganizationID uint      `gorm:"not null;index" json:"organization_id"`
	URL            string    `gorm:"not null" json:"url"`
	EventType      string    `gorm:"size:50;not null;index" json:"event_type"`
	Secret         string    `gorm:"not null" json:"-"`
	Active         bool      `gorm:"default:true" json:"active"`
	CreatedAt      time.Time `json:"created_at"`
}

type WebhookDelivery struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	EndpointID uint      `gorm:"not null;index" json:"endpoint_id"`
	Payload    string    `gorm:"type:text;not null" json:"payload"`
	Status     int       `gorm:"not null" json:"status"`
	Response   string    `gorm:"type:text" json:"response"`
	Attempts   int       `gorm:"default:1" json:"attempts"`
	CreatedAt  time.Time `json:"created_at"`
}

// ===================== ASYNC REPORTS =====================

type ReportJob struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	OrganizationID uint      `gorm:"not null;index" json:"organization_id"`
	ReportType     string    `gorm:"size:50;not null" json:"report_type"`
	Status         string    `gorm:"size:20;not null;default:'pending'" json:"status"` // pending, running, completed, failed
	Result         string    `gorm:"type:text" json:"result"`
	RequestedBy    uint      `gorm:"not null" json:"requested_by"`
	CreatedAt      time.Time `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at"`
}

type SlowQueryLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Query     string    `gorm:"type:text;not null" json:"query"`
	Duration  int64     `gorm:"not null" json:"duration_ms"`
	Caller    string    `gorm:"size:255" json:"caller"`
	CreatedAt time.Time `json:"created_at"`
}
