# 하네스 엔지니어링 가이드 — 4시간 / Spring / PostgreSQL 프로파일

> **4시간 AI 코딩 과제**에서 하네스 엔지니어링을 적용하기 위한 실전 가이드.
> 30분 안에 자동 검증 루프를 세팅하고, 남은 3시간 10분을 루프 안에서 코딩한 뒤, 마지막 30분을 검증에 투자한다.

---

## 이 가이드 적용 대상

| 항목 | 값 |
|---|---|
| 시간 | **4시간 (240분)** |
| 스택 | Java 21, Spring Boot 3.x (Jakarta EE), PostgreSQL 15+, Docker Compose |
| 실행 전제 | **단일 인스턴스**, **AWS 관리형 서비스 비의존** |
| 모듈 구조 | **Gradle 멀티 모듈** (multi-project build) |
| 외부 API | **없음** (ACL 규칙 D-5 제외) |
| 설계 지향 | **이벤트 기반 느슨한 결합** (cross-module 통신은 주로 outbox 경유, 일부 동기 허용) |

40분 버전 (MSA / Kafka·SQS / 멀티 인스턴스 / AWS) 은 별도 가이드 [`HARNESS_GUIDE.md`](./HARNESS_GUIDE.md) 참고. **두 가이드의 규칙을 섞어 쓰지 마라** — false-positive 가 루프를 오염시킨다.

---

## 시간 배분 근거

| Phase | 배분 | 근거 |
|---|---|---|
| Phase 1 — 제약 정의 | 20분 | 4h 짜리라 Phase 3 자동 루프가 돌 시간이 충분 → 규칙을 촘촘히 짜는 것이 투자 대비 효과 큼 |
| Phase 2 — 훅 작성 | 10분 | 규칙 함수 1:1 복제. `bash -n` 문법 검증 필수 |
| Phase 3 — 구현 | 2h 40min | 60분 / 120분 경과 시 self-checkpoint 2회 (드리프트 방지) |
| Phase 4 — 검증 | 30분 | `check.sh --full` + 실제 `docker compose up` + `/simplify` + `/security-review` + README walk |
| **합계** | **4h 00min** | |

---

## 자율 실행 프롬프트

Claude Code 에 **아래 프롬프트 하나만** 붙여넣는다. `<여기에 문제 전문을 붙여넣으세요>` 부분만 실제 문제로 교체하면 Phase 1~4 를 한 턴 안에 자율 실행한다.

```
너는 4시간(240분) 제한 AI 코딩 과제를 수행하는 시니어 엔지니어다.
스택: Java 21, Spring Boot 3.x (Jakarta EE), PostgreSQL 15+, Docker Compose.
실행 전제:
  - 단일 인스턴스 (분산 캐시/락 불필요)
  - AWS 등 관리형 서비스 비의존 (로컬 대체 사용)
  - 외부 API 호출 없음
  - Gradle 멀티 모듈 (modules/<bc>/src/main/java/.../{domain,application,infrastructure,presentation})
설계 지향: 이벤트 기반 느슨한 결합. 모든 cross-module 통신이 이벤트일 필요는 없으나
  도메인 핵심 이벤트는 Transactional Outbox 를 경유한다.

아래 [문제 요구사항] 을 읽고 다음 Phase 1~4 를 중단 없이 자율 실행하라.
각 Phase 사이에 사용자 확인을 받지 말고, Phase 4 완료 보고까지 한 턴 안에 끝내라.
사용자에게 질문하지 말고 합리적 가정으로 진행하라.

시간 배분: Phase 1 = 20분, Phase 2 = 10분, Phase 3 = 2h 40min, Phase 4 = 30분.

시작 시 TaskCreate 로 Phase 1~4 + Phase 3 의 60분/120분 체크포인트 2개를 todo 로 만들고,
각 단계 진입/완료마다 상태를 전환하라.
Phase 1~2 는 plan mode 로 설계를 확정한 뒤 ExitPlanMode 로 승인받고,
Phase 3~4 는 auto mode 로 실행하라 — 30분 세팅 오류를 조기 발견하여 후반 재작업을 방지한다.

[Phase 1 — 제약 정의 (20분)]
ultrathink 로 문제를 먼저 분석하고, 아래 D/M/E/I 규칙 중 실제 적용할 것만 선별하라.
제외 판단의 근거(문제 맥락과 맞지 않는 이유) 는 CLAUDE.md "제외된 규칙" 섹션에 한 줄씩 기록한다.

프로젝트 루트의 CLAUDE.md 에서 다음 섹션을 모두 채워라:
- [문제 개요]
- [실행 전제] — JDK 21, Spring Boot 버전 고정, PG 버전, 포트, 프로파일명, docker-compose 서비스 목록
- [디렉토리 구조] — Gradle 멀티 모듈 레이아웃 (BC 별 설명)
- [스코프 결정] — 필수 기능 vs 시간 부족 시 포기 후보 (Phase 3 체크포인트가 참조)
- [금지 패턴] 3~5개, 모두 grep 으로 검증 가능
- [필수 패턴] 2~3개, 모두 grep 으로 검증 가능
- [수정 레시피] 각 FAIL ID 에 대응하는 수정 방법 (4~6줄)
- [설계 가이드라인] grep 불가 원칙 (서술형, hook 비대상)
- [제외된 규칙] D/M/E/I 중 제외한 규칙 + 사유
- [보류 항목] Phase 3 중 일시 skip 처리된 규칙 (시작 시 빈 섹션)

규칙 작성 원칙:
- 각 규칙에 ID (F-1, F-2, ..., R-1, R-2, ... 또는 D-*, M-*, E-*, I-*) 와
  적용 범위(디렉토리 글롭) 를 명시한다.
- 추상적 원칙("clean code", "최종 일관성", "Tell-Don't-Ask") 은 [설계 가이드라인] 에만
  서술형으로 기술하고 [금지/필수 패턴] 엔 절대 넣지 마라 — hook 이 판정할 수 없다.
- EDA 는 본 과제의 필수 요구이므로 E 블록은 기본 포함. 문제가 비동기 이벤트를 사용하지
  않는다는 명시적 근거가 없으면 드롭하지 마라.

[Phase 2 — 검증 훅 작성 (10분)]
.claude/scripts/check.sh 의 예시 함수(check_F1 / check_F2 / check_R1 등) 를 모두 삭제하고,
Phase 1 에서 정의한 ID 와 1:1 매칭되는 bash 함수를 새로 작성하라.
- 위반 시: "FAIL [ID]: <설명>" 출력 + FAIL=1
- 통과 시: "PASS [ID]" 출력
- FAST_RULES (grep, 매 편집) / FULL_RULES (빌드·테스트·docker config, --full 일 때만) 배열에 등록
- 작성 후 반드시 `bash -n .claude/scripts/check.sh` 로 문법 검증하고, 오류가 있으면 고쳐라.

아키텍처 프로파일 — Single-Instance Spring-PG (기본 전부 포함, 문제 맥락과 안 맞는 것만 제외):

경로 규약: `modules/<mod>/src/main/java/...` 를 기본으로 가정한다. 실제 구조가 다르면
해당 경로를 실제 경로로 치환하라. grep 의 import 문자열은 Java/Spring (Jakarta) 기준이다.

[D — DDD 준수]
  D-1  domain 순수성 — <mod>/domain/ 에서 Spring/Jakarta/Jackson 등 프레임워크 import 금지
        grep -rnE 'import (org\.springframework|jakarta\.(persistence|validation|servlet)|javax\.persistence|com\.fasterxml\.jackson)' modules/<mod>/src/main/java/*/domain/
        → 매치되면 FAIL
  D-2  Value Object 강제 — 금융·시간·식별자 필드에 원시 BigDecimal/double/float 직접 사용 금지
        grep -rnE '\b(BigDecimal|double|float)\s+(amount|price|fee|balance|total|limit)\b' modules/<mod>/src/main/java/*/domain/
        → 매치되면 FAIL
  D-3  Aggregate Root 명시 — 각 BC 에 최소 1개의 AggregateRoot (상태 변경 진입점)
        grep -rnE '(@AggregateRoot|class\s+\w+\s+(extends|implements)\s+AggregateRoot|class\s+\w+AggregateRoot)' modules/<mod>/src/main/java/*/domain/
        → 매치가 0이면 FAIL (필수 패턴)
  D-4  상태 전이 캡슐화 — 엔티티에 public setter 금지 (상태 변경은 명명된 도메인 메서드로만)
        grep -rnE '^\s*public\s+void\s+set[A-Z]\w*\s*\(' modules/<mod>/src/main/java/*/domain/
        → 매치되면 FAIL
  # D-5 (ACL) — 외부 API 호출 없는 과제에서는 제외. CLAUDE.md "제외된 규칙" 에 기록.

[M — 멀티 모듈 준수]
  M-1  모듈 간 직접 import 금지 — 다른 BC 의 패키지 import 불가
       (BC 쌍별로 1개 함수씩 생성하는 것을 권장)
        grep -rnE 'import\s+[^;]*\.(order|payment|balance|account|inventory|shipping)\.' modules/<mod>/src/main/java/   # 자기 BC 제외
        → 매치되면 FAIL
  M-2  Schema-per-Module — @Table(schema=...) 이 모듈명과 일치
        grep -rnE '@Table\s*\([^)]*schema\s*=\s*"(?!<mod>)' modules/<mod>/src/main/java/*/infrastructure/
        → 타 모듈 schema 참조 시 FAIL
  M-3  모듈별 독립 Outbox — 각 모듈이 OutboxEntity/테이블 보유 (BC 분리 시 그대로 가져감)
        find modules/<mod>/src/main/java -path '*/infrastructure/outbox/*.java' 2>/dev/null | head -1
        → 결과 없으면 FAIL (필수 패턴)
  M-4  Cross-BC 직접 호출 금지 — application 계층에서 타 모듈 service/facade 직접 호출 금지
       (내부 @EventListener 또는 outbox 로만 연동)
        grep -rnE 'import\s+[^;]*\.(order|payment|balance|account|inventory|shipping)\.application\.' modules/<mod>/src/main/java/*/application/
        → 매치되면 FAIL (M-1 보강)
  M-5  동시성 제어 명시 — 금전/재고 변경 usecase 에 `@Version`(낙관) 또는 `FOR UPDATE`(비관)
       또는 PG advisory lock 중 하나가 존재
        grep -rnE '(@Version|FOR\s+UPDATE|pg_advisory_xact_lock|pg_try_advisory_lock)' modules/<mod>/src/main/java/
        → 매치가 0이면 FAIL (필수 패턴. 단일 인스턴스라 synchronized 도 기술적으로 허용되나 DB 락을 우선)

[E — EDA 준수]
  E-1  Transactional Outbox — usecase 가 ApplicationEventPublisher/producer 를 직접 호출 금지
       (outbox.save() 만 허용; 릴레이가 publish 한다)
        grep -rnE '(applicationEventPublisher|eventPublisher|eventProducer)\.(publishEvent|publish|send)' modules/<mod>/src/main/java/*/application/
        → 매치되면 FAIL
  E-2  Outbox Relay 분리 — outbox/relay/ 에 @Scheduled poller + publisher 분리 존재
        grep -rnE '@Scheduled' modules/<mod>/src/main/java/*/infrastructure/outbox/relay/
        → 매치가 0이면 FAIL (필수 패턴)
  E-3  이벤트 멱등성 — consumer 쪽에 processed_events 테이블/엔티티 + eventId 기반 dedup
        grep -rnE '(processed_events|ProcessedEvent|@IdempotentConsumer)' modules/<mod>/src/main/java/*/infrastructure/
        → 매치가 0이면 FAIL (필수 패턴)
  E-4  Dead-letter 테이블 — 재시도 한도 초과 시 dead_letter_events 로 이동 (DB 테이블 + 이동 로직)
        grep -rniE '(dead_letter|DeadLetterEvent|dlq)' modules/ scripts/ db/ 2>/dev/null
        → 매치가 0이면 FAIL (필수 패턴)
  E-5  이벤트 카탈로그 — 도메인 이벤트 명세 (publisher/consumer/payload 계약) 존재
        find docs/events -name '*.md' 2>/dev/null | head -1
        → 결과 없으면 FAIL (필수 패턴)
  E-6  Cross-BC 는 이벤트로만 — M-1/M-4 재사용

[I — 인프라 / 단일-인스턴스 / 비-AWS 준수]
  I-1  AWS SDK import 전면 금지 — 전체 트리에서 AWS SDK 사용 불가
        grep -rnE 'import\s+(software\.amazon\.awssdk|com\.amazonaws)' .
        → 매치되면 FAIL
  I-2  docker-compose 유효성 — docker-compose.yml 존재 + `docker compose config -q` 통과
        test -f docker-compose.yml && docker compose config -q > /dev/null 2>&1
        → 실패 시 FAIL (FULL_RULES 에 등록)
  I-3  Dockerfile 멀티스테이지 — builder + runtime 2 stage 이상
        awk '/^FROM /{c++} END{exit !(c>=2)}' Dockerfile
        → 개수 < 2 이면 FAIL
  I-4  README 실행 가이드 — README 에 'docker compose up' 또는 'docker-compose up' 명시
        grep -iE 'docker(-|\s)compose\s+up' README.md
        → 매치가 0이면 FAIL

판정 방향 요약:
  - "존재해야 함" (매치 0이면 FAIL): D-3, M-3, M-5, E-2, E-3, E-4, E-5, I-4
  - "없어야 함"   (매치 있으면 FAIL): D-1, D-2, D-4, M-1, M-2, M-4, E-1, I-1
  - FULL_RULES 전용 (빌드·docker config·IT): I-2, I-3 + `./gradlew clean build` + 통합 테스트

[Phase 3 — 구현 (자동 수정 루프, 2h 40min)]
CLAUDE.md 의 제약을 전부 지키면서 [문제 요구사항] 을 구현하라.
Write/Edit 마다 PostToolUse hook 이 check.sh 를 자동 실행한다.
FAIL 출력을 보면 그 즉시 원인을 분석하고 코드를 수정해서 PASS 가 뜰 때까지 스스로 반복하라.

중간 체크포인트 (60분, 120분 경과 시 각 1회):
  1. `.claude/scripts/check.sh --full` 실행
  2. "진행률 X% / 남은 작업 Y개 / 남은 시간 Z분" 을 한 줄로 보고
  3. 남은 시간 < 필요 시간이면 [스코프 결정] 의 포기 후보 중 N개를 공식 드롭하고
     CLAUDE.md 에 드롭 사유 기록

에스컬레이션 규칙:
  - 동일 FAIL 3회 연속: 접근 방식을 바꿔라 (같은 수정을 반복하지 말 것)
  - 동일 FAIL 5회 연속: 해당 규칙을 임시 skip 리스트로 이동 + CLAUDE.md "보류 항목" 에
    기록 + 우선순위 낮은 태스크로 이동 (Phase 4 에서 재시도)

[Phase 4 — 최종 검증 (30분)]
1. `.claude/scripts/check.sh --full` 을 실행해 FAST + FULL 규칙 + 빌드 + 테스트가 모두 PASS 인지 확인
   (실패 항목은 고치고 재실행)
2. `docker compose up -d --build` → curl 로 health endpoint 200 확인 → `docker compose down`
3. `/simplify` 1회 (단, 4h 제약상 대규모 리팩터는 지양 — 중복·과설계 제거만)
4. `/security-review` 1회 (SQL injection / 인증 우회 / 비밀 노출 / 이벤트 payload 민감정보)
5. README 의 "빠른 시작" 섹션을 실제로 따라 실행해 문서 오류 수정
6. Phase 3 에서 발생한 "보류 항목" 이 남아 있으면 재시도해서 해소하거나, 해소 불가 시
   CLAUDE.md 최종 보고 섹션에 명시적으로 기록

전부 PASS 되면 정확히 다음 한 문장으로 보고하고 종료하라:

모든 구현 및 검증이 완료되었습니다

========================================
[문제 요구사항]
<여기에 문제 전문을 붙여넣으세요>
```

> **작동 원리**: Phase 1 은 *제약의 문법화*, Phase 2 는 *제약의 실행 가능화*, Phase 3 은 `settings.json` hook 이 매 편집마다 강제 피드백을 주는 구간, Phase 4 는 실제 기동·정적 리뷰까지 포함한 전체 검증. 프롬프트가 Claude 에게 "사용자 확인 금지 + 체크포인트 2회" 를 명시하므로 루프가 멈추거나 드리프트하지 않는다.

---

## 40분 / AWS 버전과의 차이 요약

| 항목 | 40분 (HARNESS_GUIDE.md) | 4시간 (이 가이드) |
|---|---|---|
| 시간 | 8+32 = 40min | 20+10+160+30 = 220min (+ 20min 버퍼) |
| 아키텍처 | MSA/EDA/DDD on AWS | 멀티 모듈 + 로컬 EDA + 단일 인스턴스 |
| 브로커 | SQS FIFO, Kafka, @FeignClient | PG outbox + @Scheduled poller + ApplicationEventPublisher |
| DLQ | SQS dead-letter 토픽 | `dead_letter_events` DB 테이블 |
| 동시성 | RedissonClient / `@DistributedLock` 필수 | `@Version` / `FOR UPDATE` / PG advisory lock 중 택 1 |
| D-5 ACL | 필수 | 외부 API 호출 없어 제외 |
| 체크포인트 | 없음 | 60분 / 120분 2회 (드리프트 방지) |
| Phase 4 | check.sh + simplify + security-review | + 실제 `docker compose up` + README walk |
| I 블록 | 없음 | 신규 4개 (AWS 금지 / docker config / Dockerfile 멀티스테이지 / README) |

## 드롭·치환한 원본 규칙과 사유

- **M-5 (RedissonClient 분산 락)** → **치환 (M-5)**: 단일 인스턴스라 분산 락은 불필요. 다만 금전/재고 동시성은 여전히 중요하므로 **DB 레벨 락 (`@Version` / `FOR UPDATE` / advisory lock) 명시 사용** 을 새 필수 패턴으로 요구.
- **E-2 (SQS publish Relay)** → **치환**: 동일 구조이되 publisher 가 `ApplicationEventPublisher` 또는 로컬 핸들러. Relay 는 `@Scheduled` poller 로 동일하게 분리.
- **E-4 (SQS/Kafka DLQ 토픽)** → **치환**: 같은 목적(실패 격리) 을 PG `dead_letter_events` 테이블로 달성.
- **D-5 (ACL — 외부 API DTO 의 domain 침투 금지)** → **드롭**: 외부 API 호출 없음이 과제 전제이므로 도메인 오염 경로가 원천 차단. CLAUDE.md "제외된 규칙" 에 명시.

## 이 가이드의 설계 원칙

1. **grep 으로 판정 불가한 원칙은 hook 에 넣지 않는다** — "Tell-Don't-Ask", "최종 일관성", "BC 경계 정합성" 같은 질적 판단은 [설계 가이드라인] 서술로만. hook 에 넣으면 false-positive/false-negative 가 루프를 오염시킨다.
2. **드롭보다 치환을 우선한다** — 원본 규칙이 의도한 *불변식* 을 같은 엄격도로 로컬 대체물에 요구한다 (SQS DLQ → DB DLQ, 분산 락 → DB 락).
3. **체크포인트는 시간이 긴 경우의 드리프트 방지 장치** — 60분마다 진행률·남은 시간을 자기 확인하고, 필요하면 [스코프 결정] 에서 공식 드롭. "적당히 계속" 이 아니라 "명시적 스코프 조정".
4. **Phase 4 는 정적 검사로 끝내지 않는다** — 실제 `docker compose up` + README walk 까지 포함해야 채점자 환경 재현성이 보장된다.
