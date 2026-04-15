# 4단계: Fix (코드 리뷰 및 리팩토링)

## 1. CI 정적 분석 리뷰 체크리스트

### 1.1 MSA (Microservice Architecture) 위반 검출

| ID | 규칙 | 검증 방법 | 적발 시 조치 |
|---|---|---|---|
| MSA-1 | 모듈 간 직접 호출 금지 | `order/` 패키지가 `balance/` 패키지를 직접 import하는지 grep | 도메인 이벤트로 리팩토링 또는 application 계층에서 인터페이스로 조합 |
| MSA-2 | Cross-module JOIN 쿼리 금지 | SQL 문자열에서 다른 BC 테이블과의 JOIN 검색 | CQRS Read Model로 분리 |
| MSA-3 | API 경계에서 도메인 엔티티 노출 금지 | handler가 domain 구조체를 직접 JSON 반환하는지 확인 | DTO로 변환 |

### 1.2 DDD (Domain-Driven Design) 위반 검출

| ID | 규칙 | 검증 방법 | 적발 시 조치 |
|---|---|---|---|
| DDD-1 | `domain/`이 `infrastructure/` 참조 금지 | `grep -rn '".*infrastructure' internal/*/domain/` | 의존성 역전 원칙 적용 (인터페이스를 domain에, 구현을 infrastructure에) |
| DDD-2 | `domain/`에 DB 드라이버 import 금지 | `grep -rn '"database/sql"\|"sqlx"\|"go-sql-driver"' internal/*/domain/` | repository interface로 추상화 |
| DDD-3 | 도메인 계층에 인프라 관심사 금지 | 커넥션 문자열, 토픽 이름, HTTP 상태 코드, 재시도 로직 검사 | 인프라 계층으로 이동 |
| DDD-4 | Repository interface는 domain에, 구현은 infrastructure에 | 파일 위치 확인 | 올바른 패키지로 이동 |

### 1.3 EDA (Event-Driven Architecture) 위반 검출

| ID | 규칙 | 검증 방법 | 적발 시 조치 |
|---|---|---|---|
| EDA-1 | DLQ 처리 존재 | consumer 코드에 DLQ 토픽 발행 경로가 있는지 확인 | DLQ 발행 코드 추가 |
| EDA-2 | Consumer 멱등성 보장 | 모든 consumer handler에 `processed_events` 조회+삽입이 있는지 확인 | 멱등성 래퍼 추가 |
| EDA-3 | Outbox INSERT가 비즈니스 트랜잭션 내부 | outbox insert가 `tx.` 컨텍스트 내에서 실행되는지 확인 | 트랜잭션 내부로 이동 |
| EDA-4 | 보상 트랜잭션 설계 | 주문 생성 실패 시 잔고 롤백이 보장되는지 확인 | 보상 이벤트 추가 |
| EDA-5 | 이벤트 페이로드 자기 서술적 | 모든 이벤트에 event_id, event_type, aggregate_id, timestamp 포함 확인 | 누락 필드 추가 |

## 2. Outbox Relay Worker 검증

| ID | 규칙 | 검증 방법 |
|---|---|---|
| RELAY-1 | `SELECT ... FOR UPDATE SKIP LOCKED` 사용 | relay 쿼리에 `SKIP LOCKED` grep |
| RELAY-2 | Kafka ack 확인 후에만 SENT 마킹 | produce → flush → update 순서 확인 |
| RELAY-3 | Exponential backoff 적용 | 재시도 간격이 `base × 2^retry_count` 인지 확인 |
| RELAY-4 | max_retries 초과 시 FAILED 처리 | retry_count >= max 시 status 변경 확인 |
| RELAY-5 | Stuck event 감지 | `PENDING AND created_at < NOW() - 5분` 검사 로직 존재 확인 |

## 3. 성능 / Connection Pool 검증

| ID | 규칙 | 검증 방법 |
|---|---|---|
| PERF-1 | Pool 크기가 TPS 목표에 적합 | `MaxOpenConns >= TPS × avg_latency` 계산 확인 |
| PERF-2 | 무제한 고루틴 생성 없음 | worker pool 또는 semaphore 사용 확인 |
| PERF-3 | Context propagation | 모든 DB/Kafka 호출에 `context.Context` 전달 확인 |
| PERF-4 | N+1 쿼리 없음 | 반복문 내 개별 SELECT 대신 `IN (?)` 사용 확인 |
| PERF-5 | float64로 금융 연산 금지 | `shopspring/decimal` 사용 확인 |

## 4. CI 아키텍처 검증 스크립트

```bash
#!/bin/bash
# ci/arch-check.sh
set -euo pipefail

FAIL=0

echo "=== DDD-1: domain must not import infrastructure ==="
if grep -rn '".*infrastructure' internal/order/domain/ internal/balance/domain/ 2>/dev/null; then
    echo "FAIL: domain/ imports infrastructure/"
    FAIL=1
fi

echo ""
echo "=== DDD-2: domain must not import database drivers ==="
if grep -rn '"database/sql"\|"github.com/jmoiron/sqlx"\|"github.com/go-sql-driver"' \
    internal/order/domain/ internal/balance/domain/ internal/shared/domain/ 2>/dev/null; then
    echo "FAIL: domain/ imports DB drivers"
    FAIL=1
fi

echo ""
echo "=== MSA-1: no direct cross-BC imports ==="
if grep -rn '"github.com/HongJungWan/harness-engineering/internal/balance' internal/order/ 2>/dev/null | \
    grep -v 'application/' | grep -v '_test.go'; then
    echo "FAIL: order/ directly imports balance/ outside application layer"
    FAIL=1
fi

echo ""
echo "=== EDA-3: outbox insert must be in transaction ==="
if grep -rn 'INSERT INTO outbox_events' internal/ 2>/dev/null | grep -v 'tx\.\|Tx\.' | grep -v '_test.go'; then
    echo "WARNING: outbox insert possibly outside transaction context"
fi

echo ""
echo "=== RELAY-1: relay must use SKIP LOCKED ==="
if ! grep -rn 'SKIP LOCKED' internal/outbox/ 2>/dev/null; then
    echo "FAIL: relay worker missing SKIP LOCKED"
    FAIL=1
fi

echo ""
echo "=== PERF-5: no float64 for financial calculations ==="
if grep -rn 'float64' internal/order/domain/ internal/balance/domain/ 2>/dev/null | \
    grep -v '_test.go' | grep -v '// float64'; then
    echo "WARNING: float64 found in domain layer - verify it's not used for money"
fi

echo ""
if [ $FAIL -eq 0 ]; then
    echo "All architecture checks PASSED."
else
    echo "Architecture checks FAILED. Fix violations before merging."
    exit 1
fi
```

## 5. 아키텍처 리뷰 추가 점검 항목

### 5.1 최종 일관성 (Eventual Consistency)

- [ ] Outbox → Kafka → Consumer 경로에서 이벤트가 최종적으로 소비되는가?
- [ ] Consumer 장애 시 재처리가 가능한가? (Kafka offset commit 전략)
- [ ] 이벤트 순서가 보장되는가? (같은 aggregate_id는 같은 파티션)

### 5.2 보상 트랜잭션

- [ ] 체결 엔진에서 주문 거부 시 잔고 복원 경로가 존재하는가?
- [ ] 부분 체결 후 나머지 취소 시 정확한 금액이 복원되는가?

### 5.3 MSA 전환 로드맵 준수

- [ ] Order BC와 Balance BC의 코드 결합도가 낮은가?
- [ ] DB 분리 시 outbox 이벤트 스키마만으로 통합 가능한가?
- [ ] 공유 도메인(shared/)이 최소한으로 유지되는가?

## 6. 리뷰 프로세스

1. 코드 구현 완료 후 `ci/arch-check.sh` 실행
2. 위반 적발 시 즉시 수정
3. 수정 후 재실행하여 모든 체크 PASS 확인
4. BDD 테스트 전체 통과 확인
5. 최종 아키텍처 점검 항목 수동 리뷰
