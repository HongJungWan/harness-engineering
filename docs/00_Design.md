# 0단계: Design (설계 내러티브)

> 이 문서는 "왜 이렇게 설계했는가"를 기록한다. 실행 환경(하네스)이 아니라 **스펙 배경 문서**다.
> 하네스가 실제로 돌리는 규칙·상태·레시피는 `01_Plan.md` ~ `04_Fix.md`에 있다.

## 1. 문제 정의

코빗과 같은 가상자산 거래소의 MSA 환경에서 **주문 서비스**와 **체결 엔진**이 분리되어 있다.
주문이 들어오면 사용자의 잔고를 점검/차감하고, 이 이벤트를 체결 엔진(Kafka)으로 전달해야 한다.

### 핵심 요구사항
- **데이터 누락 없음** (Zero data loss)
- **중복 처리 없음** (Exactly-once semantics)
- **TPS 4,000+** 고부하 환경 지원

### 기술 스택
- Go 1.25
- MySQL 8.0 (sqlx)
- Kafka (KRaft mode)
- Docker
- 테스트: 실제 MySQL + Kafka 통합 테스트 (mock 금지)

---

## 2. DDD 모델링

### 2.1 Bounded Context

| BC | 책임 | 관계 |
|---|---|---|
| **Order BC** | 주문 생명주기 (PENDING → ACCEPTED → FILLED/CANCELLED) | Balance BC와 Customer-Supplier |
| **Balance BC** | 사용자 자산 잔고 (차감/잠금/복원/정산) | Order BC에 잔고 서비스 제공 |
| **Matching Engine BC** | 주문 매칭·체결 (외부) | Kafka 이벤트 소비자 |

**Order BC와 Balance BC는 동일 MySQL 인스턴스를 공유**하되 패키지 수준에서 격리한다. 단일 ACID 트랜잭션으로 잔고 차감 + 주문 생성을 묶어 정합성을 보장한다. DB 분리는 추후 Outbox 이벤트 스키마를 통합 계약으로 유지해 마이그레이션 가능하게 설계.

### 2.2 Order Aggregate

```
Entity: Order
Table: orders

필드: id (UUIDv7), user_id, pair, side (BUY|SELL), order_type (LIMIT|MARKET),
     price DECIMAL(30,8), quantity DECIMAL(30,8), filled_qty DECIMAL(30,8),
     status, reason, version

상태 머신:
  PENDING ──Accept()──> ACCEPTED ──Fill()──> PARTIALLY_FILLED ──Fill()──> FILLED
     │                     │                         │
     └──Reject()──> REJECTED  └──Cancel()──> CANCELLED  └──Cancel()──> CANCELLED

불변식:
  1. filled_qty <= quantity
  2. 터미널 상태(FILLED/CANCELLED/REJECTED)는 불변
  3. LIMIT 주문에는 price > 0
  4. quantity > 0

도메인 이벤트: OrderPlaced, OrderCancelled, OrderFilled
```

### 2.3 Balance Aggregate

```
Entity: Balance
Table: balances

필드: id, user_id, currency, available DECIMAL(30,8), locked DECIMAL(30,8), version
유니크: (user_id, currency)

불변식:
  1. available >= 0
  2. locked >= 0
  3. DeductAndLock(amount): 반드시 available >= amount 확인

메서드:
  DeductAndLock(amount, refID) → available -= amount, locked += amount
  Unlock(amount, refID)        → locked -= amount, available += amount (취소)
  SettleFill(amount, refID)    → locked -= amount (체결 완료)

도메인 이벤트: BalanceDeducted, BalanceRestored
```

### 2.4 Value Objects

| VO | 구현 | 불변식 |
|---|---|---|
| Money | shopspring/decimal + currency string | currency 비어있으면 안됨 |
| AssetPair | base + quote | base ≠ quote, 둘 다 비어있으면 안됨 |
| OrderQuantity | decimal > 0 | 양수만 허용 |

---

## 3. EDA / Transactional Outbox 설계

### 3.1 이벤트 카탈로그

| Event Type | Kafka Topic | Kafka Key | Payload |
|---|---|---|---|
| OrderPlaced | `order.events` | order_id | order_id, user_id, pair, side, type, price, qty, status |
| OrderCancelled | `order.events` | order_id | order_id, user_id, reason, cancelled_at |
| BalanceDeducted | `balance.events` | balance_id | user_id, currency, amount, ref, available_after, locked_after |
| BalanceRestored | `balance.events` | balance_id | user_id, currency, amount, ref, available_after, locked_after |

### 3.2 이벤트 봉투

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

### 3.3 Outbox 테이블 설계 근거

- PK = `BIGINT AUTO_INCREMENT`: 삽입 순서 보장, 폴러의 순차 스캔 최적화. `event_id`(UUIDv4)는 PK로 쓰면 인덱스 단편화가 심함.
- `kafka_key = aggregate_id`: 같은 엔티티 이벤트가 같은 파티션으로 → 순서 보장.
- `idx_status_id(status, id)`: 릴레이의 `WHERE status='PENDING' ORDER BY id`를 range scan 으로.
- `UNIQUE KEY uk_event_id(event_id)`: 중복 outbox 행 삽입 방지 (클라이언트 재시도 안전).

### 3.4 Relay Worker 알고리즘

```
loop:
  BEGIN
  SELECT ... FROM outbox_events
    WHERE status='PENDING' AND id > :last_seen
    ORDER BY id ASC LIMIT :batch
    FOR UPDATE SKIP LOCKED     -- 다중 워커 안전
  produce(events) asynchronously
  flush (wait all acks)        -- acks=all + idempotent=true
  UPDATE status='SENT', sent_at=NOW(6) WHERE id IN (...)
  COMMIT

설정: 폴링 50ms (idle 시 500ms 백오프), batch=100, worker=2, max_retry=5
```

### 3.5 Consumer 멱등성

```sql
CREATE TABLE processed_events (
    event_id       VARCHAR(100) NOT NULL,
    consumer_group VARCHAR(100) NOT NULL,
    processed_at   DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (event_id, consumer_group)
);
```

단일 트랜잭션 안에서: 조회 → 비즈니스 처리 → `processed_events` 삽입.

---

## 4. 고부하(TPS 4,000) 구조적 대응

### 4.1 동일 사용자 잔고 경합 → 비관적 락

낙관적 락(version check)은 TPS 4,000에서 retry storm 발생. `SELECT ... FOR UPDATE` 로 잔고 행을 배타적 잠금. 임계구간 ≈ 2ms → 단일 사용자 ~500 orders/sec. 서로 다른 사용자는 서로 다른 행이므로 완전 병렬.

**트랜잭션 플로우:**
1. 멱등성 키 확인
2. VO 생성 + 필요 금액 계산
3. BEGIN
4. `SELECT ... FROM balances WHERE user_id=? AND currency=? FOR UPDATE`
5. `balance.DeductAndLock(amount)` (실패 시 ROLLBACK)
6. `NewOrder(...).Accept()`
7. `balanceRepo.Save(tx, balance)` → UPDATE balances + INSERT outbox_events
8. `orderRepo.Save(tx, order)` → INSERT orders + INSERT outbox_events
9. INSERT idempotency_keys
10. COMMIT

### 4.2 Connection Pool (Little's Law)

```
필요 커넥션 = TPS × avg_tx_time = 4000 × 0.002s = 8 (최소)
실제: MaxOpenConns=50, MaxIdleConns=25, ConnMaxLifetime=5m, ConnMaxIdleTime=1m
MySQL: max_connections=300, innodb_lock_wait_timeout=3, transaction-isolation=READ-COMMITTED
```

### 4.3 Outbox Poller 병목

- `idx_status_id(status, id)` → range scan
- Batch 100, 2 worker × 20 polls/sec = **4,000 events/sec**
- 7일 이상 SENT 이벤트 정기 삭제

### 4.4 Kafka 장애 시 데이터 유실 방지

Outbox 패턴 자체가 해결: MySQL에 먼저 저장(ACID) → Kafka 장애 시 축적 → 복구 후 자동 전달. **설계상 유실 없음**.

### 4.5 데드락 방지

- **잠금 순서 일관성**: 항상 balance → order
- InnoDB 데드락 감지 후 자동 롤백
- 앱 레벨에서 1회 재시도

---

## 5. 아키텍처 규칙의 근거

### 5.1 DDD

| 규칙 | 근거 |
|---|---|
| `domain/` → `infrastructure/` 금지 | 도메인 순수성. 테스트 고립성. DB 스킴 변경이 도메인 침범 금지 |
| `domain/` → `database/sql`/`sqlx` 금지 | 동일. Repository interface 를 domain에, 구현을 infrastructure에 |
| Handler는 DTO만 노출 | 도메인 엔티티의 필드 추가가 API breaking change 로 새 나가지 않도록 |

### 5.2 MSA

| 규칙 | 근거 |
|---|---|
| `order/domain/` → `balance/` 직접 import 금지 | 미래 BC 분리 로드맵. 교차 의존은 application 계층에서만 |
| Cross-module JOIN 금지 | 미래 DB 분리 시 쿼리 깨짐 방지. 필요 시 CQRS Read Model |

### 5.3 EDA

| 규칙 | 근거 |
|---|---|
| Outbox INSERT 는 비즈니스 TX 내부 | 이중 쓰기 방지. Kafka 직접 발행은 MySQL 롤백과 분리될 수 있음 |
| Consumer 멱등성 필수 | at-least-once 전달 현실. 동일 이벤트 N회 수신 시 부작용 1회 |
| DLQ 존재 | poison pill 격리. 장애 이벤트가 전체 consumer 를 막지 않도록 |

### 5.4 Relay

| 규칙 | 근거 |
|---|---|
| `FOR UPDATE SKIP LOCKED` | 다중 워커 수평 확장. 같은 행 충돌 대신 분산 처리 |
| Kafka ack 후에만 SENT 마킹 | acks=all 확인 없이 마킹하면 ack 실패 시 이벤트 유실 |
| Exponential backoff | 일시 장애 동안 thundering herd 회피 |
| Stuck 감지 (PENDING > 5분) | silent stuck 상태 (워커 크래시 후 트랜잭션 미종료 등) 조기 발견 |

### 5.5 Performance

| 규칙 | 근거 |
|---|---|
| `float64` 도메인 금지 | IEEE 754 오차 → 금융 연산에서 1원 손실도 치명. `shopspring/decimal` 사용 |
| Context propagation | cancellation / deadline / tracing 전파. 모든 DB/Kafka 호출에 전달 |

---

## 6. BDD 인수 테스트 시나리오 (6개)

### Scenario 1: Happy Path

```gherkin
Feature: 주문 생성 Happy Path
  Background:
    Given user "user-001"의 KRW 잔고 = 10,000,000

  Scenario: BUY LIMIT 주문 성공
    When BTC/KRW BUY LIMIT 주문 (price=95,000,000, qty=0.1)
    Then status = "ACCEPTED"
    And available = 500,000, locked = 9,500,000
    And outbox 에 OrderPlaced + BalanceDeducted 존재
```

### Scenario 2: 동시성 비관적 락

```gherkin
Feature: 동시 주문 직렬화
  Scenario: 잔고 10M, 10개 동시 BUY 주문 (각 950K)
    Then 전부 성공 또는 정확히 일부만 성공
    And available + locked == 10,000,000 (보존 법칙)
    And 음수 잔고 없음

  Scenario: 잔고 3M, 10개 동시 BUY 주문 (각 950K)
    Then 정확히 3개 성공, 7개 "insufficient balance"
```

### Scenario 3: Outbox 보장

```gherkin
Feature: Outbox 이벤트 복구
  Background:
    Given relay worker 중지
  Scenario: 크래시 시뮬레이션
    When 주문 생성 → outbox PENDING 확인, Kafka 메시지 없음 확인
    When relay worker 시작
    Then 10초 이내 Kafka 도착, outbox SENT 마킹
```

### Scenario 4: 멱등성

```gherkin
Feature: Consumer 멱등성
  Scenario: 동일 event_id 20회 발행
    Then processed_events 정확히 1행
```

### Scenario 5: 잔고 부족 거부

```gherkin
Feature: 잔고 부족 거부
  Scenario: 잔고 100K, BUY 주문 1.0 BTC @ 95M
    Then "insufficient balance" 에러
    And 잔고 변화 없음, 주문 레코드 없음, outbox 없음
```

### Scenario 6: 주문 취소 + 잔고 복원

```gherkin
Feature: 취소
  Scenario: ACCEPTED 주문 취소
    Then status = "CANCELLED"
    And available = 원래값, locked = 0
    And outbox 에 OrderCancelled + BalanceRestored
```

---

## 7. 관련 문서

- `01_Plan.md` — Task DAG + state machine (하네스 실행 계약)
- `02_Code.md` — 패턴 레지스트리 + 파일 템플릿 (하네스 구현 계약)
- `03_Hook.md` — Hook 인벤토리 + settings.json 배선
- `04_Fix.md` — reason → recipe lookup + escalation
- `05_Review.md` — 리뷰 체크리스트 (Phase 2에서 개정 예정)
