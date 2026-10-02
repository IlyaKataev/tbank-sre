package app

import (
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
)

type Config struct {
	JWTSecret             string
	JWTAccessTTL          time.Duration
	JWTRefreshTTL         time.Duration
	OrderRateLimitMinutes int
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

	return r
}

func RunMigrations(pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	sourceDriver, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return err
	}

	dbDriver, err := pgmigrate.WithInstance(db, &pgmigrate.Config{})
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "marketplace", dbDriver)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}
