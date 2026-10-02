package app

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/golang-migrate/migrate/v4"
	pgmigrate "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"marketplace/internal/api"
	sqlcdb "marketplace/internal/db/sqlc"
	"marketplace/internal/handler"
	mw "marketplace/internal/middleware"
	"marketplace/internal/migrations"
	"marketplace/internal/service"
	"marketplace/web"
)

type Config struct {
	JWTSecret             string
	JWTAccessTTL          time.Duration
	JWTRefreshTTL         time.Duration
	OrderRateLimitMinutes int
	ReadyTimeout          time.Duration
	IsShuttingDown        func() bool
}

func NewRouter(pool *pgxpool.Pool, cfg Config) http.Handler {
	queries := sqlcdb.New(pool)

	authSvc := service.NewAuthService(queries, cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	productSvc := service.NewProductService(queries)
	orderSvc := service.NewOrderService(queries, pool, cfg.OrderRateLimitMinutes)
	promoSvc := service.NewPromoService(queries)

	h := handler.New(authSvc, productSvc, orderSvc, promoSvc)

	strictOpts := api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  handler.RequestErrorHandler,
		ResponseErrorHandlerFunc: handler.ResponseErrorHandler,
	}
	strictHandler := api.NewStrictHandlerWithOptions(h, nil, strictOpts)

	wrapper := &api.ServerInterfaceWrapper{
		Handler: strictHandler,
		ErrorHandlerFunc: func(w http.ResponseWriter, req *http.Request, err error) {
			handler.RequestErrorHandler(w, req, err)
		},
	}

	r := chi.NewRouter()
	r.Use(chimw.Recoverer)
	r.Use(chimw.AllowContentType("application/json"))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			if req.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, req)
		})
	})
	r.Use(mw.Logger)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	r.Get("/readyz", readyHandler(pool.Ping, cfg.ReadyTimeout, cfg.IsShuttingDown))

	// Public auth routes, no JWT required
	r.Post("/auth/register", wrapper.RegisterUser)
	r.Post("/auth/login", wrapper.LoginUser)
	r.Post("/auth/refresh", wrapper.RefreshToken)

	// All other routes, JWT required
	r.Group(func(r chi.Router) {
		r.Use(mw.Auth(cfg.JWTSecret))
		r.Get("/products", wrapper.ListProducts)
		r.Post("/products", wrapper.CreateProduct)
		r.Get("/products/{id}", wrapper.GetProduct)
		r.Put("/products/{id}", wrapper.UpdateProduct)
		r.Delete("/products/{id}", wrapper.DeleteProduct)
		r.Post("/orders", wrapper.CreateOrder)
		r.Get("/orders/{id}", wrapper.GetOrder)
		r.Put("/orders/{id}", wrapper.UpdateOrder)
		r.Post("/orders/{id}/cancel", wrapper.CancelOrder)
		r.Post("/promo-codes", wrapper.CreatePromoCode)
	})
	r.Handle("/*", web.Handler())

	return r
}

func readyHandler(ping func(context.Context) error, timeout time.Duration, shuttingDown func() bool) http.HandlerFunc {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if shuttingDown != nil && shuttingDown() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("{\"status\":\"draining\"}\n"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if err := ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("{\"status\":\"unavailable\"}\n"))
			return
		}
		_, _ = w.Write([]byte("{\"status\":\"ready\"}\n"))
	}
}

func RunMigrations(pool *pgxpool.Pool) error {
	// Migration metadata and advisory locks also use background contexts inside
	// the driver. Server-side timeouts bound those operations, including driver
	// initialization before migrate.LockTimeout is available. A dedicated
	// connection keeps these session settings out of the application's pool.
	migrationConfig := pool.Config().ConnConfig.Copy()
	if migrationConfig.RuntimeParams == nil {
		migrationConfig.RuntimeParams = make(map[string]string)
	}
	migrationConfig.RuntimeParams["lock_timeout"] = "30s"
	migrationConfig.RuntimeParams["statement_timeout"] = "60s"
	db := stdlib.OpenDB(*migrationConfig)
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	sourceDriver, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return err
	}

	dbDriver, err := pgmigrate.WithConnection(ctx, conn, &pgmigrate.Config{StatementTimeout: 60 * time.Second})
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "marketplace", dbDriver)
	if err != nil {
		return err
	}
	defer m.Close()
	m.LockTimeout = 35 * time.Second

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}
