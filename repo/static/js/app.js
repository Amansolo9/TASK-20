// ===================== HTML ESCAPING =====================
// Prevents XSS by escaping user-sourced data before HTML insertion
function escapeHtml(str) {
    if (str === null || str === undefined) return '';
    const s = String(str);
    const div = document.createElement('div');
    div.appendChild(document.createTextNode(s));
    return div.innerHTML;
}

// ===================== CSRF TOKEN AUTO-INJECTION =====================
// Reads the csrf_token cookie and injects a hidden field into every POST form.
function getCSRFToken() {
    const match = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]*)/);
    return match ? decodeURIComponent(match[1]) : '';
}

document.addEventListener('DOMContentLoaded', function() {
    const token = getCSRFToken();
    if (!token) return;

    document.querySelectorAll('form[method="POST"], form[method="post"]').forEach(function(form) {
        // Skip if already has a csrf_token field
        if (form.querySelector('input[name="csrf_token"]')) return;
        const input = document.createElement('input');
        input.type = 'hidden';
        input.name = 'csrf_token';
        input.value = token;
        form.appendChild(input);
    });
});

// ===================== CLEAR DRAFTS ON LOGOUT =====================
// Clears sessionStorage (clinician drafts with patient data) when user logs out.
document.addEventListener('DOMContentLoaded', function() {
    const logoutLink = document.querySelector('a[href="/logout"]');
    if (logoutLink) {
        logoutLink.addEventListener('click', function() {
            sessionStorage.clear();
        });
    }
});

// ===================== FORM SUBMIT LOADING STATES =====================
// Auto-disable submit buttons and show loading text on all POST form submissions.
document.addEventListener('DOMContentLoaded', function() {
    document.querySelectorAll('form[method="POST"], form[method="post"]').forEach(function(form) {
        // Skip forms handled by custom JS (upload, booking)
        if (form.id === 'uploadForm' || form.id === 'bookingForm' || form.id === 'orderForm') return;

        form.addEventListener('submit', function() {
            const btn = form.querySelector('button[type="submit"]');
            if (btn && !btn.disabled) {
                btn.disabled = true;
                btn.dataset.originalText = btn.textContent;
                btn.textContent = 'Submitting...';
                // Re-enable after 5s as a safety net
                setTimeout(function() {
                    btn.disabled = false;
                    btn.textContent = btn.dataset.originalText || 'Submit';
                }, 5000);
            }
        });
    });
});

// ===================== FILE UPLOAD VALIDATION =====================
function validateFile(input) {
    const fileError = document.getElementById('fileError');
    const fileSuccess = document.getElementById('fileSuccess');
    const uploadBtn = document.getElementById('uploadBtn');

    fileError.style.display = 'none';
    fileSuccess.style.display = 'none';
    uploadBtn.disabled = true;

    if (!input.files || !input.files[0]) return;

    const file = input.files[0];
    const maxSize = 10 * 1024 * 1024; // 10MB
    const allowedTypes = ['application/pdf', 'image/jpeg', 'image/png', 'image/gif'];

    if (file.size > maxSize) {
        fileError.textContent = 'File exceeds 10MB limit. Please choose a smaller file.';
        fileError.style.display = 'block';
        input.value = '';
        return;
    }

    if (!allowedTypes.includes(file.type)) {
        fileError.textContent = 'Invalid file type. Only PDF, JPEG, PNG, and GIF files are accepted.';
        fileError.style.display = 'block';
        input.value = '';
        return;
    }

    fileSuccess.textContent = `File "${file.name}" (${(file.size / 1024).toFixed(1)} KB) ready to upload.`;
    fileSuccess.style.display = 'block';
    uploadBtn.disabled = false;
}

// Handle upload form submission
document.addEventListener('DOMContentLoaded', function() {
    const uploadForm = document.getElementById('uploadForm');
    if (uploadForm) {
        uploadForm.addEventListener('submit', function(e) {
            e.preventDefault();
            const formData = new FormData(this);
            const uploadBtn = document.getElementById('uploadBtn');
            const fileSuccess = document.getElementById('fileSuccess');
            const fileError = document.getElementById('fileError');

            uploadBtn.disabled = true;
            uploadBtn.textContent = 'Uploading...';

            // Include CSRF token in multipart upload via header
            const csrfToken = getCSRFToken();
            fetch('/health/upload', {
                method: 'POST',
                headers: csrfToken ? { 'X-CSRF-Token': csrfToken } : {},
                body: formData
            })
            .then(res => res.json())
            .then(data => {
                if (data.error) {
                    fileError.textContent = data.error;
                    fileError.style.display = 'block';
                    fileSuccess.style.display = 'none';
                } else {
                    fileSuccess.textContent = 'File uploaded successfully!';
                    fileSuccess.style.display = 'block';
                    fileError.style.display = 'none';
                    setTimeout(() => location.reload(), 1500);
                }
            })
            .catch(err => {
                fileError.textContent = 'Upload failed: ' + err.message;
                fileError.style.display = 'block';
            })
            .finally(() => {
                uploadBtn.disabled = false;
                uploadBtn.textContent = 'Upload';
            });
        });
    }
});

// ===================== DRAFT SAVE/RESTORE =====================
function saveDraft(formId) {
    const form = document.getElementById(formId);
    if (!form) return;

    const data = {};
    const inputs = form.querySelectorAll('input, textarea, select');
    inputs.forEach(input => {
        if (input.name && input.type !== 'hidden' && input.type !== 'submit') {
            data[input.name] = input.value;
        }
    });

    sessionStorage.setItem('draft_' + formId, JSON.stringify(data));
}

function restoreDraft(formId) {
    const saved = sessionStorage.getItem('draft_' + formId);
    if (!saved) return;

    try {
        const data = JSON.parse(saved);
        const form = document.getElementById(formId);
        if (!form) return;

        Object.keys(data).forEach(key => {
            const input = form.querySelector(`[name="${key}"]`);
            if (input && input.type !== 'hidden') {
                input.value = data[key];
            }
        });
    } catch (e) {
        console.error('Failed to restore draft:', e);
    }
}

function clearDraft(formId) {
    sessionStorage.removeItem('draft_' + formId);
    const form = document.getElementById(formId);
    if (form) form.reset();
}

// ===================== BOOKING CALENDAR GRID =====================
// Renders slots entirely from server response — no client-side slot generation.
let selectedSlotValue = null;

function loadCalendar() {
    const venueId = document.getElementById('venueSelect')?.value;
    const date = document.getElementById('bookingDate')?.value;
    const grid = document.getElementById('calendarGrid');
    const hiddenInput = document.getElementById('slotStartHidden');

    if (!venueId || !date || !grid) return;

    grid.innerHTML = '<p class="text-muted">Loading slots...</p>';
    selectedSlotValue = null;
    if (hiddenInput) hiddenInput.value = '';

    fetch(`/api/slots?venue_id=${venueId}&date=${date}`)
        .then(res => res.json())
        .then(data => {
            grid.innerHTML = '';

            if (!data.slots || data.slots.length === 0) {
                grid.innerHTML = '<p class="text-muted">No slots for this date.</p>';
                return;
            }

            // Render directly from server-provided slot data (single source of truth)
            data.slots.forEach(slot => {
                const d = new Date(slot.time);
                const available = slot.available;
                const label = d.toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' });

                const cell = document.createElement('div');
                cell.className = 'calendar-slot ' + (available ? 'slot-available' : 'slot-booked');
                cell.textContent = label;
                cell.title = available ? 'Available — click to select' : 'Already booked';

                if (available) {
                    const slotISO = slot.time; // Use server-provided ISO timestamp directly
                    cell.addEventListener('click', function() {
                        grid.querySelectorAll('.slot-selected').forEach(el => el.classList.remove('slot-selected'));
                        cell.classList.add('slot-selected');
                        selectedSlotValue = slotISO;
                        if (hiddenInput) hiddenInput.value = slotISO;
                        const warning = document.getElementById('conflictWarning');
                        if (warning) warning.style.display = 'none';
                    });
                }

                grid.appendChild(cell);
            });
        })
        .catch(() => {
            grid.innerHTML = '<p class="text-muted">Error loading slots.</p>';
        });
}

function loadSlots() { loadCalendar(); }

// ===================== PARTNER MATCHING =====================
function findPartners() {
    const skillRange = document.getElementById('skillRange')?.value || 2;
    const weightRange = document.getElementById('weightRange')?.value || 20;
    const style = document.getElementById('styleFilter')?.value || '';
    const results = document.getElementById('matchResults');

    if (!results) return;

    results.style.display = 'block';
    results.innerHTML = 'Searching...';

    fetch(`/api/match-partners?skill_range=${skillRange}&weight_range=${weightRange}&style=${style}`)
        .then(res => res.json())
        .then(data => {
            if (!data.matches || data.matches.length === 0) {
                results.innerHTML = 'No matching partners found. Try widening your criteria.';
                return;
            }
            let html = '<strong>Matching Partners:</strong><ul>';
            data.matches.forEach(m => {
                html += `<li><a href="#" onclick="selectPartner(${parseInt(m.user_id)}); return false;">${escapeHtml(m.full_name)}</a> - Skill: ${escapeHtml(m.skill_level)}, Weight: ${escapeHtml(m.weight_class)}lb, Style: ${escapeHtml(m.primary_style)}</li>`;
            });
            html += '</ul>';
            results.innerHTML = html;
        })
        .catch(() => {
            results.innerHTML = 'Error finding partners.';
        });
}

function selectPartner(userId) {
    const input = document.getElementById('partnerInput');
    if (input) input.value = userId;
}

// ===================== CONFLICT CHECK =====================
function checkBeforeSubmit() {
    const venueId = document.getElementById('venueSelect')?.value;
    const slotStart = document.getElementById('slotStartHidden')?.value || selectedSlotValue;
    const partnerId = document.getElementById('partnerInput')?.value;
    const warning = document.getElementById('conflictWarning');
    const form = document.getElementById('bookingForm');

    if (!venueId || !slotStart) return true;

    let url = `/api/check-conflicts?venue_id=${venueId}&slot_start=${encodeURIComponent(slotStart)}`;
    if (partnerId) url += `&partner_id=${partnerId}`;

    // Async conflict check — prevent default submit, then re-submit if clear
    if (warning) warning.style.display = 'none';

    fetch(url)
        .then(res => res.json())
        .then(data => {
            if (data.conflicts && data.conflicts.length > 0) {
                warning.innerHTML = '<strong>Conflicts detected:</strong><ul>' +
                    data.conflicts.map(c => `<li>${escapeHtml(c)}</li>`).join('') + '</ul>';
                warning.style.display = 'block';
            } else {
                // No conflicts — submit the form directly (bypass this handler)
                if (form) {
                    form._skipConflictCheck = true;
                    form.submit();
                }
            }
        })
        .catch(() => {
            // On error, allow submission
            if (form) {
                form._skipConflictCheck = true;
                form.submit();
            }
        });

    return false; // Always prevent default; async handler will submit if OK
}

// Attach to form submit to handle the skip flag
document.addEventListener('DOMContentLoaded', function() {
    const form = document.getElementById('bookingForm');
    if (form) {
        form.addEventListener('submit', function(e) {
            if (form._skipConflictCheck) {
                form._skipConflictCheck = false;
                return true; // Allow through
            }
            // Otherwise let checkBeforeSubmit handle it (called via onclick)
        });
    }
});

// ===================== DATE FORMATTING =====================
// Format date as MM/DD/YYYY hh:mm AM/PM per prompt spec
function formatTimestamp(isoString) {
    const d = new Date(isoString);
    if (isNaN(d.getTime())) return isoString;
    const mm = String(d.getMonth() + 1).padStart(2, '0');
    const dd = String(d.getDate()).padStart(2, '0');
    const yyyy = d.getFullYear();
    let hh = d.getHours();
    const min = String(d.getMinutes()).padStart(2, '0');
    const ampm = hh >= 12 ? 'PM' : 'AM';
    hh = hh % 12 || 12;
    return `${mm}/${dd}/${yyyy} ${String(hh).padStart(2, '0')}:${min} ${ampm}`;
}

// ===================== BOOKING AUDIT =====================
function showBookingAudit(bookingId) {
    const modal = document.getElementById('auditModal');
    const content = document.getElementById('auditContent');

    if (!modal || !content) return;

    content.innerHTML = 'Loading...';
    openModal('auditModal');

    fetch(`/bookings/${bookingId}/audit`)
        .then(res => res.json())
        .then(data => {
            if (!data.audits || data.audits.length === 0) {
                content.innerHTML = '<p>No audit records found.</p>';
                return;
            }
            let html = '<table class="table table-sm"><thead><tr><th>Time</th><th>From</th><th>To</th><th>By</th><th>Note</th></tr></thead><tbody>';
            data.audits.forEach(a => {
                const time = formatTimestamp(a.timestamp);
                html += `<tr><td>${escapeHtml(time)}</td><td>${escapeHtml(a.old_status || '-')}</td><td><span class="status-badge status-${escapeHtml(a.new_status)}">${escapeHtml(a.new_status)}</span></td><td>${escapeHtml(a.changed_by)}</td><td>${escapeHtml(a.note)}</td></tr>`;
            });
            html += '</tbody></table>';
            content.innerHTML = html;
        })
        .catch(() => {
            content.innerHTML = '<p>Error loading audit trail.</p>';
        });
}

// ===================== HISTORY MODAL =====================
function showHistory(table, recordId) {
    const modal = document.getElementById('historyModal');
    const content = document.getElementById('historyContent');

    if (!modal || !content) return;

    content.innerHTML = 'Loading...';
    openModal('historyModal');

    fetch(`/health/history?table=${table}&record_id=${recordId}`)
        .then(res => res.json())
        .then(data => {
            if (!data || data.length === 0) {
                content.innerHTML = '<p>No revision history.</p>';
                return;
            }
            let html = '<table class="table table-sm"><thead><tr><th>Timestamp</th><th>Action</th><th>Editor</th><th>Reason</th><th>Fingerprint</th></tr></thead><tbody>';
            data.forEach(log => {
                const time = formatTimestamp(log.timestamp);
                html += `<tr><td>${escapeHtml(time)}</td><td>${escapeHtml(log.action)}</td><td>${escapeHtml(log.editor_id)}</td><td>${escapeHtml(log.reason)}</td><td><code>${escapeHtml(log.fingerprint?.substring(0, 12))}...</code></td></tr>`;
            });
            html += '</tbody></table>';
            content.innerHTML = html;
        })
        .catch(() => {
            content.innerHTML = '<p>Error loading history.</p>';
        });
}

// ===================== MODAL HELPERS =====================
function openModal(modalId) {
    const modal = document.getElementById(modalId);
    if (!modal) return;
    modal.style.display = 'flex';
    modal.setAttribute('role', 'dialog');
    modal.setAttribute('aria-modal', 'true');
    // Focus first focusable element
    const focusable = modal.querySelector('button, input, select, textarea, a[href]');
    if (focusable) focusable.focus();
}

function closeModal(modalId) {
    const modal = document.getElementById(modalId);
    if (modal) {
        modal.style.display = 'none';
        modal.removeAttribute('aria-modal');
    }
}

// Close modals on backdrop click
document.addEventListener('click', function(e) {
    if (e.target.classList.contains('modal')) {
        e.target.style.display = 'none';
    }
});

// Close modals on Escape key
document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') {
        document.querySelectorAll('.modal').forEach(function(modal) {
            if (modal.style.display === 'flex') {
                modal.style.display = 'none';
            }
        });
    }
});

// ===================== MENU MANAGEMENT =====================
function showItemSettings(itemId) {
    const modal = document.getElementById('itemSettingsModal');
    if (!modal) return;

    // Update form actions
    const sellWindowForm = document.getElementById('sellWindowForm');
    const substituteForm = document.getElementById('substituteForm');
    const choiceForm = document.getElementById('choiceForm');

    if (sellWindowForm) sellWindowForm.action = `/menu/manage/item/${itemId}/sell-windows`;
    if (substituteForm) substituteForm.action = `/menu/manage/item/${itemId}/substitutes`;
    if (choiceForm) choiceForm.action = `/menu/manage/item/${itemId}/choices`;

    modal.style.display = 'flex';
}

function addSellWindowRow() {
    const container = document.getElementById('sellWindowRows');
    if (!container) return;

    const row = document.createElement('div');
    row.className = 'form-row sell-window-row';
    row.innerHTML = `
        <select name="day_of_week">
            <option value="0">Sunday</option>
            <option value="1">Monday</option>
            <option value="2">Tuesday</option>
            <option value="3">Wednesday</option>
            <option value="4">Thursday</option>
            <option value="5">Friday</option>
            <option value="6">Saturday</option>
        </select>
        <input type="time" name="open_time" value="06:30">
        <input type="time" name="close_time" value="14:00">
    `;
    container.appendChild(row);
}

// ===================== MENU ORDER =====================
let orderItems = [];

function addToOrder(itemId) {
    const qtyInput = document.getElementById('qty-' + itemId);
    const hiddenInput = document.getElementById('item-' + itemId);
    if (!qtyInput || !hiddenInput) return;

    hiddenInput.disabled = false;
    qtyInput.disabled = false;

    const qty = parseInt(qtyInput.value) || 1;

    // Fetch the real price from the API
    const orderType = document.getElementById('orderType')?.value || 'dine_in';
    const isMember = document.getElementById('isMember')?.checked ? 'true' : 'false';

    fetch(`/api/price?item_id=${itemId}&order_type=${orderType}&is_member=${isMember}`)
        .then(res => res.json())
        .then(data => {
            const price = data.price || 0;
            orderItems.push({ id: itemId, qty: qty, unitPrice: price, name: hiddenInput.closest('.menu-card')?.querySelector('h4')?.textContent || ('Item #' + itemId) });
            updateOrderSummary();
        })
        .catch(() => {
            alert('Failed to fetch price for this item. Please try again.');
        });
}

function updateOrderSummary() {
    const summary = document.getElementById('orderSummary');
    const itemsDiv = document.getElementById('orderItems');
    const totalSpan = document.getElementById('orderTotal');

    if (!summary || !itemsDiv) return;

    summary.style.display = 'block';

    let total = 0;
    itemsDiv.innerHTML = orderItems.map((item, i) => {
        const lineTotal = item.unitPrice * item.qty;
        total += lineTotal;
        return `<div style="display:flex;justify-content:space-between;align-items:center;padding:0.3rem 0;border-bottom:1px solid #eee;">
            <span>${escapeHtml(item.name)} x${item.qty}</span>
            <span>
                <strong>$${lineTotal.toFixed(2)}</strong>
                <button type="button" class="btn btn-sm btn-outline" onclick="removeFromOrder(${i})" style="margin-left:0.5rem;">Remove</button>
            </span>
        </div>`;
    }).join('');

    if (totalSpan) totalSpan.textContent = total.toFixed(2);
}

function removeFromOrder(index) {
    orderItems.splice(index, 1);
    updateOrderSummary();
    if (orderItems.length === 0) {
        document.getElementById('orderSummary').style.display = 'none';
        const totalSpan = document.getElementById('orderTotal');
        if (totalSpan) totalSpan.textContent = '0.00';
    }
}

function recalculateAll() {
    if (orderItems.length === 0) return;

    const orderType = document.getElementById('orderType')?.value || 'dine_in';
    const isMember = document.getElementById('isMember')?.checked ? 'true' : 'false';
    const submitBtn = document.querySelector('#orderForm button[type="submit"]');
    const totalSpan = document.getElementById('orderTotal');

    // Show loading state
    if (submitBtn) { submitBtn.disabled = true; submitBtn.textContent = 'Recalculating...'; }
    if (totalSpan) totalSpan.textContent = '...';

    const promises = orderItems.map((item, i) =>
        fetch(`/api/price?item_id=${item.id}&order_type=${orderType}&is_member=${isMember}`)
            .then(res => res.json())
            .then(data => { orderItems[i].unitPrice = data.price || 0; })
            .catch(() => {})
    );

    Promise.all(promises).then(() => {
        updateOrderSummary();
        if (submitBtn) { submitBtn.disabled = false; submitBtn.textContent = 'Submit Order'; }
    });
}
