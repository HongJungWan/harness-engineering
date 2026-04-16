# 하네스 엔지니어링 가이드

> **40분 AI 코딩테스트**에서 하네스 엔지니어링을 적용하기 위한 실전 가이드.
> 사전에 3개 템플릿을 준비하고, 테스트 시작과 동시에 **8분 안에 자동 검증 루프를 세팅**한 뒤, 나머지 32분을 루프 안에서 코딩한다.

---

## 자율 실행 프롬프트

Claude Code에 **아래 프롬프트 하나만** 붙여넣는다. `<여기에 문제 전문을 붙여넣으세요>` 부분만 실제 문제로 교체하면 Claude 가 Phase 1~4 를 한 턴 안에 자율적으로 실행한다 (각 Phase 사이 사용자 확인 없음).

```
너는 40분 제한 AI 코딩 테스트를 수행하는 시니어 엔지니어다.
아래 [문제 요구사항] 을 읽고 다음 Phase 1~4 를 중단 없이 자율 실행하라.
각 Phase 사이에 사용자 확인을 받지 말고, Phase 4 완료 보고까지 한 턴 안에 끝내라.
사용자에게 질문하지 말고 합리적 가정으로 진행하라.

시작 시 TaskCreate 로 Phase 1~4 를 todo 로 만들고, 각 단계 진입/완료마다 상태를 전환하라.
Phase 1~2 는 plan mode 로 설계를 확정한 뒤 ExitPlanMode 로 승인받고,
Phase 3~4 는 auto mode 로 실행하라 — 8분 세팅 오류를 조기 발견하여 30분대 재작업을 방지한다.

[Phase 1 — 제약 정의]
ultrathink 로 문제를 먼저 분석하고, 아래 18개 D/M/E 규칙 중 실제 적용할 것만 선별하라.
제외 판단의 근거(문제 맥락과 맞지 않는 이유)는 CLAUDE.md "제외된 규칙" 섹션에 한 줄씩 기록한다.
프로젝트 루트의 CLAUDE.md 에서 [문제 개요], [디렉토리 구조], [금지 패턴],
[필수 패턴], [수정 레시피] placeholder 를 모두 채워라.
- 금지 패턴 3~5개 / 필수 패턴 2~3개, 모두 grep 으로 검증 가능해야 한다.
- 추상적 규칙("좋은 설계", "clean code") 금지. 구체적 경로·심볼·import 문자열 기반.
- 각 규칙에는 ID (F-1, F-2, ..., R-1, R-2, ...) 와 적용 범위(디렉토리/파일 글롭) 를 명시한다.
- 문제가 MSA / EDA / DDD / Bounded Context / Outbox / Aggregate 중 하나라도 언급하면
  Phase 2 하단의 [D/M/E 프로파일] 규칙을 CLAUDE.md [금지/필수 패턴] 표에 전부 복사하라.
  문제 맥락에 해당 없는 규칙만 제외하되, 제외 사유를 CLAUDE.md 하단 "제외된 규칙" 섹션에
  한 줄씩 기록한다 (나중 검토용).
- 검증 불가능한 원칙(최종 일관성, MSA 전환 로드맵, Tell-Don't-Ask) 은 [금지/필수 패턴] 에
  넣지 말고 CLAUDE.md 의 "설계 가이드라인" 섹션에 서술형으로만 기술하라 — hook 은 다루지 않는다.

[Phase 2 — 검증 훅 작성]
.claude/scripts/check.sh 의 예시 함수(check_F1 / check_F2 / check_R1 등) 를 모두 삭제하고,
Phase 1 에서 정의한 ID 와 1:1 매칭되는 bash 함수를 새로 작성하라.
- 위반 시: "FAIL [ID]: <설명>" 출력 + FAIL=1 세팅.
- 통과 시: "PASS [ID]" 출력.
- FAST_RULES (grep, 매 편집) / FULL_RULES (빌드·테스트, --full 일 때만) 배열에 함수명을 등록.
- 작성 후 반드시 `bash -n .claude/scripts/check.sh` 로 문법을 검증하고, 오류가 있으면 고쳐라.

아키텍처 프로파일 규칙 (문제가 MSA/EDA/DDD 기반일 때 아래를 전부 포함하라.
스택이 다르면 <mod>/domain/ 같은 경로만 실제 구조에 맞게 치환하고, grep 의 import
문자열은 언어/프레임워크에 맞게 바꿔라. Java/Kotlin Spring 기준 템플릿):

[D — DDD 준수]
  D-1  domain 순수성 — <mod>/domain/ 에서 infrastructure·presentation·외부 프레임워크 import 금지
        grep -rnE 'import .*(infrastructure|presentation|\.api\.dto|org\.springframework|javax\.persistence|jakarta\.persistence)' <mod>/domain/
        → 매치되면 FAIL
  D-2  Value Object 강제 — 금융·시간·식별자 필드에 원시 BigDecimal/double/float 직접 사용 금지 (Money·Amount·Period VO 필수)
        grep -rnE '\b(BigDecimal|double|float)\s+(amount|price|fee|balance|total|limit)\b' <mod>/domain/
        → 매치되면 FAIL
  D-3  Aggregate Root 명시 — 각 BC 에 최소 1개의 AggregateRoot 존재 (상태 변경 진입점)
        grep -rnE '(@AggregateRoot|class\s+\w+AggregateRoot|extends\s+AggregateRoot)' <mod>/domain/
        → 매치가 0이면 FAIL (필수 패턴)
  D-4  상태 전이 캡슐화 — 엔티티에 public setter 금지 (상태 변경은 명명된 도메인 메서드로만)
        grep -rnE '^\s*public\s+void\s+set[A-Z]\w*\s*\(' <mod>/domain/
        → 매치되면 FAIL
  D-5  Anti-Corruption Layer — 외부 API DTO 가 domain/ 에 침투 금지 (acl/ 또는 adapter/ 에서 도메인 모델로 변환)
        grep -rnE 'import .*(partner|external|ninehire|\.api\.client|\.api\.dto)' <mod>/domain/
        → 매치되면 FAIL

[M — MSA 준수]
  M-1  모듈 간 직접 import 금지 — 다른 BC 의 패키지 import 불가 (호출은 이벤트 또는 로컬 read model 로만)
        grep -rnE 'import .*\.(payment|remittance|partner|balance|order)\b' <mod>/   # 자기 BC 제외하고
        → 매치되면 FAIL (BC 쌍별로 1개 함수씩 생성 권장)
  M-2  Schema-per-Module — 각 모듈 엔티티의 @Table(schema=...) 이 모듈명과 일치
        grep -rnE '@Table\s*\([^)]*schema\s*=\s*"(?!<mod>)' <mod>/infrastructure/
        → 타모듈 스키마 참조 시 FAIL
  M-3  모듈별 독립 Outbox — 각 모듈이 자기 OutboxEntity/테이블 보유 (서비스 분리 시 그대로 가져감)
        ls <mod>/infrastructure/outbox/*.{java,kt} 2>/dev/null
        → 파일 없으면 FAIL (필수 패턴)
  M-4  CQRS Read Model — application 계층에서 타 모듈 feign/rest 직접 호출 금지 (로컬 read model 조회로 대체)
        grep -rnE '@FeignClient|RestTemplate|WebClient\.create' <mod>/application/
        → 매치되면 FAIL
  M-5  분산 락 — JVM synchronized 블록 금지 (멀티 인스턴스 환경 호환), RedissonClient 또는 @DistributedLock 사용
        grep -rnE 'synchronized\s*\(' <mod>/application/ <mod>/domain/
        → 매치되면 FAIL

[E — EDA 준수]
  E-1  Transactional Outbox — usecase 가 eventProducer 를 직접 호출하면 안됨 (outbox.save() 만 허용; 릴레이가 publish)
        grep -rnE '(eventProducer|sqsClient|kafkaTemplate)\.(send|publish)' <mod>/application/
        → 매치되면 FAIL
  E-2  Outbox Relay 분리 — outbox/relay/ 디렉토리에 별도 poller + SQS publish 로직 존재
        find . -path '*/outbox/relay/*' -name '*.{java,kt,go}' 2>/dev/null
        → 결과 없으면 FAIL (필수 패턴)
  E-3  이벤트 멱등성 — consumer 쪽에 processed_events 테이블/엔티티 + eventId 기반 dedup
        grep -rnE 'processed_events|ProcessedEvent|@IdempotentConsumer' <mod>/infrastructure/
        → 매치가 0이면 FAIL (필수 패턴)
  E-4  DLQ 설정 — SQS/Kafka 설정에 dead-letter 토픽 존재
        grep -rniE '(dlq|dead[-_]?letter)' config/ <mod>/infrastructure/
        → 매치가 0이면 FAIL (필수 패턴)
  E-5  이벤트 카탈로그 — 도메인 이벤트 명세 문서 (publisher/consumer/payload 계약) 존재
        ls docs/events/*.md 2>/dev/null || ls docs/*events*.md 2>/dev/null
        → 파일 없으면 FAIL (필수 패턴)
  E-6  Cross-BC 호출은 이벤트로만 — application 이 타 BC 의 service/facade import 시 FAIL (M-1 재사용 가능)

각 규칙은 독립 bash 함수로 작성하고 FAST_RULES 배열에 등록하라. D-3·E-2·M-3·E-3·E-4·E-5
같은 "존재해야 함" 규칙은 매치 0 이면 FAIL, 나머지는 매치가 있으면 FAIL 이다.

[Phase 3 — 구현 (자동 수정 루프)]
CLAUDE.md 의 제약을 전부 지키면서 [문제 요구사항] 을 구현하라.
Write/Edit 마다 PostToolUse hook 이 check.sh 를 자동 실행한다.
FAIL 출력을 보면 그 즉시 원인을 분석하고 코드를 수정해서 PASS 가 뜰 때까지 스스로 반복하라.
(동일 FAIL 이 3회 연속이면 접근 방식을 바꿔라 — 같은 수정을 반복하지 말 것.)

[Phase 4 — 최종 검증]
`.claude/scripts/check.sh --full` 을 실행해 전체 규칙 + 빌드 + 테스트가 모두 PASS 인지 확인하라.
실패 항목이 있으면 고치고 재실행하라.
`--full` PASS 후에는 `/simplify` 와 `/security-review` 를 각 1회 돌려 최종 품질 패스를 수행하라.
전부 PASS 되면 정확히 다음 한 문장으로 보고하고 종료하라:

모든 구현 및 검증이 완료되었습니다

========================================
[문제 요구사항]
<여기에 문제 전문을 붙여넣으세요>
```

> **작동 원리**: Phase 1 은 *제약의 문법화*, Phase 2 는 *제약의 실행 가능화*, Phase 3 은 `settings.json` 의 hook 이 활성화되어 매 편집마다 강제 피드백이 들어오는 구간, Phase 4 는 빌드·테스트까지 포함한 전체 검증. 프롬프트가 Claude 에게 "사용자 확인 금지" 를 명시하므로 중간에 멈추지 않고 루프가 돈다.
>
> **아키텍처 프로파일 (D/M/E) 설계 의도**: 나열된 18개 규칙은 모두 `grep` 만으로 판정 가능한 정적 불변식(invariant)으로만 구성되어 있다. "최종 일관성", "MSA 전환 로드맵", "Tell-Don't-Ask" 같이 텍스트 파싱으로 검증 불가한 원칙은 의도적으로 제외하고 CLAUDE.md 의 서술 섹션으로 분리했다 — hook 이 판단할 수 없는 것을 hook 에 넣으면 false-positive/false-negative 가 루프를 오염시킨다. 그리고 규칙을 "존재해야 함(D-3, E-2, M-3, E-3~5)" 과 "없어야 함(D-1·2·4·5, M-1·2·4·5, E-1)" 으로 이원화한 이유는 전자는 구조적 완결성(필수 패턴), 후자는 경계 위반(금지 패턴) 을 각각 검증하기 때문이다 — Phase 1 의 [금지/필수 패턴] 표 분할과 1:1 대응된다.

---
