# 2단계: Code (구현 제약 사항)

## 1. 디렉토리 구조 제약 (Schema-per-Module + 패키지 순수성)

```
harness-engineering/
├── cmd/server/main.go                          # 엔트리포인트 (DI wiring)
├── internal/
│   ├── config/config.go                        # 환경변수 기반 설정
│   ├── order/                                  # ── Order Bounded Context ──
│   │   ├── domain/                             # 순수 비즈니스 로직 (인프라 의존성 0)
│   │   │   ├── order.go                        # Order Aggregate Root + 상태 전이
│   │   │   ├── events.go                       # OrderPlaced, OrderCancelled 도메인 이벤트
│   │   │   ├── repository.go                   # OrderRepository interface 정의
│   │   │   └── errors.go                       # 도메인 에러
│   │   ├── application/                        # 트랜잭션 관리, 유스케이스 오케스트레이션
│   │   │   ├── place_order.go                  # PlaceOrderUseCase (핵심)
│   │   │   └── cancel_order.go                 # CancelOrderUseCase
│   │   ├── infrastructure/                     # MySQL, Kafka 연동
│   │   │   └── mysql_order_repo.go             # OrderRepository 구현
│   │   └── presentation/                       # 외부 API + ACL
│   │       ├── handler.go                      # HTTP 핸들러
│   │       └── dto.go                          # Request/Response DTO
│   ├── balance/                                # ── Balance Bounded Context ──
│   │   ├── domain/
│   │   │   ├── balance.go                      # Balance Aggregate Root
│   │   │   ├── events.go                       # BalanceDeducted, BalanceRestored
│   │   │   ├── repository.go                   # BalanceRepository interface
│   │   │   └── errors.go
│   │   └── infrastructure/
│   │       └── mysql_balance_repo.go           # SELECT FOR UPDATE 구현
│   ├── shared/                                 # ── 공유 도메인 ──
│   │   ├── domain/
│   │   │   ├── event.go                        # DomainEvent interface
│   │   │   ├── money.go                        # Money VO (shopspring/decimal)
│   │   │   └── asset_pair.go                   # AssetPair VO
│   │   └── infrastructure/
│   │       └── tx.go                           # WithTx 트랜잭션 헬퍼
│   └── outbox/                                 # ── Outbox 인프라 ──
│       ├── repository.go                       # OutboxRepository interface
│       ├── mysql_outbox_repo.go                # 구현
│       └── relay.go                            # Outbox Relay Worker
├── migrations/                                 # 마이그레이션 SQL
├── scripts/init.sql                            # Docker 초기화 DDL
├── test/acceptance/                            # BDD 인수 테스트
├── docker-compose.yml
├── Dockerfile
├── Makefile
└── go.mod
```

## 2. 패키지 의존성 규칙 (반드시 준수)

```
[presentation] → [application] → [domain] ← [infrastructure]
                                     ↑
                              [shared/domain]
```

### 금지 사항 (위반 시 Fix 단계에서 적발)
| 규칙 | 설명 |
|---|---|
| `domain/` → `infrastructure/` 참조 금지 | 도메인 계층은 순수해야 함 |
| `domain/` → `database/sql` import 금지 | DB 드라이버 의존 금지 |
| `order/` → `balance/` 직접 호출 금지 | BC 간 직접 호출 대신 application 계층에서 조합 |
| Cross-module JOIN 쿼리 금지 | 다른 BC 테이블과 JOIN 하지 않음 |

## 3. 핵심 비즈니스 로직 구현 제약

### 3.1 비관적 락 트랜잭션 (PlaceOrderUseCase)

```go
// 반드시 아래 순서를 지킬 것:
// 1. 멱등성 키 확인
// 2. Value Object 생성 + 필요 금액 계산
// 3. BEGIN TX
//    4. SELECT ... FROM balances WHERE user_id=? AND currency=? FOR UPDATE
//    5. balance.DeductAndLock(amount)
//    6. NewOrder → Accept()
//    7. balanceRepo.Save(tx, balance) → UPDATE balances + INSERT outbox_events
//    8. orderRepo.Save(tx, order)    → INSERT orders + INSERT outbox_events
//    9. INSERT idempotency_keys
// 10. COMMIT
```

**제약:**
- 잠금 순서: 항상 balance 먼저, order 그 다음 (데드락 방지)
- 단일 트랜잭션에서 비즈니스 엔티티 + Outbox 이벤트 모두 저장
- 트랜잭션 외부에서 Kafka 직접 발행 금지 (이중 쓰기 방지)

### 3.2 Outbox Relay Worker 제약

- `FOR UPDATE SKIP LOCKED` 필수 사용
- Kafka `acks=all` 확인 후에만 `status='SENT'` 업데이트
- 재시도 시 exponential backoff 적용
- max_retries 초과 시 `status='FAILED'` 설정 + 로그/알림

### 3.3 Consumer 멱등성 제약

- 모든 이벤트 처리 전 `processed_events` 테이블 조회
- 처리 후 `processed_events` 삽입
- 조회 + 비즈니스 로직 + 삽입을 단일 트랜잭션으로 묶음

## 4. Go 의존성 명세

```
github.com/jmoiron/sqlx                     # DB 접근 (sqlx)
github.com/go-sql-driver/mysql              # MySQL 드라이버
github.com/shopspring/decimal               # 정밀 금융 연산 (부동소수점 금지)
github.com/google/uuid                      # UUID 생성
github.com/IBM/sarama                       # Kafka 클라이언트
github.com/go-chi/chi/v5                    # HTTP 라우터
github.com/caarlos0/env/v10                 # 환경변수 파싱
```

## 5. 코딩 컨벤션

- 금융 연산에 `float64` 사용 금지 → 반드시 `shopspring/decimal`
- Aggregate Root의 필드 변경은 메서드를 통해서만 허용
- 도메인 이벤트는 Aggregate Root 내부에서만 생성
- `PullEvents()` 호출로 이벤트를 drain한 후 repository에서 outbox에 저장
- 에러는 도메인 패키지에 sentinel error로 정의
- Context propagation: 모든 DB/Kafka 호출에 `context.Context` 전달
