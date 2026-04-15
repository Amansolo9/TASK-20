package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"campus-portal/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSOSync_CreateNewUsers(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	auditSvc := NewAuditService(db)

	// Ensure org exists
	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")
	// Hard-delete leftovers from prior runs
	db.Unscoped().Where("username IN ?", []string{"sso_student1", "sso_faculty1"}).Delete(&models.User{})

	dir := t.TempDir()
	ssoFile := filepath.Join(dir, "sso_users.json")

	users := []SSOUser{
		{Username: "sso_student1", FullName: "SSO Student One", Email: "sso1@campus.local", Role: "student", Active: true, Organization: "Campus University"},
		{Username: "sso_faculty1", FullName: "SSO Faculty One", Email: "fac1@campus.local", Role: "faculty", Active: true, Organization: "Campus University"},
	}
	data, _ := json.Marshal(users)
	os.WriteFile(ssoFile, data, 0644)

	source := &FileSSOSource{FilePath: ssoFile}
	svc := NewSSOSyncService(db, source, auditSvc)

	svc.RunOnce()

	// Verify users were created
	var u1 models.User
	err := db.Where("username = ?", "sso_student1").First(&u1).Error
	require.NoError(t, err)
	assert.Equal(t, "SSO Student One", u1.FullName)
	assert.Equal(t, models.RoleStudent, u1.Role)
	assert.True(t, u1.Active)

	var u2 models.User
	err = db.Where("username = ?", "sso_faculty1").First(&u2).Error
	require.NoError(t, err)
	assert.Equal(t, "SSO Faculty One", u2.FullName)
	assert.Equal(t, models.RoleFaculty, u2.Role)

	// Verify audit log was created
	var auditLog models.AuditLog
	err = db.Where("table_name = ? AND record_id = ? AND action = ?", "users", u1.ID, "sso_sync_created").First(&auditLog).Error
	assert.NoError(t, err)
	assert.Contains(t, auditLog.Reason, "SSO sync")

	// Cleanup
	db.Unscoped().Where("username IN ?", []string{"sso_student1", "sso_faculty1"}).Delete(&models.User{})
}

func TestSSOSync_UpdateExistingUser(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	auditSvc := NewAuditService(db)

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")
	db.Unscoped().Where("username = ?", "sso_update_test").Delete(&models.User{})

	// Pre-create a user
	db.Create(&models.User{
		Username: "sso_update_test", PasswordHash: "$2a$10$placeholder",
		FullName: "Old Name", Email: "old@campus.local",
		Role: models.RoleStudent, OrganizationID: 1, Active: true,
	})

	dir := t.TempDir()
	ssoFile := filepath.Join(dir, "sso_users.json")

	users := []SSOUser{
		{Username: "sso_update_test", FullName: "New Name", Email: "new@campus.local", Role: "faculty", Active: true, Organization: "Campus University"},
	}
	data, _ := json.Marshal(users)
	os.WriteFile(ssoFile, data, 0644)

	source := &FileSSOSource{FilePath: ssoFile}
	svc := NewSSOSyncService(db, source, auditSvc)

	svc.RunOnce()

	var user models.User
	db.Where("username = ?", "sso_update_test").First(&user)
	assert.Equal(t, "New Name", user.FullName)
	assert.Equal(t, "new@campus.local", user.Email)
	assert.Equal(t, models.RoleFaculty, user.Role)

	// Verify audit log was created for the update
	var auditLog models.AuditLog
	err := db.Where("table_name = ? AND record_id = ? AND action = ?", "users", user.ID, "sso_sync_updated").First(&auditLog).Error
	assert.NoError(t, err)
	assert.Contains(t, auditLog.Reason, "SSO sync")

	// Cleanup
	db.Unscoped().Where("username = ?", "sso_update_test").Delete(&models.User{})
}

func TestSSOSync_DeactivateUser(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	auditSvc := NewAuditService(db)

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")
	db.Unscoped().Where("username = ?", "sso_deactivate_test").Delete(&models.User{})

	// Pre-create an active user
	db.Create(&models.User{
		Username: "sso_deactivate_test", PasswordHash: "$2a$10$placeholder",
		FullName: "Deactivate Me", Email: "deact@campus.local",
		Role: models.RoleStudent, OrganizationID: 1, Active: true,
	})

	dir := t.TempDir()
	ssoFile := filepath.Join(dir, "sso_users.json")

	users := []SSOUser{
		{Username: "sso_deactivate_test", FullName: "Deactivate Me", Email: "deact@campus.local", Role: "student", Active: false, Organization: "Campus University"},
	}
	data, _ := json.Marshal(users)
	os.WriteFile(ssoFile, data, 0644)

	source := &FileSSOSource{FilePath: ssoFile}
	svc := NewSSOSyncService(db, source, auditSvc)

	svc.RunOnce()

	var user models.User
	db.Where("username = ?", "sso_deactivate_test").First(&user)
	assert.False(t, user.Active, "user should be deactivated by SSO sync")

	// Cleanup
	db.Unscoped().Where("username = ?", "sso_deactivate_test").Delete(&models.User{})
}

func TestSSOSync_InvalidRoleSkipped(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	auditSvc := NewAuditService(db)

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")

	dir := t.TempDir()
	ssoFile := filepath.Join(dir, "sso_users.json")

	users := []SSOUser{
		{Username: "sso_badrole", FullName: "Bad Role", Email: "bad@campus.local", Role: "superadmin", Active: true},
	}
	data, _ := json.Marshal(users)
	os.WriteFile(ssoFile, data, 0644)

	source := &FileSSOSource{FilePath: ssoFile}
	svc := NewSSOSyncService(db, source, auditSvc)

	svc.RunOnce()

	// User should NOT be created
	var count int64
	db.Model(&models.User{}).Where("username = ?", "sso_badrole").Count(&count)
	assert.Equal(t, int64(0), count, "user with invalid role should be skipped")
}

func TestSSOSync_AutoCreateDepartment(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	auditSvc := NewAuditService(db)

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")
	db.Unscoped().Where("username = ?", "sso_newdept").Delete(&models.User{})

	dir := t.TempDir()
	ssoFile := filepath.Join(dir, "sso_users.json")

	users := []SSOUser{
		{Username: "sso_newdept", FullName: "New Dept User", Email: "newdept@campus.local", Role: "staff", Active: true, Department: "Radiology", Organization: "Campus University"},
	}
	data, _ := json.Marshal(users)
	os.WriteFile(ssoFile, data, 0644)

	source := &FileSSOSource{FilePath: ssoFile}
	svc := NewSSOSyncService(db, source, auditSvc)

	svc.RunOnce()

	// Department should be auto-created
	var dept models.DepartmentRecord
	err := db.Where("name = ? AND organization_id = ?", "Radiology", 1).First(&dept).Error
	assert.NoError(t, err, "department Radiology should be auto-created")

	// User should have the department assigned
	var user models.User
	db.Where("username = ?", "sso_newdept").First(&user)
	require.NotNil(t, user.DepartmentID)
	assert.Equal(t, dept.ID, *user.DepartmentID)

	// Cleanup
	db.Unscoped().Where("username = ?", "sso_newdept").Delete(&models.User{})
	db.Where("name = ? AND organization_id = ?", "Radiology", 1).Delete(&models.DepartmentRecord{})
}

func TestSSOSync_EmptyUsernameSkipped(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	auditSvc := NewAuditService(db)

	dir := t.TempDir()
	ssoFile := filepath.Join(dir, "sso_users.json")

	users := []SSOUser{
		{Username: "", FullName: "No Username", Email: "none@campus.local", Role: "student", Active: true},
	}
	data, _ := json.Marshal(users)
	os.WriteFile(ssoFile, data, 0644)

	source := &FileSSOSource{FilePath: ssoFile}
	svc := NewSSOSyncService(db, source, auditSvc)

	svc.RunOnce()

	var count int64
	db.Model(&models.User{}).Where("email = ?", "none@campus.local").Count(&count)
	assert.Equal(t, int64(0), count, "user with empty username should be skipped")
}

func TestSSOSync_FileNotFound(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	auditSvc := NewAuditService(db)

	source := &FileSSOSource{FilePath: "/nonexistent/path/sso.json"}
	svc := NewSSOSyncService(db, source, auditSvc)

	// Should not panic, just log error
	svc.RunOnce()
}

func TestSSOSync_MalformedJSON(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	auditSvc := NewAuditService(db)

	dir := t.TempDir()
	ssoFile := filepath.Join(dir, "sso_users.json")
	os.WriteFile(ssoFile, []byte("{invalid json"), 0644)

	source := &FileSSOSource{FilePath: ssoFile}
	svc := NewSSOSyncService(db, source, auditSvc)

	// Should not panic
	svc.RunOnce()
}

func TestSSOSync_IdempotentRerun(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	auditSvc := NewAuditService(db)

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")
	db.Unscoped().Where("username = ?", "sso_idempotent").Delete(&models.User{})

	dir := t.TempDir()
	ssoFile := filepath.Join(dir, "sso_users.json")

	users := []SSOUser{
		{Username: "sso_idempotent", FullName: "Idem User", Email: "idem@campus.local", Role: "student", Active: true, Organization: "Campus University"},
	}
	data, _ := json.Marshal(users)
	os.WriteFile(ssoFile, data, 0644)

	source := &FileSSOSource{FilePath: ssoFile}
	svc := NewSSOSyncService(db, source, auditSvc)

	// Run twice
	svc.RunOnce()
	svc.RunOnce()

	// Should only create one user
	var count int64
	db.Model(&models.User{}).Where("username = ?", "sso_idempotent").Count(&count)
	assert.Equal(t, int64(1), count, "idempotent re-run should not duplicate users")

	// Second run should not produce an "sso_sync_updated" audit log (no changes)
	var auditCount int64
	db.Model(&models.AuditLog{}).Where("action = ? AND reason LIKE ?", "sso_sync_updated", "%sso_idempotent%").Count(&auditCount)
	assert.Equal(t, int64(0), auditCount, "re-run with no changes should not produce update audit log")

	// Cleanup
	db.Unscoped().Where("username = ?", "sso_idempotent").Delete(&models.User{})
}
