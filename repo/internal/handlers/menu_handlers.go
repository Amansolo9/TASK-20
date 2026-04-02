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

	"github.com/gin-gonic/gin"
)

type MenuHandler struct {
	MenuSvc *services.MenuService
}

func NewMenuHandler(menuSvc *services.MenuService) *MenuHandler {
	return &MenuHandler{MenuSvc: menuSvc}
}

var timeFormatRE = regexp.MustCompile(`^\d{2}:\d{2}$`)

func (h *MenuHandler) MenuPage(c *gin.Context) {
	user := GetCurrentUser(c)
	categories, _ := h.MenuSvc.GetCategories()

	var catID *uint
	if cid := c.Query("category_id"); cid != "" {
		if id, err := strconv.ParseUint(cid, 10, 64); err == nil {
			uid := uint(id)
			catID = &uid
		}
	}

	items, _ := h.MenuSvc.GetMenuItems(catID)

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

	c.HTML(http.StatusOK, "menu.html", gin.H{
		"title":      "Dining Menu",
		"user":       user,
		"categories": categories,
		"items":      enriched,
		"activeCat":  catID,
	})
}

func (h *MenuHandler) MenuManagePage(c *gin.Context) {
	user := GetCurrentUser(c)
	categories, _ := h.MenuSvc.GetCategories()
	items, _ := h.MenuSvc.GetMenuItems(nil)
	blackouts, _ := h.MenuSvc.GetBlackouts()

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

	var parentID *uint
	if pid := c.PostForm("parent_id"); pid != "" {
		if id, err := strconv.ParseUint(pid, 10, 64); err == nil {
			uid := uint(id)
			parentID = &uid
		}
	}
	sortOrder, _ := strconv.Atoi(c.PostForm("sort_order"))

	cat := &models.MenuCategory{Name: name, ParentID: parentID, SortOrder: sortOrder}
	if err := h.MenuSvc.CreateCategory(cat); err != nil {
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

	item := &models.MenuItem{
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

	if err := h.MenuSvc.SetSellWindows(uint(id), windows); err != nil {
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
	subIDs := c.PostForm("substitute_ids")

	var ids []uint
	for _, s := range strings.Split(subIDs, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if sid, err := strconv.ParseUint(s, 10, 64); err == nil {
			ids = append(ids, uint(sid))
		}
	}

	if err := h.MenuSvc.SetSubstitutes(uint(id), ids); err != nil {
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

	if err := h.MenuSvc.CreateBlackout(&models.HolidayBlackout{Date: date, Description: desc}); err != nil {
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
	if err := h.MenuSvc.DeleteBlackout(uint(id)); err != nil {
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

	if err := h.MenuSvc.CreatePromotion(&models.Promotion{
		MenuItemID: uint(itemID), DiscountPct: discount,
		StartsAt: startsAt, EndsAt: endsAt, Active: true,
	}); err != nil {
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

	for i, idStr := range itemIDs {
		itemID, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			continue
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
			continue
		}

		orderItems = append(orderItems, models.MenuOrderItem{
			MenuItemID: uint(itemID), Quantity: qty, UnitPrice: price,
		})
		totalPrice += price * float64(qty)
	}

	order := &models.MenuOrder{
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
	if err := h.MenuSvc.CreateChoice(choice); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create choice: " + err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/menu/manage")
}
