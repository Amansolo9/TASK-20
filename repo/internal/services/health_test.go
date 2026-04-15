package services

import (
	"testing"

	"campus-portal/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertHealthRecord_CreateNew(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	setupTestEncryption(t)
	auditSvc := NewAuditService(db)
	svc := NewHealthService(db, auditSvc, t.TempDir())

	// Ensure user exists
	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")
	db.FirstOrCreate(&models.User{
		Username: "health_test_user", PasswordHash: "$2a$10$placeholder",
		FullName: "Health Test", Role: models.RoleStudent, OrganizationID: 1, Active: true,
	}, "username = ?", "health_test_user")
	var user models.User
	db.First(&user, "username = ?", "health_test_user")

	record, err := svc.UpsertHealthRecord(user.ID, 1, "penicillin", "asthma", "albuterol", "A+", "initial record")
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.NotZero(t, record.ID)

	// Read it back and check decrypted values
	retrieved, err := svc.GetHealthRecord(user.ID)
	require.NoError(t, err)
	assert.Equal(t, "penicillin", retrieved.Allergies)
	assert.Equal(t, "asthma", retrieved.Conditions)
	assert.Equal(t, "albuterol", retrieved.Medications)
	assert.Equal(t, "A+", retrieved.BloodType)

	// Verify audit log
	var audit models.AuditLog
	err = db.Where("table_name = ? AND record_id = ? AND action = ?", "health_records", record.ID, "create").First(&audit).Error
	assert.NoError(t, err)
	assert.Contains(t, audit.Reason, "initial record")

	// Cleanup
	db.Where("user_id = ?", user.ID).Delete(&models.HealthRecord{})
	db.Where("username = ?", "health_test_user").Delete(&models.User{})
}

func TestUpsertHealthRecord_UpdateExisting(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	setupTestEncryption(t)
	auditSvc := NewAuditService(db)
	svc := NewHealthService(db, auditSvc, t.TempDir())

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")
	db.FirstOrCreate(&models.User{
		Username: "health_update_user", PasswordHash: "$2a$10$placeholder",
		FullName: "Health Update", Role: models.RoleStudent, OrganizationID: 1, Active: true,
	}, "username = ?", "health_update_user")
	var user models.User
	db.First(&user, "username = ?", "health_update_user")

	// Create initial record
	svc.UpsertHealthRecord(user.ID, 1, "none", "none", "none", "O+", "initial")

	// Update the record
	record, err := svc.UpsertHealthRecord(user.ID, 1, "peanuts", "diabetes", "insulin", "O+", "updated allergies")
	require.NoError(t, err)

	// Read back
	retrieved, err := svc.GetHealthRecord(user.ID)
	require.NoError(t, err)
	assert.Equal(t, "peanuts", retrieved.Allergies)
	assert.Equal(t, "diabetes", retrieved.Conditions)
	assert.Equal(t, "insulin", retrieved.Medications)

	// Verify update audit log
	var audit models.AuditLog
	err = db.Where("table_name = ? AND record_id = ? AND action = ?", "health_records", record.ID, "update").First(&audit).Error
	assert.NoError(t, err)
	assert.Contains(t, audit.Reason, "updated allergies")

	// Cleanup
	db.Where("user_id = ?", user.ID).Delete(&models.HealthRecord{})
	db.Where("username = ?", "health_update_user").Delete(&models.User{})
}

func TestGetHealthRecord_NotFound(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	setupTestEncryption(t)
	auditSvc := NewAuditService(db)
	svc := NewHealthService(db, auditSvc, t.TempDir())

	_, err := svc.GetHealthRecord(999999)
	assert.Error(t, err)
}

func TestRecordVitals_Success(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	auditSvc := NewAuditService(db)
	svc := NewHealthService(db, auditSvc, t.TempDir())

	vital := &models.Vital{
		UserID:       1,
		WeightLb:     165.5,
		BPSystolic:   120,
		BPDiastolic:  80,
		TemperatureF: 98.6,
		HeartRate:    72,
		RecordedBy:   1,
	}

	err := svc.RecordVitals(vital, "routine check")
	require.NoError(t, err)
	assert.NotZero(t, vital.ID)

	// Verify vitals were stored
	vitals, err := svc.GetRecentVitals(1, 10)
	require.NoError(t, err)
	found := false
	for _, v := range vitals {
		if v.WeightLb == 165.5 && v.BPSystolic == 120 {
			found = true
		}
	}
	assert.True(t, found, "recorded vitals should be retrievable")
}

func TestGetEncountersByDept_OrgIsolation(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	svc := NewHealthService(db, nil, "")

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")

	// Create org 2
	var org2 models.Organization
	db.Where("name = ?", "Isolation Test Org").FirstOrCreate(&org2, models.Organization{Name: "Isolation Test Org"})

	// Create clinicians for each org
	db.FirstOrCreate(&models.User{
		Username: "clinician_iso1", PasswordHash: "$2a$10$placeholder",
		FullName: "Clinician Org1", Role: models.RoleClinician, OrganizationID: 1, Active: true,
	}, "username = ?", "clinician_iso1")
	db.FirstOrCreate(&models.User{
		Username: "clinician_iso2", PasswordHash: "$2a$10$placeholder",
		FullName: "Clinician Org2", Role: models.RoleClinician, OrganizationID: org2.ID, Active: true,
	}, "username = ?", "clinician_iso2")

	var clin1, clin2 models.User
	db.First(&clin1, "username = ?", "clinician_iso1")
	db.First(&clin2, "username = ?", "clinician_iso2")

	// Create encounters for each org
	db.Create(&models.Encounter{
		UserID: 1, ClinicianID: clin1.ID, Department: "general",
		ChiefComplaint: "org1_iso_test", EncounterDate: models.Encounter{}.EncounterDate,
	})
	db.Create(&models.Encounter{
		UserID: clin2.ID, ClinicianID: clin2.ID, Department: "general",
		ChiefComplaint: "org2_iso_test", EncounterDate: models.Encounter{}.EncounterDate,
	})

	// Org 1 should not see org 2 encounters
	enc1, err := svc.GetEncountersByDept("general", 1)
	require.NoError(t, err)
	for _, e := range enc1 {
		assert.NotEqual(t, "org2_iso_test", e.ChiefComplaint, "org 1 must not see org 2 encounters")
	}

	// Org 2 should not see org 1 encounters
	enc2, err := svc.GetEncountersByDept("general", org2.ID)
	require.NoError(t, err)
	for _, e := range enc2 {
		assert.NotEqual(t, "org1_iso_test", e.ChiefComplaint, "org 2 must not see org 1 encounters")
	}

	// Cleanup
	db.Where("username IN ?", []string{"clinician_iso1", "clinician_iso2"}).Delete(&models.User{})
	db.Where("name = ?", "Isolation Test Org").Delete(&models.Organization{})
}

func TestMaskSSN(t *testing.T) {
	tests := []struct {
		name     string
		ssn      string
		role     models.Role
		expected string
	}{
		{"empty SSN", "", models.RoleStudent, ""},
		{"admin sees full", "123-45-6789", models.RoleAdmin, "123-45-6789"},
		{"student sees masked", "123-45-6789", models.RoleStudent, "***-**-6789"},
		{"clinician sees masked", "123456789", models.RoleClinician, "***-**-6789"},
		{"short SSN", "1234", models.RoleStudent, "***-**-1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskSSN(tt.ssn, tt.role)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskEmail(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		role     models.Role
		expected string
	}{
		{"admin sees full", "admin@campus.local", models.RoleAdmin, "admin@campus.local"},
		{"clinician sees full", "patient@campus.local", models.RoleClinician, "patient@campus.local"},
		{"student sees masked", "student@campus.local", models.RoleStudent, "st***@campus.local"},
		{"faculty sees masked", "faculty@campus.local", models.RoleFaculty, "fa***@campus.local"},
		{"invalid email", "noemail", models.RoleStudent, "***@***"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskEmail(tt.email, tt.role)
			assert.Equal(t, tt.expected, result)
		})
	}
}
