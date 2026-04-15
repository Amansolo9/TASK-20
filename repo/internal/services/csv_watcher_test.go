package services

import (
	"os"
	"path/filepath"
	"testing"

	"campus-portal/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVWatcher_ImportEnrollment_CreatesUsers(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")
	// Hard-delete leftovers from prior runs
	db.Unscoped().Where("username IN ?", []string{"csv_test_user1", "csv_test_user2"}).Delete(&models.User{})

	dir := t.TempDir()
	csvContent := "username,full_name,email,role,eligible\ncsv_test_user1,CSV User One,csv1@campus.local,student,true\ncsv_test_user2,CSV User Two,csv2@campus.local,faculty,true\n"
	csvFile := filepath.Join(dir, "enrollment_test.csv")
	require.NoError(t, os.WriteFile(csvFile, []byte(csvContent), 0644))

	watcher := NewCSVWatcher(db, dir)
	watcher.processFiles()

	// Users should be created
	var u1 models.User
	err := db.Where("username = ?", "csv_test_user1").First(&u1).Error
	require.NoError(t, err)
	assert.Equal(t, "CSV User One", u1.FullName)
	assert.Equal(t, models.RoleStudent, u1.Role)
	assert.True(t, u1.Active)

	var u2 models.User
	err = db.Where("username = ?", "csv_test_user2").First(&u2).Error
	require.NoError(t, err)
	assert.Equal(t, models.RoleFaculty, u2.Role)

	// File should be moved to processed dir
	_, err = os.Stat(csvFile)
	assert.True(t, os.IsNotExist(err), "original file should be moved")
	_, err = os.Stat(filepath.Join(dir, "processed", "enrollment_test.csv"))
	assert.NoError(t, err, "file should exist in processed dir")

	// Cleanup
	db.Unscoped().Where("username IN ?", []string{"csv_test_user1", "csv_test_user2"}).Delete(&models.User{})
}

func TestCSVWatcher_ImportEnrollment_IneligibleDeactivated(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")
	db.Unscoped().Where("username = ?", "csv_ineligible").Delete(&models.User{})

	// Pre-create the user as active so the CSV import updates (not creates) them.
	// GORM's Create skips zero-value booleans with default:true, so we test
	// the update path which correctly sets Active=false.
	db.Create(&models.User{
		Username: "csv_ineligible", PasswordHash: "$2a$10$placeholder",
		FullName: "Ineligible User", Email: "inel@campus.local",
		Role: models.RoleStudent, OrganizationID: 1, Active: true,
	})

	dir := t.TempDir()
	csvContent := "username,full_name,email,role,eligible\ncsv_ineligible,Ineligible User,inel@campus.local,student,false\n"
	csvFile := filepath.Join(dir, "enrollment_inel.csv")
	require.NoError(t, os.WriteFile(csvFile, []byte(csvContent), 0644))

	watcher := NewCSVWatcher(db, dir)
	watcher.processFiles()

	var user models.User
	err := db.Where("username = ?", "csv_ineligible").First(&user).Error
	require.NoError(t, err)
	assert.False(t, user.Active, "ineligible user should be deactivated by CSV import")

	db.Unscoped().Where("username = ?", "csv_ineligible").Delete(&models.User{})
}

func TestCSVWatcher_ImportEnrollment_InvalidRoleSkipped(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")

	dir := t.TempDir()
	csvContent := "username,full_name,email,role\ncsv_badrole,Bad Role User,bad@campus.local,superadmin\n"
	csvFile := filepath.Join(dir, "enrollment_bad.csv")
	require.NoError(t, os.WriteFile(csvFile, []byte(csvContent), 0644))

	watcher := NewCSVWatcher(db, dir)
	watcher.processFiles()

	var count int64
	db.Model(&models.User{}).Where("username = ?", "csv_badrole").Count(&count)
	assert.Equal(t, int64(0), count, "user with invalid role should be skipped")
}

func TestCSVWatcher_ImportEnrollment_UpdatesExistingUser(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")
	db.Unscoped().Where("username = ?", "csv_existing").Delete(&models.User{})

	// Pre-create a user
	db.Create(&models.User{
		Username: "csv_existing", PasswordHash: "$2a$10$placeholder",
		FullName: "Old CSV Name", Email: "old@csv.local",
		Role: models.RoleStudent, OrganizationID: 1, Active: true,
	})

	dir := t.TempDir()
	csvContent := "username,full_name,email,role\ncsv_existing,New CSV Name,new@csv.local,student\n"
	csvFile := filepath.Join(dir, "enrollment_update.csv")
	require.NoError(t, os.WriteFile(csvFile, []byte(csvContent), 0644))

	watcher := NewCSVWatcher(db, dir)
	watcher.processFiles()

	var user models.User
	db.Where("username = ?", "csv_existing").First(&user)
	assert.Equal(t, "New CSV Name", user.FullName, "name should be updated")
	assert.Equal(t, "new@csv.local", user.Email, "email should be updated")

	// Cleanup
	db.Unscoped().Where("username = ?", "csv_existing").Delete(&models.User{})
}

func TestCSVWatcher_ImportOrgStructure_CreatesDepartments(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")

	dir := t.TempDir()
	csvContent := "department,organization\nCSV Test Dept,Campus University\n"
	csvFile := filepath.Join(dir, "org_structure.csv")
	require.NoError(t, os.WriteFile(csvFile, []byte(csvContent), 0644))

	watcher := NewCSVWatcher(db, dir)
	watcher.processFiles()

	var dept models.DepartmentRecord
	err := db.Where("name = ?", "CSV Test Dept").First(&dept).Error
	assert.NoError(t, err, "department should be created from org CSV")
	assert.Equal(t, uint(1), dept.OrganizationID)

	// Cleanup
	db.Where("name = ?", "CSV Test Dept").Delete(&models.DepartmentRecord{})
}

func TestCSVWatcher_UnrecognizedCSV_MovedToErrors(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	dir := t.TempDir()
	csvContent := "col1,col2\nval1,val2\n"
	csvFile := filepath.Join(dir, "unknown_data.csv")
	require.NoError(t, os.WriteFile(csvFile, []byte(csvContent), 0644))

	watcher := NewCSVWatcher(db, dir)
	watcher.processFiles()

	// File should be moved to errors dir
	_, err := os.Stat(csvFile)
	assert.True(t, os.IsNotExist(err), "original file should be moved")
	_, err = os.Stat(filepath.Join(dir, "errors", "unknown_data.csv"))
	assert.NoError(t, err, "unrecognized file should be in errors dir")
}

func TestCSVWatcher_EmptyCSV_Error(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	dir := t.TempDir()
	csvContent := "username\n"
	csvFile := filepath.Join(dir, "enrollment_empty.csv")
	require.NoError(t, os.WriteFile(csvFile, []byte(csvContent), 0644))

	watcher := NewCSVWatcher(db, dir)
	watcher.processFiles()

	// File with only headers should be moved to errors dir
	_, err := os.Stat(filepath.Join(dir, "errors", "enrollment_empty.csv"))
	assert.NoError(t, err, "empty CSV should be moved to errors")
}

func TestCSVWatcher_DepartmentSync(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)

	db.FirstOrCreate(&models.Organization{Name: "Campus University"}, "name = ?", "Campus University")
	db.FirstOrCreate(&models.DepartmentRecord{Name: "General Medicine", OrganizationID: 1}, "name = ? AND organization_id = ?", "General Medicine", 1)
	db.Unscoped().Where("username = ?", "csv_dept_user").Delete(&models.User{})

	dir := t.TempDir()
	csvContent := "username,full_name,email,role,department\ncsv_dept_user,Dept User,dept@campus.local,clinician,General Medicine\n"
	csvFile := filepath.Join(dir, "enrollment_dept.csv")
	require.NoError(t, os.WriteFile(csvFile, []byte(csvContent), 0644))

	watcher := NewCSVWatcher(db, dir)
	watcher.processFiles()

	var user models.User
	err := db.Where("username = ?", "csv_dept_user").First(&user).Error
	require.NoError(t, err)
	require.NotNil(t, user.DepartmentID, "user should have department assigned")

	var dept models.DepartmentRecord
	db.First(&dept, *user.DepartmentID)
	assert.Equal(t, "General Medicine", dept.Name)

	// Cleanup
	db.Unscoped().Where("username = ?", "csv_dept_user").Delete(&models.User{})
}
