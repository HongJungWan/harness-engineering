package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	balanceinfra "github.com/HongJungWan/harness-engineering/internal/balance/infrastructure"
	"github.com/HongJungWan/harness-engineering/internal/config"
	"github.com/HongJungWan/harness-engineering/internal/order/application"
	orderinfra "github.com/HongJungWan/harness-engineering/internal/order/infrastructure"
	"github.com/HongJungWan/harness-engineering/internal/order/presentation"
	"github.com/HongJungWan/harness-engineering/internal/outbox"
	sharedinfra "github.com/HongJungWan/harness-engineering/internal/shared/infrastructure"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Database
	db, err := sqlx.Connect("mysql", cfg.Database.DSN())
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.Database.ConnMaxIdleTime)
	defer db.Close()

	logger.Info("database connected", "host", cfg.Database.Host, "db", cfg.Database.DBName)

	// Kafka Producer via facade factory (no direct sarama in main)
	eventProducer, rawProducer, err := sharedinfra.NewKafkaProducer(cfg.Kafka.Brokers, cfg.Kafka.ProducerRetries)
	if err != nil {
		logger.Error("failed to create kafka producer", "error", err)
		os.Exit(1)
	}
	defer rawProducer.Close()

	logger.Info("kafka producer created", "brokers", cfg.Kafka.Brokers)

	// Dependency Injection
	txManager := sharedinfra.NewTxManager(db)
	outboxRepo := outbox.NewMysqlOutboxRepository(db)
	idempotencyRepo := outbox.NewMysqlIdempotencyRepository(db)
	orderRepo := orderinfra.NewMysqlOrderRepository(db, outboxRepo)
	balanceRepo := balanceinfra.NewMysqlBalanceRepository(db, outboxRepo)

	placeOrderUC := application.NewPlaceOrderUseCase(txManager, orderRepo, balanceRepo, idempotencyRepo)
	cancelOrderUC := application.NewCancelOrderUseCase(txManager, orderRepo, balanceRepo)

	// HTTP Router
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)

	handler := presentation.NewHandler(placeOrderUC, cancelOrderUC, orderRepo, balanceRepo)
	handler.RegisterRoutes(r)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Outbox Relay Worker (uses EventProducer facade)
	relay := outbox.NewRelay(outboxRepo, eventProducer, outbox.RelayConfig{
		PollInterval: cfg.App.RelayPollInterval,
		BatchSize:    cfg.App.RelayBatchSize,
		MaxRetries:   cfg.App.RelayMaxRetries,
		BackoffBase:  cfg.App.RelayBackoffBase,
	}, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start relay workers
	for i := 0; i < cfg.App.RelayWorkerCount; i++ {
		go relay.Start(ctx)
	}

	// HTTP Server
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.App.Port),
		Handler: r,
	}

	go func() {
		logger.Info("http server starting", "port", cfg.App.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.App.GracefulTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}

	cancel()
	time.Sleep(500 * time.Millisecond)

	logger.Info("server stopped gracefully")
}
