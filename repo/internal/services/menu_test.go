package services

import (
	"testing"
	"time"

	"campus-portal/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMenuData(t *testing.T, db *gorm.DB) {
	t.Helper()
	db.FirstOrCreate(&models.MenuCategory{Name: "TestCat", SortOrder: 99}, "name = ?", "TestCat")

	var cat models.MenuCategory
	db.First(&cat, "name = ?", "TestCat")

	db.Where("sku = ?", "UNIT-001").Delete(&models.MenuItem{})
	db.Create(&models.MenuItem{
		CategoryID: cat.ID, SKU: "UNIT-001", Name: "Unit Test Burger",
		ItemType: "dish", BasePriceDineIn: 10.00, BasePriceTakeout: 11.00,
		MemberDiscount: 10, Active: true,
	})
}

func getTestItem(db *gorm.DB) models.MenuItem {
	var item models.MenuItem
	db.First(&item, "sku = ?", "UNIT-001")
	return item
}

func TestCalculatePrice_DineIn(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	setupMenuData(t, db)
	audit := NewAuditService(db)
	svc := NewMenuService(db, audit)

	item := getTestItem(db)
	price, err := svc.CalculatePrice(item.ID, "dine_in", false)
	assert.NoError(t, err)
	assert.Equal(t, 10.00, price)
}

func TestCalculatePrice_Takeout(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	setupMenuData(t, db)
	audit := NewAuditService(db)
	svc := NewMenuService(db, audit)

	item := getTestItem(db)
	price, err := svc.CalculatePrice(item.ID, "takeout", false)
	assert.NoError(t, err)
	assert.Equal(t, 11.00, price)
}

func TestCalculatePrice_MemberDiscount(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	setupMenuData(t, db)
	audit := NewAuditService(db)
	svc := NewMenuService(db, audit)

	item := getTestItem(db)
	price, err := svc.CalculatePrice(item.ID, "dine_in", true)
	assert.NoError(t, err)
	assert.Equal(t, 9.00, price) // 10% off $10
}

func TestCalculatePrice_WithPromotion(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	setupMenuData(t, db)
	audit := NewAuditService(db)
	svc := NewMenuService(db, audit)

	item := getTestItem(db)
	db.Create(&models.Promotion{
		MenuItemID: item.ID, DiscountPct: 20,
		StartsAt: time.Now().Add(-1 * time.Hour),
		EndsAt:   time.Now().Add(1 * time.Hour),
		Active:   true,
	})

	price, err := svc.CalculatePrice(item.ID, "dine_in", false)
	assert.NoError(t, err)
	assert.Equal(t, 8.00, price) // 20% off $10
}

func TestCalculatePrice_NotFound(t *testing.T) {
	db := getTestDB(t)
	audit := NewAuditService(db)
	svc := NewMenuService(db, audit)

	_, err := svc.CalculatePrice(999999, "dine_in", false)
	assert.Error(t, err)
}

func TestIsWithinSellWindow_NoWindowsAlwaysAvailable(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	setupMenuData(t, db)
	audit := NewAuditService(db)
	svc := NewMenuService(db, audit)

	item := getTestItem(db)
	avail, err := svc.IsWithinSellWindow(item.ID, time.Now())
	assert.NoError(t, err)
	assert.True(t, avail)
}

func TestIsWithinSellWindow_HolidayBlackout(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	setupMenuData(t, db)
	audit := NewAuditService(db)
	svc := NewMenuService(db, audit)

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	db.Create(&models.HolidayBlackout{Date: today, Description: "Unit Test Holiday"})

	item := getTestItem(db)
	avail, err := svc.IsWithinSellWindow(item.ID, now)
	assert.NoError(t, err)
	assert.False(t, avail)
}

func TestCreateOrder_SoldOutReject(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	setupMenuData(t, db)
	audit := NewAuditService(db)
	svc := NewMenuService(db, audit)

	item := getTestItem(db)
	db.Model(&models.MenuItem{}).Where("id = ?", item.ID).Update("sold_out", true)

	order := &models.MenuOrder{UserID: 1, OrderType: "dine_in", TotalPrice: 10, Status: "pending"}
	items := []models.MenuOrderItem{{MenuItemID: item.ID, Quantity: 1, UnitPrice: 10}}

	err := svc.CreateOrder(order, items)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sold out")
}

func TestSubstitutes(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	setupMenuData(t, db)
	audit := NewAuditService(db)
	svc := NewMenuService(db, audit)

	var cat models.MenuCategory
	db.First(&cat, "name = ?", "TestCat")

	db.Where("sku = ?", "UNIT-002").Delete(&models.MenuItem{})
	db.Create(&models.MenuItem{
		CategoryID: cat.ID, SKU: "UNIT-002", Name: "Alt Burger",
		ItemType: "dish", BasePriceDineIn: 9, BasePriceTakeout: 10, Active: true,
	})

	item1 := getTestItem(db)
	var item2 models.MenuItem
	db.First(&item2, "sku = ?", "UNIT-002")

	require.NotZero(t, item1.ID)
	require.NotZero(t, item2.ID)

	svc.SetSubstitutes(item1.ID, []uint{item2.ID})
	subs, err := svc.GetSubstitutes(item1.ID)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(subs))
	assert.Equal(t, "Alt Burger", subs[0].Name)
}

func TestCreateOrder_Success(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	setupMenuData(t, db)
	audit := NewAuditService(db)
	svc := NewMenuService(db, audit)

	item := getTestItem(db)

	order := &models.MenuOrder{
		UserID:     1,
		OrderType:  "dine_in",
		TotalPrice: 20.00,
		IsMember:   false,
		Status:     "pending",
	}
	items := []models.MenuOrderItem{
		{MenuItemID: item.ID, Quantity: 2, UnitPrice: 10.00},
	}

	err := svc.CreateOrder(order, items)
	require.NoError(t, err)
	assert.NotZero(t, order.ID)

	// Verify order persisted
	var savedOrder models.MenuOrder
	db.First(&savedOrder, order.ID)
	assert.Equal(t, "dine_in", savedOrder.OrderType)
	assert.Equal(t, 20.00, savedOrder.TotalPrice)

	// Verify line items persisted
	var savedItems []models.MenuOrderItem
	db.Where("order_id = ?", order.ID).Find(&savedItems)
	assert.Equal(t, 1, len(savedItems))
	assert.Equal(t, 2, savedItems[0].Quantity)
	assert.Equal(t, 10.00, savedItems[0].UnitPrice)
}
