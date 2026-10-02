package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"marketplace/internal/app"
	dbpkg "marketplace/internal/db"
)

var testSrv *httptest.Server

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgCtr, err := tcpostgres.Run(ctx,
		"postgres:18-alpine",
		tcpostgres.WithDatabase("marketplace"),
		tcpostgres.WithUsername("marketplace"),
		tcpostgres.WithPassword("marketplace"),
		tcpostgres.WithSQLDriver("pgx"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres container: %v\n", err)
		os.Exit(1)
	}

	dsn, err := pgCtr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get connection string: %v\n", err)
		_ = pgCtr.Terminate(ctx)
		os.Exit(1)
	}

	pool, err := dbpkg.NewPool(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create pool: %v\n", err)
		_ = pgCtr.Terminate(ctx)
		os.Exit(1)
	}

	if err := app.RunMigrations(pool); err != nil {
		fmt.Fprintf(os.Stderr, "run migrations: %v\n", err)
		pool.Close()
		_ = pgCtr.Terminate(ctx)
		os.Exit(1)
	}

	cfg := app.Config{
		JWTSecret:             "e2e-test-secret",
		JWTAccessTTL:          15 * time.Minute,
		JWTRefreshTTL:         168 * time.Hour,
		OrderRateLimitMinutes: 1,
	}

	testSrv = httptest.NewServer(app.NewRouter(pool, cfg))

	code := m.Run()

	testSrv.Close()

	closed := make(chan struct{})
	go func() { pool.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
	}
	_ = pgCtr.Terminate(ctx)
	os.Exit(code)
}

func do(t *testing.T, method, path string, body any, token string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req, err := http.NewRequest(method, testSrv.URL+path, &buf)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := testSrv.Client().Do(req)
	require.NoError(t, err)
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	require.NoError(t, json.NewDecoder(resp.Body).Decode(dst))
}

type authResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// register creates a user and returns tokens. Email must be unique per test.
func register(t *testing.T, email, password, role string) authResp {
	t.Helper()
	body := map[string]string{"email": email, "password": password, "role": role}
	resp := do(t, http.MethodPost, "/auth/register", body, "")
	require.Equal(t, http.StatusCreated, resp.StatusCode, "register %s", email)
	var ar authResp
	decodeJSON(t, resp, &ar)
	require.NotEmpty(t, ar.AccessToken)
	return ar
}

func TestAuth_RegisterAndLogin(t *testing.T) {
	tokens := register(t, "auth1@example.com", "password123", "USER")
	assert.NotEmpty(t, tokens.AccessToken)
	assert.NotEmpty(t, tokens.RefreshToken)

	resp := do(t, http.MethodPost, "/auth/login",
		map[string]string{"email": "auth1@example.com", "password": "password123"}, "")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var loginResp authResp
	decodeJSON(t, resp, &loginResp)
	assert.NotEmpty(t, loginResp.AccessToken)

	resp = do(t, http.MethodPost, "/auth/login",
		map[string]string{"email": "auth1@example.com", "password": "wrong"}, "")
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp.Body.Close()
}

func TestAuth_DuplicateEmail(t *testing.T) {
	register(t, "dup@example.com", "password123", "USER")

	resp := do(t, http.MethodPost, "/auth/register",
		map[string]string{"email": "dup@example.com", "password": "password123", "role": "USER"}, "")
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	var body map[string]any
	decodeJSON(t, resp, &body)
	assert.Equal(t, "USER_ALREADY_EXISTS", body["error_code"])
}

func TestAuth_RefreshToken(t *testing.T) {
	tokens := register(t, "refresh@example.com", "password123", "USER")

	resp := do(t, http.MethodPost, "/auth/refresh",
		map[string]string{"refresh_token": tokens.RefreshToken}, "")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var newTokens authResp
	decodeJSON(t, resp, &newTokens)
	assert.NotEmpty(t, newTokens.AccessToken)
}

func TestAuth_NoToken_Returns401(t *testing.T) {
	resp := do(t, http.MethodGet, "/products", nil, "")
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	var body map[string]any
	decodeJSON(t, resp, &body)
	assert.Equal(t, "TOKEN_INVALID", body["error_code"])
}

type productResp struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Stock    int     `json:"stock"`
	Category string  `json:"category"`
	Status   string  `json:"status"`
}

func createProduct(t *testing.T, sellerToken string, price float64, stock int) string {
	t.Helper()
	resp := do(t, http.MethodPost, "/products",
		map[string]any{
			"name":     "Product-" + t.Name(),
			"price":    price,
			"stock":    stock,
			"category": "General",
		}, sellerToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var p productResp
	decodeJSON(t, resp, &p)
	return p.ID
}

func TestProducts_CRUD(t *testing.T) {
	sellerTokens := register(t, "seller-crud@example.com", "password123", "SELLER")
	userTokens := register(t, "user-crud@example.com", "password123", "USER")

	resp := do(t, http.MethodPost, "/products",
		map[string]any{"name": "Gadget", "price": 99.99, "stock": 10, "category": "Electronics"},
		userTokens.AccessToken)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	resp.Body.Close()

	resp = do(t, http.MethodPost, "/products",
		map[string]any{"name": "Laptop", "price": 1299.99, "stock": 5, "category": "Electronics"},
		sellerTokens.AccessToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var created productResp
	decodeJSON(t, resp, &created)
	assert.Equal(t, "Laptop", created.Name)
	assert.Equal(t, 5, created.Stock)
	assert.Equal(t, "ACTIVE", created.Status)

	productID := created.ID

	resp = do(t, http.MethodGet, "/products", nil, userTokens.AccessToken)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var list struct {
		Items         []productResp `json:"items"`
		TotalElements int64         `json:"totalElements"`
	}
	decodeJSON(t, resp, &list)
	assert.GreaterOrEqual(t, list.TotalElements, int64(1))

	resp = do(t, http.MethodGet, "/products/"+productID, nil, userTokens.AccessToken)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var fetched productResp
	decodeJSON(t, resp, &fetched)
	assert.Equal(t, productID, fetched.ID)

	resp = do(t, http.MethodPut, "/products/"+productID,
		map[string]any{"name": "Laptop Pro", "price": 1499.99, "stock": 3, "category": "Electronics", "status": "ACTIVE"},
		sellerTokens.AccessToken)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var updated productResp
	decodeJSON(t, resp, &updated)
	assert.Equal(t, "Laptop Pro", updated.Name)
	assert.Equal(t, 3, updated.Stock)

	resp = do(t, http.MethodDelete, "/products/"+productID, nil, sellerTokens.AccessToken)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	resp = do(t, http.MethodGet, "/products/"+productID, nil, userTokens.AccessToken)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var archived productResp
	decodeJSON(t, resp, &archived)
	assert.Equal(t, "ARCHIVED", archived.Status)
}

func TestProducts_NotFound(t *testing.T) {
	tokens := register(t, "user-nf@example.com", "password123", "USER")
	resp := do(t, http.MethodGet, "/products/00000000-0000-0000-0000-000000000000", nil, tokens.AccessToken)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	var body map[string]any
	decodeJSON(t, resp, &body)
	assert.Equal(t, "PRODUCT_NOT_FOUND", body["error_code"])
}

type orderResp struct {
	ID             string  `json:"id"`
	Status         string  `json:"status"`
	TotalAmount    float64 `json:"total_amount"`
	DiscountAmount float64 `json:"discount_amount"`
	Items          []struct {
		ProductID    string  `json:"product_id"`
		Quantity     int     `json:"quantity"`
		PriceAtOrder float64 `json:"price_at_order"`
	} `json:"items"`
}

func TestOrders_CreateHappyPath(t *testing.T) {
	sellerTokens := register(t, "seller-ord1@example.com", "password123", "SELLER")
	userTokens := register(t, "user-ord1@example.com", "password123", "USER")
	productID := createProduct(t, sellerTokens.AccessToken, 100.0, 10)

	resp := do(t, http.MethodPost, "/orders",
		map[string]any{
			"items": []map[string]any{
				{"product_id": productID, "quantity": 2},
			},
		}, userTokens.AccessToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var order orderResp
	decodeJSON(t, resp, &order)
	assert.Equal(t, "CREATED", order.Status)
	assert.InDelta(t, 200.0, order.TotalAmount, 0.01)
	assert.Equal(t, 0.0, order.DiscountAmount)
	require.Len(t, order.Items, 1)
	assert.Equal(t, 100.0, order.Items[0].PriceAtOrder)
}

func TestOrders_InsufficientStock(t *testing.T) {
	sellerTokens := register(t, "seller-stock@example.com", "password123", "SELLER")
	userTokens := register(t, "user-stock@example.com", "password123", "USER")
	productID := createProduct(t, sellerTokens.AccessToken, 50.0, 3)

	resp := do(t, http.MethodPost, "/orders",
		map[string]any{
			"items": []map[string]any{
				{"product_id": productID, "quantity": 100},
			},
		}, userTokens.AccessToken)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	var body map[string]any
	decodeJSON(t, resp, &body)
	assert.Equal(t, "INSUFFICIENT_STOCK", body["error_code"])
}

func TestOrders_RateLimit(t *testing.T) {
	sellerTokens := register(t, "seller-rl@example.com", "password123", "SELLER")
	userTokens := register(t, "user-rl@example.com", "password123", "USER")
	productID := createProduct(t, sellerTokens.AccessToken, 10.0, 20)

	// First order — succeeds
	resp := do(t, http.MethodPost, "/orders",
		map[string]any{
			"items": []map[string]any{{"product_id": productID, "quantity": 1}},
		}, userTokens.AccessToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	// Second order within the same minute — rate limited
	resp = do(t, http.MethodPost, "/orders",
		map[string]any{
			"items": []map[string]any{{"product_id": productID, "quantity": 1}},
		}, userTokens.AccessToken)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	var body map[string]any
	decodeJSON(t, resp, &body)
	assert.Equal(t, "ORDER_LIMIT_EXCEEDED", body["error_code"])
}

func TestOrders_GetAndCancel(t *testing.T) {
	sellerTokens := register(t, "seller-cancel@example.com", "password123", "SELLER")
	userTokens := register(t, "user-cancel@example.com", "password123", "USER")
	productID := createProduct(t, sellerTokens.AccessToken, 75.0, 5)

	resp := do(t, http.MethodPost, "/orders",
		map[string]any{
			"items": []map[string]any{{"product_id": productID, "quantity": 2}},
		}, userTokens.AccessToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var order orderResp
	decodeJSON(t, resp, &order)

	resp = do(t, http.MethodGet, "/orders/"+order.ID, nil, userTokens.AccessToken)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var fetched orderResp
	decodeJSON(t, resp, &fetched)
	assert.Equal(t, order.ID, fetched.ID)

	resp = do(t, http.MethodPost, "/orders/"+order.ID+"/cancel", nil, userTokens.AccessToken)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var cancelled orderResp
	decodeJSON(t, resp, &cancelled)
	assert.Equal(t, "CANCELED", cancelled.Status)

	resp = do(t, http.MethodGet, "/products/"+productID, nil, userTokens.AccessToken)
	var product productResp
	decodeJSON(t, resp, &product)
	assert.Equal(t, 5, product.Stock)
}

func TestOrders_OwnershipViolation(t *testing.T) {
	sellerTokens := register(t, "seller-own@example.com", "password123", "SELLER")
	user1 := register(t, "user-own1@example.com", "password123", "USER")
	user2 := register(t, "user-own2@example.com", "password123", "USER")
	productID := createProduct(t, sellerTokens.AccessToken, 10.0, 20)

	resp := do(t, http.MethodPost, "/orders",
		map[string]any{
			"items": []map[string]any{{"product_id": productID, "quantity": 1}},
		}, user1.AccessToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var order orderResp
	decodeJSON(t, resp, &order)

	// user2 tries to cancel user1's order → 403
	resp = do(t, http.MethodPost, "/orders/"+order.ID+"/cancel", nil, user2.AccessToken)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	resp.Body.Close()
}

func TestPromoCode_PercentageDiscount(t *testing.T) {
	sellerTokens := register(t, "seller-promo@example.com", "password123", "SELLER")
	userTokens := register(t, "user-promo@example.com", "password123", "USER")

	resp := do(t, http.MethodPost, "/promo-codes",
		map[string]any{
			"code":             "SAVE20",
			"discount_type":    "PERCENTAGE",
			"discount_value":   20,
			"min_order_amount": 0,
			"max_uses":         100,
			"valid_from":       time.Now().Add(-time.Hour).Format(time.RFC3339),
			"valid_until":      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		}, sellerTokens.AccessToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	resp.Body.Close()

	productID := createProduct(t, sellerTokens.AccessToken, 100.0, 10)

	resp = do(t, http.MethodPost, "/orders",
		map[string]any{
			"items":      []map[string]any{{"product_id": productID, "quantity": 1}},
			"promo_code": "SAVE20",
		}, userTokens.AccessToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var order orderResp
	decodeJSON(t, resp, &order)
	// total_amount is stored as NET (after discount): 100 - 20% = 80
	assert.InDelta(t, 80.0, order.TotalAmount, 0.01)
	assert.InDelta(t, 20.0, order.DiscountAmount, 0.01)
}

func TestPromoCode_InvalidCode(t *testing.T) {
	sellerTokens := register(t, "seller-inv@example.com", "password123", "SELLER")
	userTokens := register(t, "user-inv@example.com", "password123", "USER")
	productID := createProduct(t, sellerTokens.AccessToken, 50.0, 5)

	resp := do(t, http.MethodPost, "/orders",
		map[string]any{
			"items":      []map[string]any{{"product_id": productID, "quantity": 1}},
			"promo_code": "NOSUCHCODE",
		}, userTokens.AccessToken)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	var body map[string]any
	decodeJSON(t, resp, &body)
	assert.Equal(t, "PROMO_CODE_INVALID", body["error_code"])
}

func TestOrder_CannotCancelTwice(t *testing.T) {
	sellerTokens := register(t, "seller-sm@example.com", "password123", "SELLER")
	userTokens := register(t, "user-sm@example.com", "password123", "USER")
	productID := createProduct(t, sellerTokens.AccessToken, 30.0, 10)

	resp := do(t, http.MethodPost, "/orders",
		map[string]any{
			"items": []map[string]any{{"product_id": productID, "quantity": 1}},
		}, userTokens.AccessToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var order orderResp
	decodeJSON(t, resp, &order)

	// First cancel → OK (CREATED → CANCELED)
	resp = do(t, http.MethodPost, "/orders/"+order.ID+"/cancel", nil, userTokens.AccessToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Second cancel → 409 INVALID_STATE_TRANSITION (no exit from CANCELED)
	resp = do(t, http.MethodPost, "/orders/"+order.ID+"/cancel", nil, userTokens.AccessToken)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	var body map[string]any
	decodeJSON(t, resp, &body)
	assert.Equal(t, "INVALID_STATE_TRANSITION", body["error_code"])
}

func TestRBAC_UserCannotCreatePromoCode(t *testing.T) {
	userTokens := register(t, "user-rbac1@example.com", "password123", "USER")
	resp := do(t, http.MethodPost, "/promo-codes",
		map[string]any{
			"code":           "HACK",
			"discount_type":  "FIXED_AMOUNT",
			"discount_value": 10,
			"max_uses":       1,
			"valid_from":     time.Now().Add(-time.Hour).Format(time.RFC3339),
			"valid_until":    time.Now().Add(time.Hour).Format(time.RFC3339),
		}, userTokens.AccessToken)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	resp.Body.Close()
}

func TestRBAC_SellerCannotModifyOtherSellerProduct(t *testing.T) {
	seller1 := register(t, "seller-rbac1@example.com", "password123", "SELLER")
	seller2 := register(t, "seller-rbac2@example.com", "password123", "SELLER")
	productID := createProduct(t, seller1.AccessToken, 20.0, 5)

	resp := do(t, http.MethodPut, "/products/"+productID,
		map[string]any{
			"name": "Hijacked", "price": 1.0, "stock": 100, "category": "X", "status": "ACTIVE",
		}, seller2.AccessToken)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	resp.Body.Close()
}

func TestRBAC_AdminCanModifyAnyProduct(t *testing.T) {
	sellerTokens := register(t, "seller-rbac3@example.com", "password123", "SELLER")
	adminTokens := register(t, "admin-rbac@example.com", "password123", "ADMIN")
	productID := createProduct(t, sellerTokens.AccessToken, 50.0, 10)

	resp := do(t, http.MethodPut, "/products/"+productID,
		map[string]any{
			"name": "Admin Updated", "price": 99.0, "stock": 7, "category": "General", "status": "ACTIVE",
		}, adminTokens.AccessToken)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var p productResp
	decodeJSON(t, resp, &p)
	assert.Equal(t, "Admin Updated", p.Name)
}
