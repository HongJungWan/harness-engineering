Feature: 동시성 비관적 락 검증
  As a 거래소 플랫폼
  I need 동일 잔고에 대한 동시 주문이 정확하게 직렬화된다

  Background:
    Given 시스템이 초기화되어 있다
    And user 3 의 KRW 잔고가 10000000 이다

  Scenario: 동일 잔고에 10개 동시 주문 직렬화
    When 10 개의 동시 BUY 주문이 user 3 에게 들어온다 price 95000000 qty 0.01
    Then 모든 주문이 에러 없이 완료된다
    And user 3 의 잔고 보존 법칙이 성립한다 total 10000000
    And 음수 잔고가 존재하지 않는다 user 3
