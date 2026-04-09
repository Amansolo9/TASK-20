package services

import (
	"errors"
	"fmt"
	"time"

	"campus-portal/internal/models"

	"gorm.io/gorm"
)

type MenuService struct {
	DB    *gorm.DB
	Audit *AuditService
}

func NewMenuService(db *gorm.DB, audit *AuditService) *MenuService {
	return &MenuService{DB: db, Audit: audit}
}

// Categories
func (s *MenuService) GetCategories(orgID uint) ([]models.MenuCategory, error) {
	var cats []models.MenuCategory
	err := s.DB.Where("organization_id = ?", orgID).Order("sort_order ASC").Find(&cats).Error
	return cats, err
}

func (s *MenuService) CreateCategory(cat *models.MenuCategory, editorID uint) error {
	if err := s.DB.Create(cat).Error; err != nil {
		return err
	}
	s.Audit.LogChange("menu_categories", cat.ID, "create", editorID, "Category created", cat)
	return nil
}

// Menu Items
func (s *MenuService) GetMenuItems(orgID uint, categoryID *uint) ([]models.MenuItem, error) {
	var items []models.MenuItem
	q := s.DB.Where("organization_id = ?", orgID).Order("name ASC")
	if categoryID != nil {
		q = q.Where("category_id = ?", *categoryID)
	}
	err := q.Find(&items).Error
	return items, err
}

func (s *MenuService) GetMenuItem(id uint) (*models.MenuItem, error) {
	var item models.MenuItem
	err := s.DB.First(&item, id).Error
	return &item, err
}

func (s *MenuService) CreateMenuItem(item *models.MenuItem, editorID uint) error {
	if err := s.DB.Create(item).Error; err != nil {
		return err
	}
	s.Audit.LogChange("menu_items", item.ID, "create", editorID, "Menu item created", item)
	return nil
}

func (s *MenuService) UpdateMenuItem(item *models.MenuItem, editorID uint, reason string) error {
	if err := s.DB.Save(item).Error; err != nil {
		return err
	}
	s.Audit.LogChange("menu_items", item.ID, "update", editorID, reason, item)
	return nil
}

func (s *MenuService) ToggleSoldOut(itemID uint, soldOut bool, editorID uint) error {
	if err := s.DB.Model(&models.MenuItem{}).Where("id = ?", itemID).Update("sold_out", soldOut).Error; err != nil {
		return err
	}
	action := "marked available"
	if soldOut {
		action = "marked sold out"
	}
	s.Audit.LogChange("menu_items", itemID, "update", editorID, action, nil)
	return nil
}

// Substitutes
func (s *MenuService) GetSubstitutes(itemID uint) ([]models.MenuItem, error) {
	var subs []models.ItemSubstitute
	s.DB.Where("menu_item_id = ?", itemID).Find(&subs)

	var items []models.MenuItem
	for _, sub := range subs {
		var item models.MenuItem
		if err := s.DB.First(&item, sub.SubstituteID).Error; err == nil {
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *MenuService) SetSubstitutes(itemID uint, substituteIDs []uint, editorID uint) error {
	s.DB.Where("menu_item_id = ?", itemID).Delete(&models.ItemSubstitute{})
	for _, subID := range substituteIDs {
		s.DB.Create(&models.ItemSubstitute{MenuItemID: itemID, SubstituteID: subID})
	}
	s.Audit.LogChange("item_substitutes", itemID, "update", editorID, "Substitutes updated", substituteIDs)
	return nil
}

// Choices
func (s *MenuService) GetChoices(itemID uint) ([]models.MenuItemChoice, error) {
	var choices []models.MenuItemChoice
	err := s.DB.Where("menu_item_id = ?", itemID).Find(&choices).Error
	return choices, err
}

func (s *MenuService) CreateChoice(choice *models.MenuItemChoice, editorID uint) error {
	if err := s.DB.Create(choice).Error; err != nil {
		return err
	}
	s.Audit.LogChange("menu_item_choices", choice.ID, "create", editorID, "Choice created", choice)
	return nil
}

// Sell Windows
func (s *MenuService) GetSellWindows(itemID uint) ([]models.SellWindow, error) {
	var windows []models.SellWindow
	err := s.DB.Where("menu_item_id = ?", itemID).Order("day_of_week ASC").Find(&windows).Error
	return windows, err
}

func (s *MenuService) SetSellWindows(itemID uint, windows []models.SellWindow, editorID uint) error {
	s.DB.Where("menu_item_id = ?", itemID).Delete(&models.SellWindow{})
	for i := range windows {
		windows[i].MenuItemID = itemID
		s.DB.Create(&windows[i])
	}
	s.Audit.LogChange("sell_windows", itemID, "update", editorID, "Sell windows updated", windows)
	return nil
}

func (s *MenuService) IsWithinSellWindow(itemID uint, now time.Time) (bool, error) {
	// Check org-scoped holiday blackout: only apply blackouts for the item's own org
	item, err := s.GetMenuItem(itemID)
	if err != nil {
		return false, err
	}
	var blackout models.HolidayBlackout
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if err := s.DB.Where("date = ? AND organization_id = ?", today, item.OrganizationID).First(&blackout).Error; err == nil {
		return false, nil // Blacked out for this org
	}

	var windows []models.SellWindow
	s.DB.Where("menu_item_id = ? AND day_of_week = ?", itemID, int(now.Weekday())).Find(&windows)

	if len(windows) == 0 {
		return true, nil // No windows defined = always available
	}

	currentTime := fmt.Sprintf("%02d:%02d", now.Hour(), now.Minute())
	for _, w := range windows {
		if currentTime >= w.OpenTime && currentTime <= w.CloseTime {
			return true, nil
		}
	}

	return false, nil
}

// Holiday Blackouts
func (s *MenuService) GetBlackouts(orgID uint) ([]models.HolidayBlackout, error) {
	var blackouts []models.HolidayBlackout
	err := s.DB.Where("organization_id = ?", orgID).Order("date ASC").Find(&blackouts).Error
	return blackouts, err
}

func (s *MenuService) CreateBlackout(b *models.HolidayBlackout, editorID uint) error {
	if err := s.DB.Create(b).Error; err != nil {
		return err
	}
	s.Audit.LogChange("holiday_blackouts", b.ID, "create", editorID, "Blackout created", b)
	return nil
}

func (s *MenuService) DeleteBlackout(id uint, editorID uint) error {
	s.Audit.LogChange("holiday_blackouts", id, "delete", editorID, "Blackout removed", nil)
	return s.DB.Delete(&models.HolidayBlackout{}, id).Error
}

// Promotions
func (s *MenuService) GetActivePromotions(itemID uint) ([]models.Promotion, error) {
	var promos []models.Promotion
	err := s.DB.Where("menu_item_id = ? AND active = true AND starts_at <= ? AND ends_at >= ?",
		itemID, time.Now(), time.Now()).Find(&promos).Error
	return promos, err
}

func (s *MenuService) CreatePromotion(promo *models.Promotion, editorID uint) error {
	if err := s.DB.Create(promo).Error; err != nil {
		return err
	}
	s.Audit.LogChange("promotions", promo.ID, "create", editorID, "Promotion created", promo)
	return nil
}

// Pricing
func (s *MenuService) CalculatePrice(itemID uint, orderType string, isMember bool) (float64, error) {
	item, err := s.GetMenuItem(itemID)
	if err != nil {
		return 0, err
	}

	var price float64
	if orderType == "takeout" {
		price = item.BasePriceTakeout
	} else {
		price = item.BasePriceDineIn
	}

	// Apply member discount
	if isMember && item.MemberDiscount > 0 {
		price -= price * (item.MemberDiscount / 100)
	}

	// Apply active promotions
	promos, _ := s.GetActivePromotions(itemID)
	for _, p := range promos {
		price -= price * (p.DiscountPct / 100)
	}

	if price < 0 {
		price = 0
	}

	return price, nil
}

// Orders
func (s *MenuService) CreateOrder(order *models.MenuOrder, items []models.MenuOrderItem) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Validate all items are within sell window
		for _, oi := range items {
			available, _ := s.IsWithinSellWindow(oi.MenuItemID, time.Now())
			if !available {
				var mi models.MenuItem
				tx.First(&mi, oi.MenuItemID)
				return errors.New("item not available: " + mi.Name)
			}
			var mi models.MenuItem
			tx.First(&mi, oi.MenuItemID)
			if mi.SoldOut {
				return errors.New("item sold out: " + mi.Name)
			}
		}

		if err := tx.Create(order).Error; err != nil {
			return err
		}

		for i := range items {
			items[i].OrderID = order.ID
			if err := tx.Create(&items[i]).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func (s *MenuService) GetOrders(userID uint) ([]models.MenuOrder, error) {
	var orders []models.MenuOrder
	err := s.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&orders).Error
	return orders, err
}
