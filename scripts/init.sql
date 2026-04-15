CREATE DATABASE IF NOT EXISTS harness
  CHARACTER SET utf8mb4
  COLLATE utf8mb4_unicode_ci;

USE harness;

-- orders
CREATE TABLE IF NOT EXISTS orders (
    id            BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    order_uid     CHAR(36)         NOT NULL COMMENT 'UUIDv7 external reference',
    user_id       BIGINT UNSIGNED  NOT NULL,
    pair          VARCHAR(20)      NOT NULL COMMENT 'e.g. BTC/KRW',
    side          ENUM('BUY','SELL') NOT NULL,
    order_type    ENUM('LIMIT','MARKET') NOT NULL,
    price         DECIMAL(30,8)    NOT NULL DEFAULT 0,
    quantity      DECIMAL(30,8)    NOT NULL,
    filled_qty    DECIMAL(30,8)    NOT NULL DEFAULT 0,
    status        ENUM('PENDING','ACCEPTED','PARTIALLY_FILLED','FILLED','CANCELLED','REJECTED') NOT NULL DEFAULT 'PENDING',
    reason        VARCHAR(256)     NULL,
    version       BIGINT UNSIGNED  NOT NULL DEFAULT 1,
    created_at    DATETIME(6)      NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    DATETIME(6)      NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),

    PRIMARY KEY (id),
    UNIQUE KEY uk_order_uid (order_uid),
    INDEX idx_user_status (user_id, status),
    INDEX idx_pair_side_status (pair, side, status),
    INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- balances
CREATE TABLE IF NOT EXISTS balances (
    id            BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    user_id       BIGINT UNSIGNED  NOT NULL,
    currency      VARCHAR(10)      NOT NULL COMMENT 'e.g. BTC, KRW',
    available     DECIMAL(30,8)    NOT NULL DEFAULT 0,
    locked        DECIMAL(30,8)    NOT NULL DEFAULT 0,
    version       BIGINT UNSIGNED  NOT NULL DEFAULT 1,
    updated_at    DATETIME(6)      NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),

    PRIMARY KEY (id),
    UNIQUE KEY uk_user_currency (user_id, currency),
    CONSTRAINT chk_available_non_negative CHECK (available >= 0),
    CONSTRAINT chk_locked_non_negative CHECK (locked >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- outbox_events (Transactional Outbox)
CREATE TABLE IF NOT EXISTS outbox_events (
    id             BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    event_id       CHAR(36)         NOT NULL COMMENT 'Unique event identifier',
    aggregate_type VARCHAR(50)      NOT NULL COMMENT 'e.g. Order, Balance',
    aggregate_id   VARCHAR(36)      NOT NULL,
    event_type     VARCHAR(100)     NOT NULL COMMENT 'e.g. OrderPlaced, BalanceDeducted',
    kafka_topic    VARCHAR(128)     NOT NULL,
    kafka_key      VARCHAR(128)     NOT NULL,
    payload        JSON             NOT NULL,
    status         ENUM('PENDING','SENT','FAILED') NOT NULL DEFAULT 'PENDING',
    retry_count    TINYINT UNSIGNED NOT NULL DEFAULT 0,
    created_at     DATETIME(6)      NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    sent_at        DATETIME(6)      NULL,

    PRIMARY KEY (id),
    UNIQUE KEY uk_event_id (event_id),
    INDEX idx_status_id (status, id),
    INDEX idx_status_created (status, created_at),
    INDEX idx_aggregate (aggregate_type, aggregate_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- processed_events (Consumer idempotency)
CREATE TABLE IF NOT EXISTS processed_events (
    event_id       VARCHAR(100)     NOT NULL,
    consumer_group VARCHAR(100)     NOT NULL,
    processed_at   DATETIME(6)      NOT NULL DEFAULT CURRENT_TIMESTAMP(6),

    PRIMARY KEY (event_id, consumer_group),
    INDEX idx_processed_at (processed_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- idempotency_keys (API idempotency)
CREATE TABLE IF NOT EXISTS idempotency_keys (
    idempotency_key VARCHAR(128)     NOT NULL PRIMARY KEY,
    response_body   JSON             NULL,
    created_at      DATETIME(6)      NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    expires_at      DATETIME(6)      NOT NULL,

    INDEX idx_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
