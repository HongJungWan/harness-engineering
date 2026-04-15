# 3단계: Hook (테스트 및 검증)

## 1. 테스트 환경 제약

### 1.1 Fixture 구조화

- **Testcontainers-go**를 사용하여 MySQL 8.0 + Kafka 컨테이너를 테스트 시 자동 기동
- 컨테이너는 `BeforeSuite`에서 **1회만** 시작 (시나리오별 재시작 금지)
- 각 시나리오 시작 전 `TRUNCATE` + Kafka 토픽 퍼지로 격리 보장
- **Mocking 최소화**: 실제 MySQL과 Kafka를 사용하는 통합 테스트가 원칙

### 1.2 테스트 디렉토리 구조

```
test/acceptance/
├── features/                       # Gherkin .feature 파일
│   ├── order_placement.feature
│   ├── concurrent_orders.feature
│   ├── outbox_guarantee.feature
│   ├── idempotency.feature
│   ├── insufficient_balance.feature
│   └── order_cancellation.feature
├── steps/                          # Step definitions
│   ├── order_steps.go
│   ├── balance_steps.go
│   ├── outbox_steps.go
│   ├── kafka_steps.go
│   ├── concurrency_steps.go
│   └── idempotency_steps.go
└── support/                        # 테스트 인프라
    ├── suite_test.go               # godog TestSuite wiring
    ├── containers.go               # Testcontainers 라이프사이클
    ├── db_fixture.go               # DB 시드/클린업 헬퍼
    ├── kafka_fixture.go            # Kafka 토픽/메시지 헬퍼
    ├── concurrent.go               # 동시성 barrier 헬퍼
    └── wait.go                     # Eventually 폴링 헬퍼
```

### 1.3 테스트 의존성

```
github.com/cucumber/godog                   # BDD 프레임워크
github.com/testcontainers/testcontainers-go # 컨테이너 오케스트레이션
github.com/stretchr/testify                 # 어서션 헬퍼
```

## 2. BDD 인수 테스트 실행 규칙

### 2.1 테스트 호출 방식

- Step definitions는 **서비스 계층을 직접 호출** (HTTP 서버 기동 없음)
- 이유: 결정론적 제어 (relay worker 시작/중지), 동시성 테스트 오버헤드 최소화

### 2.2 Relay Worker 제어

- 기본: 테스트 시작 시 relay worker 중지 상태
- `relay worker가 시작된다` step에서만 활성화
- Outbox 보장 테스트에서 반드시 중지 → 확인 → 시작 → 확인 순서

## 3. 핵심 테스트 Hook 상세

### Hook 1: 동시성 Hook (비관적 락 검증)

**목적:** 동일 Aggregate(Balance)에 동시 수십 개 요청이 들어올 때 비관적 락이 충돌을 정상적으로 방지하는지 검증

**구현:**
```go
// sync barrier 패턴으로 모든 고루틴이 동시에 시작
func RunConcurrent(n int, fn func(index int) (string, error)) []ConcurrentResult {
    results := make([]ConcurrentResult, n)
    start := make(chan struct{})
    var wg sync.WaitGroup
    wg.Add(n)
    for i := 0; i < n; i++ {
        go func(idx int) {
            defer wg.Done()
            <-start  // barrier: 모든 고루틴 대기
            orderID, err := fn(idx)
            results[idx] = ConcurrentResult{Index: idx, OrderID: orderID, Err: err}
        }(i)
    }
    close(start)  // 동시 해제
    wg.Wait()
    return results
}
```

**검증 항목:**
- [ ] 모든 요청이 에러 없이 완료 (성공 또는 잔고 부족)
- [ ] 성공 주문의 총 차감액 = 성공_수 × 단가
- [ ] available + locked = 초기 잔고 (잔고 보존 법칙)
- [ ] 음수 잔고 없음
- [ ] 데드락 에러(MySQL 1213) 없음

### Hook 2: 이중 쓰기 Hook (Outbox 보장)

**목적:** 비즈니스 로직 성공 후 Kafka 직접 발행 전 앱 종료를 시뮬레이션하여 Outbox가 이벤트를 보장하는지 검증

**시뮬레이션 방법:**
1. Relay worker 중지
2. 주문 생성 (성공 → outbox에만 기록)
3. Kafka 토픽 확인 → 메시지 없음
4. Relay worker 시작
5. Kafka 토픽 확인 → 메시지 도착

**검증 항목:**
- [ ] Relay 중지 중에는 Kafka에 메시지 없음
- [ ] Relay 시작 후 10초 이내 Kafka에 메시지 도착
- [ ] Outbox 이벤트가 SENT로 마킹됨
- [ ] 다수 이벤트 복구 시 시간순 보장

### Hook 3: 멱등성 Hook (중복 이벤트 처리)

**목적:** 동일 `eventId`를 가진 메시지를 Kafka에 중복 발행하여 Consumer가 한 번만 처리하는지 검증

**검증 항목:**
- [ ] 동일 eventId 20회 발행 시 processed_events 1행
- [ ] 서로 다른 eventId는 각각 처리됨
- [ ] 사이드 이펙트가 정확히 1회만 적용됨

### Hook 4: 잔고 부족 Hook

**검증 항목:**
- [ ] 잔고 초과 주문 시 에러 반환 ("insufficient balance")
- [ ] 잔고 변동 없음 (available, locked 모두 원래값)
- [ ] 주문 레코드 생성되지 않음
- [ ] Outbox 이벤트 없음

### Hook 5: 주문 취소 + 잔고 복원 Hook

**검증 항목:**
- [ ] 취소 후 order.status = "CANCELLED"
- [ ] available = 원래값 (locked이 복원됨)
- [ ] locked = 0
- [ ] "OrderCancelled" + "BalanceRestored" outbox 이벤트 존재
- [ ] 이미 취소된 주문 재취소 시 멱등 (에러 없이 무시)

## 4. 비동기 결과 검증 패턴

```go
// Eventually: 비동기 결과를 폴링으로 검증
func Eventually(timeout, interval time.Duration, condition func() (bool, error)) error {
    deadline := time.After(timeout)
    for {
        ok, err := condition()
        if err != nil { return err }
        if ok { return nil }
        select {
        case <-deadline:
            return fmt.Errorf("timeout after %v", timeout)
        case <-time.After(interval):
            // retry
        }
    }
}
```

- 모든 Kafka 메시지 확인에 `Eventually` 사용 (time.Sleep 금지)
- 기본: timeout=10s, interval=200ms
