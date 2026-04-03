package repository

import (
	"context"
	"time"

	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
)

// Database for bills service
var billsDB = sqldb.NewDatabase("bills", sqldb.DatabaseConfig{
	Migrations: "./migrations",
})

// Bill represents a bill in any state (open or closed)
type Bill struct {
	BillID       string
	CurrencyCode string
	Status       string
	TotalMinor   int64
	ClosedAt     *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LineItems    []LineItem
}

// LineItem represents a single charge on a bill
type LineItem struct {
	ID          string
	AmountMinor int64
	Description string
	CreatedAt   time.Time
}

// Repository handles all database operations for bills
type Repository struct {
	db *sqldb.Database
}

// NewRepository creates a new repository instance using the package-level database
func NewRepository() *Repository {
	return &Repository{db: billsDB}
}

// StoreBill saves a bill to the database with an auto-generated ID
func (r *Repository) StoreBill(ctx context.Context, currencyCode string) (string, error) {
	billID := uuid.New().String()
	_, err := r.db.Exec(ctx, `
		INSERT INTO bills (id, currency_code, status)
		VALUES ($1, $2, $3)
	`, billID, currencyCode, "OPEN")
	return billID, err
}

// AddLineItem adds a line item to a bill and updates the bill's running total.
// Uses INSERT ... ON CONFLICT to ensure idempotency — only increments the total
// when a new row is actually inserted.
func (r *Repository) AddLineItem(ctx context.Context, billID string, amountMinor int64, currencyCode string, description string, idempotencyKey string) (string, error) {
	itemID := uuid.New().String()

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// Use a CTE to detect whether the row was actually inserted
	var insertedID *string
	err = tx.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO bill_line_items (id, bill_id, amount_minor, currency_code, description, idempotency_key)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (bill_id, idempotency_key) DO NOTHING
			RETURNING id
		)
		SELECT id FROM ins
		UNION ALL
		SELECT id FROM bill_line_items WHERE bill_id = $2 AND idempotency_key = $6
		LIMIT 1
	`, itemID, billID, amountMinor, currencyCode, description, idempotencyKey).Scan(&insertedID)
	if err != nil {
		return "", err
	}

	// Only update the running total if we actually inserted a new row
	if insertedID != nil && *insertedID == itemID {
		_, err = tx.Exec(ctx, `
			UPDATE bills
			SET running_total_minor = running_total_minor + $1, updated_at = NOW()
			WHERE id = $2
		`, amountMinor, billID)
		if err != nil {
			return "", err
		}
	} else if insertedID != nil {
		// Duplicate — return the existing item ID
		itemID = *insertedID
	}

	err = tx.Commit()
	return itemID, err
}

// CloseBill marks a bill as closed and records the final total
func (r *Repository) CloseBill(ctx context.Context, billID string, currencyCode string, lineItems []LineItem) (Bill, error) {
	var totalMinor int64
	for _, item := range lineItems {
		totalMinor += item.AmountMinor
	}

	var closedAt time.Time
	err := r.db.QueryRow(ctx, `
		UPDATE bills
		SET status = $1, closed_at = NOW(), closed_total_minor = $2, updated_at = NOW()
		WHERE id = $3
		RETURNING closed_at
	`, "CLOSED", totalMinor, billID).Scan(&closedAt)
	if err != nil {
		return Bill{}, err
	}

	return Bill{
		BillID:       billID,
		CurrencyCode: currencyCode,
		Status:       "CLOSED",
		TotalMinor:   totalMinor,
		ClosedAt:     &closedAt,
		LineItems:    lineItems,
	}, nil
}

// GetBill retrieves a bill with its line items
func (r *Repository) GetBill(ctx context.Context, billID string) (Bill, error) {
	var bill Bill
	err := r.db.QueryRow(ctx, `
		SELECT id, currency_code, status,
			CASE WHEN status = 'CLOSED' THEN closed_total_minor ELSE running_total_minor END,
			closed_at, created_at, updated_at
		FROM bills
		WHERE id = $1
	`, billID).Scan(&bill.BillID, &bill.CurrencyCode, &bill.Status, &bill.TotalMinor, &bill.ClosedAt, &bill.CreatedAt, &bill.UpdatedAt)
	if err != nil {
		return Bill{}, err
	}

	rows, err := r.db.Query(ctx, `
		SELECT id, amount_minor, description, created_at
		FROM bill_line_items
		WHERE bill_id = $1
		ORDER BY created_at
	`, billID)
	if err != nil {
		return Bill{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var item LineItem
		if err := rows.Scan(&item.ID, &item.AmountMinor, &item.Description, &item.CreatedAt); err != nil {
			return Bill{}, err
		}
		bill.LineItems = append(bill.LineItems, item)
	}

	return bill, rows.Err()
}

// ListBills retrieves bills, optionally filtered by status
func (r *Repository) ListBills(ctx context.Context, status string) ([]Bill, error) {
	var rows *sqldb.Rows
	var err error

	if status != "" {
		rows, err = r.db.Query(ctx, `
			SELECT id, currency_code, status,
				CASE WHEN status = 'CLOSED' THEN closed_total_minor ELSE running_total_minor END,
				closed_at, created_at, updated_at
			FROM bills
			WHERE status = $1
			ORDER BY created_at DESC
		`, status)
	} else {
		rows, err = r.db.Query(ctx, `
			SELECT id, currency_code, status,
				CASE WHEN status = 'CLOSED' THEN closed_total_minor ELSE running_total_minor END,
				closed_at, created_at, updated_at
			FROM bills
			ORDER BY created_at DESC
		`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bills []Bill
	for rows.Next() {
		var b Bill
		if err := rows.Scan(&b.BillID, &b.CurrencyCode, &b.Status, &b.TotalMinor, &b.ClosedAt, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		bills = append(bills, b)
	}

	return bills, rows.Err()
}
