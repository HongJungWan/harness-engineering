# 1단계: Plan (설계 및 계획)

## 1. 문제 정의

코빗과 같은 가상자산 거래소의 MSA 환경에서 **주문 서비스**와 **체결 엔진**이 분리되어 있다.
주문이 들어오면 사용자의 잔고를 점검/차감하고, 이 이벤트를 체결 엔진(Kafka)으로 전달해야 한다.

### 핵심 요구사항
- **데이터 누락 없음** (Zero data loss)
- **중복 처리 없음** (Exactly-once semantics)
- **TPS 4,000+** 고부하 환경 지원

### 기술 스택
- Go 1.2x
- MySQL 8.0 (sqlx)
- Kafka (KRaft mode)
- Docker

---

## 2. DDD 모델링

### 2.1 Bounded Context

| Bounded Context | 책임 | 관계 |
|---|---|---|
| **Order BC** | 주문 생명주기 관리 (생성 → 접수 → 체결/취소) | Balance BC와 Customer-Supplier |
| **Balance BC** | 사용자 자산 잔고 관리 (차감/복원/정산) | Order BC에 잔고 서비스 제공 |
| **Matching Engine BC** | 주문 매칭 및 체결 (외부 시스템) | Kafka 이벤트 소비 |

**설계 결정:** Order BC와 Balance BC는 동일 MySQL 인스턴스를 공유한다.
- **이유:** 잔고 차감 + 주문 생성을 단일 ACID 트랜잭션으로 처리하여 데이터 정합성 보장.
- **추후 분리 가능:** Outbox 패턴으로 통합 계약(이벤트 스키마)이 이미 정의되어 있어 DB 분리 시 마이그레이션 용이.

### 2.2 Aggregate Root: Order

```
Entity: Order
Table: orders

필드:
  id            CHAR(36)       UUIDv7, 시간 정렬 가능
  user_id       BIGINT         소유자
  pair          VARCHAR(20)    e.g. "BTC/KRW"
  side          ENUM           BUY | SELL
  order_type    ENUM           LIMIT | MARKET
  price         DECIMAL(30,8)  지정가 (MARKET은 0)
  quantity      DECIMAL(30,8)  주문 수량
  filled_qty    DECIMAL(30,8)  체결 수량
  status        ENUM           상태 (아래 상태 머신 참조)
  reason        VARCHAR(256)   거부/취소 사유
  version       BIGINT         낙관적 동시성 제어

상태 머신 (State Machine):
  PENDING ──Accept()──> ACCEPTED ──Fill()──> PARTIALLY_FILLED ──Fill()──> FILLED
     │                     │                         │
     └──Reject()──> REJECTED  └──Cancel()──> CANCELLED  └──Cancel()──> CANCELLED

불변식 (Invariants):
  1. filled_qty <= quantity (항상)
  2. 터미널 상태(FILLED, CANCELLED, REJECTED)는 불변
  3. LIMIT 주문에는 price > 0 필수
  4. quantity > 0 (항상)

도메인 이벤트:
  - OrderPlaced     (Accept 시 발행)
  - OrderCancelled  (Cancel 시 발행)
  - OrderFilled     (Fill 완료 시 발행)
```

### 2.3 Aggregate Root: Balance

```
Entity: Balance
Table: balances

필드:
  id            BIGINT         PK
  user_id       BIGINT         소유자
  currency      VARCHAR(10)    e.g. "BTC", "KRW"
  available     DECIMAL(30,8)  사용 가능 금액
  locked        DECIMAL(30,8)  주문에 의해 잠긴 금액
  version       BIGINT         낙관적 동시성 제어
  
유니크 제약: (user_id, currency)

불변식 (Invariants):
  1. available >= 0 (항상)
  2. locked >= 0 (항상)
  3. DeductAndLock 시 available >= deduction_amount 검증 필수

메서드:
  - DeductAndLock(amount, refID) → available -= amount, locked += amount
  - Unlock(amount, refID)        → locked -= amount, available += amount (취소 시)
  - SettleFill(amount, refID)    → locked -= amount (체결 완료, 자산 이전)

도메인 이벤트:
  - BalanceDeducted  (DeductAndLock 성공 시)
  - BalanceRestored  (Unlock 성공 시)
```

### 2.4 Value Objects

| Value Object | 구현 | 불변식 |
|---|---|---|
| **Money** | `shopspring/decimal` + currency string | currency 비어있으면 안됨 |
| **AssetPair** | base + quote string | base ≠ quote, 둘 다 비어있으면 안됨 |
| **OrderQuantity** | decimal > 0 | 양수만 허용 |

---

## 3. EDA / Outbox 설계

### 3.1 도메인 이벤트 카탈로그

| Event Type | Kafka Topic | Kafka Key | Payload 핵심 필드 |
|---|---|---|---|
| `OrderPlaced` | `order.events` | order_id | order_id, user_id, pair, side, type, price, qty, status |
| `OrderCancelled` | `order.events` | order_id | order_id, user_id, reason, cancelled_at |
| `BalanceDeducted` | `balance.events` | balance_id | user_id, currency, amount, reference_id, available_after, locked_after |
| `BalanceRestored` | `balance.events` | balance_id | user_id, currency, amount, reference_id, available_after, locked_after |

### 3.2 이벤트 봉투 (Event Envelope)

```json
{
  "event_id": "UUIDv4",
  "event_type": "OrderPlaced",
  "aggregate_type": "Order",
  "aggregate_id": "UUIDv7",
  "occurred_at": "2026-04-15T10:30:00.123456Z",
  "payload": { ... }
}
```

### 3.3 Transactional Outbox 테이블

```sql
CREATE TABLE outbox_events (
    id             BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    event_id       CHAR(36)         NOT NULL,
    aggregate_type VARCHAR(50)      NOT NULL,
    aggregate_id   VARCHAR(36)      NOT NULL,
    event_type     VARCHAR(100)     NOT NULL,
    kafka_topic    VARCHAR(128)     NOT NULL,
    kafka_key      VARCHAR(128)     NOT NULL,
    payload        JSON             NOT NULL,
    status         ENUM('PENDING','SENT','FAILED') NOT NULL DEFAULT 'PENDING',
    retry_count    TINYINT UNSIGNED NOT NULL DEFAULT 0,
    created_at     DATETIME(6)      NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    sent_at        DATETIME(6)      NULL,

    UNIQUE KEY uk_event_id (event_id),
    INDEX idx_status_id (status, id),
    INDEX idx_status_created (status, created_at)
) ENGINE=InnoDB;
```

**설계 결정:**
- `BIGINT AUTO_INCREMENT` PK: 삽입 순서 보장, 폴러의 순차 읽기에 최적화
- `event_id`는 외부 식별자(UUIDv4)이지만 PK로 사용하지 않음 (인덱스 단편화 방지)
- `kafka_key = aggregate_id`: 동일 엔티티 이벤트가 같은 파티션에 전달되어 순서 보장
- `idx_status_id`: 릴레이의 `WHERE status='PENDING' ORDER BY id` 쿼리 최적화

### 3.4 Outbox Relay Worker 설계

```
Relay Worker 폴링 루프:
  1. BEGIN TRANSACTION
  2. SELECT id, event_id, kafka_topic, kafka_key, payload
     FROM outbox_events
     WHERE status = 'PENDING' AND id > :last_published_id
     ORDER BY id ASC
     LIMIT :batch_size
     FOR UPDATE SKIP LOCKED
  3. 각 이벤트를 Kafka로 비동기 Produce
  4. Kafka Flush (모든 ack 대기)
  5. UPDATE outbox_events SET status='SENT', sent_at=NOW(6) WHERE id IN (:ids)
  6. COMMIT

설정값:
  - 폴링 간격: 50ms (고부하), idle 시 500ms까지 백오프
  - 배치 크기: 100
  - 워커 수: 2 (SKIP LOCKED로 충돌 방지)
  - 최대 재시도: 5회 (초과 시 status='FAILED')
  - Kafka: acks=all, enable.idempotence=true
```

### 3.5 Consumer 멱등성 테이블

```sql
CREATE TABLE processed_events (
    event_id       VARCHAR(100) NOT NULL,
    consumer_group VARCHAR(100) NOT NULL,
    processed_at   DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (event_id, consumer_group)
) ENGINE=InnoDB;
```

---

## 4. 고부하(TPS 4,000) 대비 구조적 문제점 정의

### 4.1 문제 1: 동일 사용자 잔고에 대한 동시 접근

**문제:** 동일 사용자가 짧은 시간에 다수의 주문을 넣으면 잔고 레코드에 동시 접근이 발생한다.
낙관적 락(version check)을 사용하면 TPS 4,000에서 retry storm이 발생한다.

**해결: 비관적 락 (SELECT ... FOR UPDATE)**
- 잔고 행을 트랜잭션 시작 시 배타적으로 잠금
- 다른 트랜잭션은 커밋까지 대기 (직렬화)
- 임계 구간(잔고 확인 → 차감 → 주문 생성 → Outbox 저장 → 커밋) ≈ 2ms
- 단일 사용자 처리량: ~500 orders/sec (충분)
- 서로 다른 사용자는 서로 다른 행을 잠그므로 완전 병렬

```
핵심 트랜잭션 플로우:
  1. 멱등성 키 확인
  2. Value Object 생성 + 필요 금액 계산
  3. BEGIN TRANSACTION
     4. SELECT ... FROM balances WHERE user_id=? AND currency=? FOR UPDATE  ← 잔고 행 잠금
     5. balance.DeductAndLock(requiredAmount) → 잔액 부족 시 ROLLBACK
     6. order = NewOrder(...).Accept()
     7. balanceRepo.Save(tx, balance)      → UPDATE balances + INSERT outbox_events
     8. orderRepo.Save(tx, order)          → INSERT orders + INSERT outbox_events
     9. INSERT idempotency_keys
  10. COMMIT
```

### 4.2 문제 2: DB Connection Pool 병목

**계산 (Little's Law):**
```
필요 커넥션 = TPS × 평균 트랜잭션 시간
            = 4,000 × 0.002s
            = 8 (최소)

실제 설정 (버스트 + GC + 릴레이 워커 대비):
  MaxOpenConns    = 50
  MaxIdleConns    = 25
  ConnMaxLifetime = 5m
  ConnMaxIdleTime = 1m

MySQL 서버:
  max_connections          = 300
  innodb_lock_wait_timeout = 3  (데드락 빠른 감지)
  transaction-isolation    = READ-COMMITTED (gap lock 최소화)
```

### 4.3 문제 3: Outbox Poller의 Table Full Scan

**문제:** status='PENDING' 행을 반복 조회할 때 인덱스 없이 전체 테이블 스캔 발생 가능.

**해결:**
- `idx_status_id (status, id)` 복합 인덱스: 폴러 쿼리가 range scan만 수행
- Batch polling (100건씩): 한 번의 스캔으로 여러 이벤트 처리
- `FOR UPDATE SKIP LOCKED`: 다중 워커가 동일 행을 잠그지 않고 분산 처리
- 처리량: 2 워커 × 100 batch × 20 polls/sec = **4,000 events/sec**

**정리 전략:**
- 7일 이상 `SENT` 상태 이벤트 정기 삭제
- `DELETE FROM outbox_events WHERE status='SENT' AND sent_at < NOW() - INTERVAL 7 DAY`

### 4.4 문제 4: Kafka 장애 시 이벤트 유실

**문제:** Kafka 브로커 다운 시 이벤트가 유실될 수 있다.

**해결:** Outbox 패턴 자체가 해결책
- 이벤트는 MySQL에 먼저 저장 (ACID 보장)
- Kafka 장애 시 outbox에 이벤트가 축적됨
- 릴레이가 무한 재시도하며 Kafka 복구 후 자동 전달
- **설계상 데이터 유실 없음**

### 4.5 문제 5: 데드락

**문제:** 다중 테이블에 대한 동시 잠금 시 데드락 발생 가능.

**해결:**
- 잠금 순서 일관성: 항상 balance 먼저, 그 다음 order
- InnoDB의 데드락 감지: 즉시 감지 후 한 트랜잭션 롤백
- 애플리케이션 레벨: 데드락 시 1회 재시도

---

## 5. BDD 인수 테스트 시나리오

### Scenario 1: Happy Path - 주문 생성

```gherkin
Feature: 주문 생성 Happy Path
  As a 가상자산 거래소 사용자
  I want to BUY 주문을 넣으면 잔고가 차감되고 이벤트가 발행된다

  Background:
    Given 시스템이 초기화되어 있다
    And user "user-001"의 KRW 잔고가 10,000,000 이다

  Scenario: 잔고 충분한 BUY LIMIT 주문 성공
    When user "user-001"이 BTC/KRW BUY LIMIT 주문을 넣는다 (price=95,000,000, qty=0.1)
    Then 주문이 "ACCEPTED" 상태로 생성된다
    And user "user-001"의 KRW available 잔고가 500,000 이다
    And user "user-001"의 KRW locked 잔고가 9,500,000 이다
    And "OrderPlaced" outbox 이벤트가 존재한다
    And "BalanceDeducted" outbox 이벤트가 존재한다
```

### Scenario 2: 동시성 - 비관적 락 검증

```gherkin
Feature: 동시성 비관적 락 검증
  As a 거래소 플랫폼
  I need 동일 잔고에 대한 동시 주문이 정확하게 직렬화된다

  Scenario: 동일 잔고에 10개 동시 주문
    Given user "user-concurrent"의 KRW 잔고가 10,000,000 이다
    When 10개의 동시 BUY 주문이 들어온다 (각 price=95,000,000, qty=0.01)
    Then 모든 주문이 에러 없이 성공 또는 실패한다
    And 성공한 주문의 총 차감액 = 성공_수 × 950,000
    And available + locked = 10,000,000 (잔고 보존 법칙)
    And 음수 잔고가 존재하지 않는다

  Scenario: 잔고 초과 동시 주문 부분 거부
    Given user "user-concurrent"의 KRW 잔고가 3,000,000 이다
    When 10개의 동시 BUY 주문이 들어온다 (각 price=95,000,000, qty=0.01)
    Then 정확히 3개 주문이 성공한다
    And 정확히 7개 주문이 "insufficient balance"로 거부된다
```

### Scenario 3: 이중 쓰기 방지 - Outbox 보장

```gherkin
Feature: Outbox 이벤트 전달 보장
  As a 거래소 플랫폼
  I need 앱 크래시 후에도 이벤트가 복구되어 Kafka에 전달된다

  Background:
    Given relay worker가 중지되어 있다
    And user "user-outbox"의 KRW 잔고가 50,000,000 이다

  Scenario: 크래시 시뮬레이션 후 이벤트 복구
    When user "user-outbox"가 BUY 주문을 넣는다
    Then 주문이 "ACCEPTED" 상태로 생성된다
    And outbox에 PENDING 이벤트가 존재한다
    And Kafka "order.events" 토픽에 메시지가 없다
    When relay worker가 시작된다
    Then 10초 이내에 Kafka "order.events"에 메시지가 도착한다
    And outbox 이벤트가 "SENT"로 마킹된다
```

### Scenario 4: 멱등성 - 중복 이벤트 처리

```gherkin
Feature: Consumer 멱등성
  As a 거래소 플랫폼
  I need 동일 이벤트가 중복 전달되어도 한 번만 처리된다

  Scenario: 동일 eventId 20회 중복 발행
    Given event consumer가 실행 중이다
    When event "evt-001"이 Kafka에 20회 발행된다
    Then processed_events 테이블에 "evt-001" 행이 정확히 1개 존재한다
```

### Scenario 5: 잔고 부족 거부

```gherkin
Feature: 잔고 부족 주문 거부
  Scenario: 잔고 초과 주문 거부
    Given user "user-poor"의 KRW 잔고가 100,000 이다
    When user "user-poor"가 BUY 주문을 넣는다 (price=95,000,000, qty=1.0)
    Then "insufficient balance" 에러로 거부된다
    And user "user-poor"의 KRW 잔고가 100,000 그대로이다
    And 주문이 생성되지 않는다
    And outbox 이벤트가 없다
```

### Scenario 6: 주문 취소 + 잔고 복원

```gherkin
Feature: 주문 취소 및 잔고 복원
  Scenario: 주문 취소 시 잔고 원자적 복원
    Given user "user-cancel"의 KRW 잔고가 10,000,000 이다
    And user "user-cancel"이 BUY 주문 "order-c1"을 넣어 9,500,000 KRW가 잠겼다
    When user "user-cancel"이 "order-c1"을 취소한다
    Then "order-c1"의 상태가 "CANCELLED"이다
    And user "user-cancel"의 KRW available이 10,000,000 이다
    And user "user-cancel"의 KRW locked이 0 이다
    And "OrderCancelled" outbox 이벤트가 존재한다
    And "BalanceRestored" outbox 이벤트가 존재한다
```
