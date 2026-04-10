package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"campus-portal/internal/models"
	"campus-portal/internal/services"
	"campus-portal/internal/views"

	"github.com/gin-gonic/gin"
)

type MenuHandler struct {
	MenuSvc *services.MenuService
}

func NewMenuHandler(menuSvc *services.MenuService) *MenuHandler {
	return &MenuHandler{MenuSvc: menuSvc}
}

var timeFormatRE = regexp.MustCompile(`^\d{2}:\d{2}$`)

// verifyMenuItemOrg checks that a menu item belongs to the user's org. Returns false and sends 403 if not.
func (h *MenuHandler) verifyMenuItemOrg(c *gin.Context, itemID uint) bool {
	item, err := h.MenuSvc.GetMenuItem(itemID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return false
	}
	orgID, _ := c.Get("orgID")
	if item.OrganizationID != orgID.(uint) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied: item belongs to another organization"})
		return false
	}
	return true
}

func (h *MenuHandler) MenuPage(c *gin.Context) {
	user := GetCurrentUser(c)
	orgID, _ := c.Get("orgID")
	categories, _ := h.MenuSvc.GetCategories(orgID.(uint))

	var catID *uint
	if cid := c.Query("category_id"); cid != "" {
		if id, err := strconv.ParseUint(cid, 10, 64); err == nil {
			uid := uint(id)
			catID = &uid
		}
	}

	items, _ := h.MenuSvc.GetMenuItems(orgID.(uint), catID)

	type EnrichedItem struct {
		models.MenuItem
		Available   bool                    `json:"available"`
		FinalPrice  float64                 `json:"final_price"`
		Substitutes []models.MenuItem       `json:"substitutes"`
		Choices     []models.MenuItemChoice `json:"choices"`
	}

	var enriched []EnrichedItem
	for _, item := range items {
		avail, _ := h.MenuSvc.IsWithinSellWindow(item.ID, time.Now())
		price, _ := h.MenuSvc.CalculatePrice(item.ID, "dine_in", false)
		subs, _ := h.MenuSvc.GetSubstitutes(item.ID)
		choices, _ := h.MenuSvc.GetChoices(item.ID)
		enriched = append(enriched, EnrichedItem{
			MenuItem:    item,
			Available:   avail && !item.SoldOut,
			FinalPrice:  price,
			Substitutes: subs,
			Choices:     choices,
		})
	}

	md := views.MenuData{
		User:      &views.UserInfo{FullName: user.FullName, Role: string(user.Role)},
		ActiveCat: catID,
	}
	for _, cat := range categories {
		md.Categories = append(md.Categories, views.CategoryOption{ID: cat.ID, Name: cat.Name})
	}
	for _, item := range enriched {
		ei := views.EnrichedMenuItem{
			ID:          item.ID,
			SKU:         item.SKU,
			Name:        item.Name,
			Description: item.Description,
			ItemType:    item.ItemType,
			FinalPrice:  item.FinalPrice,
			SoldOut:     item.MenuItem.SoldOut,
			Available:   item.Available,
		}
		for _, ch := range item.Choices {
			ei.Choices = append(ei.Choices, views.ChoiceInfo{Name: ch.Name, ChoiceType: ch.ChoiceType, ExtraPrice: ch.ExtraPrice})
		}
		for _, sub := range item.Substitutes {
			ei.Substitutes = append(ei.Substitutes, views.SubstituteInfo{Name: sub.Name})
		}
		md.Items = append(md.Items, ei)
	}
	views.Render(c, http.StatusOK, views.MenuPage(md))
}

func (h *MenuHandler) MenuManagePage(c *gin.Context) {
	user := GetCurrentUser(c)
	orgID, _ := c.Get("orgID")
	categories, _ := h.MenuSvc.GetCategories(orgID.(uint))
	items, _ := h.MenuSvc.GetMenuItems(orgID.(uint), nil)
	blackouts, _ := h.MenuSvc.GetBlackouts(orgID.(uint))

	c.HTML(http.StatusOK, "menu_manage.html", gin.H{
		"title":      "Menu Management",
		"user":       user,
		"categories": categories,
		"items":      items,
		"blackouts":  blackouts,
	})
}

func (h *MenuHandler) CreateCategory(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "category name is required"})
		return
	}

	orgID, _ := c.Get("orgID")
	var parentID *uint
	if pid := c.PostForm("parent_id"); pid != "" {
		if id, err := strconv.ParseUint(pid, 10, 64); err == nil {
			// Verify parent category belongs to same org
			var parentCat models.MenuCategory
			if err := h.MenuSvc.DB.First(&parentCat, uint(id)).Error; err != nil || parentCat.OrganizationID != orgID.(uint) {
				c.JSON(http.StatusForbidden, gin.H{"error": "parent category not in your organization"})
				return
			}
			uid := uint(id)
			parentID = &uid
		}
	}
	sortOrder, _ := strconv.Atoi(c.PostForm("sort_order"))

	user := GetCurrentUser(c)
	cat := &models.MenuCategory{OrganizationID: orgID.(uint), Name: name, ParentID: parentID, SortOrder: sortOrder}
	if err := h.MenuSvc.CreateCategory(cat, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create category: " + err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/menu/manage")
}

func (h *MenuHandler) CreateMenuItem(c *gin.Context) {
	user := GetCurrentUser(c)
	catID, err := strconv.ParseUint(c.PostForm("category_id"), 10, 64)
	if err != nil || catID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid category ID"})
		return
	}
	// Verify category belongs to caller's org
	orgIDv, _ := c.Get("orgID")
	var cat models.MenuCategory
	if err := h.MenuSvc.DB.First(&cat, uint(catID)).Error; err != nil || cat.OrganizationID != orgIDv.(uint) {
		c.JSON(http.StatusForbidden, gin.H{"error": "category not in your organization"})
		return
	}

	sku := strings.TrimSpace(c.PostForm("sku"))
	name := strings.TrimSpace(c.PostForm("name"))
	if sku == "" || name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "SKU and name are required"})
		return
	}

	priceDineIn, _ := strconv.ParseFloat(c.PostForm("base_price_dine_in"), 64)
	priceTakeout, _ := strconv.ParseFloat(c.PostForm("base_price_takeout"), 64)
	memberDiscount, _ := strconv.ParseFloat(c.PostForm("member_discount"), 64)

	if priceDineIn < 0 || priceTakeout < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prices must be >= 0"})
		return
	}
	if memberDiscount < 0 || memberDiscount > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "member discount must be between 0 and 100"})
		return
	}

	itemType := c.PostForm("item_type")
	validTypes := map[string]bool{"dish": true, "combo": true, "addon": true}
	if !validTypes[itemType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "item type must be dish, combo, or addon"})
		return
	}

	orgID, _ := c.Get("orgID")
	item := &models.MenuItem{
		OrganizationID:  orgID.(uint),
		CategoryID:       uint(catID),
		SKU:              sku,
		Name:             name,
		Description:      c.PostForm("description"),
		ItemType:         itemType,
		BasePriceDineIn:  priceDineIn,
		BasePriceTakeout: priceTakeout,
		MemberDiscount:   memberDiscount,
	}

	if err := h.MenuSvc.CreateMenuItem(item, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create item: " + err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/menu/manage")
}

func (h *MenuHandler) ToggleSoldOut(c *gin.Context) {
	user := GetCurrentUser(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item ID"})
		return
	}
	if !h.verifyMenuItemOrg(c, uint(id)) {
		return
	}
	soldOut := c.PostForm("sold_out") == "true"
	if err := h.MenuSvc.ToggleSoldOut(uint(id), soldOut, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update item: " + err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/menu/manage")
}

func (h *MenuHandler) SetSellWindows(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item ID"})
		return
	}
	if !h.verifyMenuItemOrg(c, uint(id)) {
		return
	}

	days := c.PostFormArray("day_of_week")
	opens := c.PostFormArray("open_time")
	closes := c.PostFormArray("close_time")

	var windows []models.SellWindow
	for i := range days {
		if i >= len(opens) || i >= len(closes) {
			break
		}
		day, dayErr := strconv.Atoi(days[i])
		if dayErr != nil || day < 0 || day > 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid day_of_week at index %d: must be 0-6", i)})
			return
		}
		if !timeFormatRE.MatchString(opens[i]) || !timeFormatRE.MatchString(closes[i]) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid time format at index %d: use HH:MM", i)})
			return
		}
		if opens[i] >= closes[i] {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("open time must be before close time at index %d", i)})
			return
		}
		windows = append(windows, models.SellWindow{
			DayOfWeek: day, OpenTime: opens[i], CloseTime: closes[i],
		})
	}

	user := GetCurrentUser(c)
	if err := h.MenuSvc.SetSellWindows(uint(id), windows, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to set sell windows: " + err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/menu/manage")
}

func (h *MenuHandler) SetSubstitutes(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item ID"})
		return
	}
	if !h.verifyMenuItemOrg(c, uint(id)) {
		return
	}
	subIDs := c.PostForm("substitute_ids")
	orgID, _ := c.Get("orgID")

	var ids []uint
	for _, s := range strings.Split(subIDs, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if sid, err := strconv.ParseUint(s, 10, 64); err == nil {
			// Verify each substitute item belongs to same org
			subItem, subErr := h.MenuSvc.GetMenuItem(uint(sid))
			if subErr != nil || subItem.OrganizationID != orgID.(uint) {
				c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("substitute item %d not in your organization", sid)})
				return
			}
			ids = append(ids, uint(sid))
		}
	}

	user := GetCurrentUser(c)
	if err := h.MenuSvc.SetSubstitutes(uint(id), ids, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to set substitutes: " + err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/menu/manage")
}

func (h *MenuHandler) CreateBlackout(c *gin.Context) {
	dateStr := c.PostForm("date")
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format (use YYYY-MM-DD)"})
		return
	}
	desc := strings.TrimSpace(c.PostForm("description"))

	user := GetCurrentUser(c)
	orgID, _ := c.Get("orgID")
	if err := h.MenuSvc.CreateBlackout(&models.HolidayBlackout{OrganizationID: orgID.(uint), Date: date, Description: desc}, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create blackout: " + err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/menu/manage")
}

func (h *MenuHandler) DeleteBlackout(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid blackout ID"})
		return
	}
	// Verify blackout belongs to user's org
	orgID, _ := c.Get("orgID")
	var blackout models.HolidayBlackout
	if err := h.MenuSvc.DB.First(&blackout, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "blackout not found"})
		return
	}
	if blackout.OrganizationID != orgID.(uint) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}
	user := GetCurrentUser(c)
	if err := h.MenuSvc.DeleteBlackout(uint(id), user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete blackout: " + err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/menu/manage")
}

func (h *MenuHandler) CreatePromotion(c *gin.Context) {
	itemID, err := strconv.ParseUint(c.PostForm("menu_item_id"), 10, 64)
	if err != nil || itemID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid menu item ID"})
		return
	}
	if !h.verifyMenuItemOrg(c, uint(itemID)) {
		return
	}
	discount, _ := strconv.ParseFloat(c.PostForm("discount_pct"), 64)
	if discount <= 0 || discount > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "discount must be between 0 and 100"})
		return
	}
	startsAt, err1 := time.Parse("2006-01-02T15:04", c.PostForm("starts_at"))
	endsAt, err2 := time.Parse("2006-01-02T15:04", c.PostForm("ends_at"))
	if err1 != nil || err2 != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date/time format"})
		return
	}
	if !endsAt.After(startsAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end date must be after start date"})
		return
	}

	user := GetCurrentUser(c)
	if err := h.MenuSvc.CreatePromotion(&models.Promotion{
		MenuItemID: uint(itemID), DiscountPct: discount,
		StartsAt: startsAt, EndsAt: endsAt, Active: true,
	}, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create promotion: " + err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/menu/manage")
}

func (h *MenuHandler) CalculatePrice(c *gin.Context) {
	itemID, err := strconv.ParseUint(c.Query("item_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item ID"})
		return
	}
	// Verify item belongs to caller's org
	if !h.verifyMenuItemOrg(c, uint(itemID)) {
		return
	}
	orderType := c.DefaultQuery("order_type", "dine_in")
	isMember := c.Query("is_member") == "true"

	price, err := h.MenuSvc.CalculatePrice(uint(itemID), orderType, isMember)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"price": price})
}

func (h *MenuHandler) CreateOrder(c *gin.Context) {
	user := GetCurrentUser(c)
	orderType := c.PostForm("order_type")
	isMember := c.PostForm("is_member") == "true"

	itemIDs := c.PostFormArray("item_id")
	quantities := c.PostFormArray("quantity")

	if len(itemIDs) == 0 {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": "no items in order"})
		return
	}

	var orderItems []models.MenuOrderItem
	var totalPrice float64

	orgID, _ := c.Get("orgID")
	for i, idStr := range itemIDs {
		itemID, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": fmt.Sprintf("invalid item ID at position %d", i)})
			return
		}
		// Verify every ordered item belongs to the caller's org
		item, itemErr := h.MenuSvc.GetMenuItem(uint(itemID))
		if itemErr != nil || item.OrganizationID != orgID.(uint) {
			c.HTML(http.StatusForbidden, "error.html", gin.H{"error": fmt.Sprintf("item %d not available in your organization", itemID)})
			return
		}
		qty := 1
		if i < len(quantities) {
			qty, _ = strconv.Atoi(quantities[i])
			if qty < 1 {
				qty = 1
			}
		}

		price, err := h.MenuSvc.CalculatePrice(uint(itemID), orderType, isMember)
		if err != nil {
			c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": fmt.Sprintf("cannot price item %d: %s", itemID, err.Error())})
			return
		}

		orderItems = append(orderItems, models.MenuOrderItem{
			MenuItemID: uint(itemID), Quantity: qty, UnitPrice: price,
		})
		totalPrice += price * float64(qty)
	}

	if len(orderItems) == 0 {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": "order has no valid items"})
		return
	}

	order := &models.MenuOrder{
		OrganizationID: orgID.(uint),
		UserID: user.ID, OrderType: orderType,
		TotalPrice: totalPrice, IsMember: isMember, Status: "pending",
	}

	if err := h.MenuSvc.CreateOrder(order, orderItems); err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": err.Error()})
		return
	}

	c.Redirect(http.StatusFound, "/menu")
}

func (h *MenuHandler) AddChoice(c *gin.Context) {
	itemID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item ID"})
		return
	}
	if !h.verifyMenuItemOrg(c, uint(itemID)) {
		return
	}

	choiceType := c.PostForm("choice_type")
	validChoiceTypes := map[string]bool{"prep": true, "flavor": true, "size": true}
	if !validChoiceTypes[choiceType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "choice type must be prep, flavor, or size"})
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "choice name is required"})
		return
	}

	extraPrice, _ := strconv.ParseFloat(c.PostForm("extra_price"), 64)
	if extraPrice < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "extra price must be >= 0"})
		return
	}

	choice := &models.MenuItemChoice{
		MenuItemID: uint(itemID), ChoiceType: choiceType,
		Name: name, ExtraPrice: extraPrice,
	}
	user := GetCurrentUser(c)
	if err := h.MenuSvc.CreateChoice(choice, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create choice: " + err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/menu/manage")
}
