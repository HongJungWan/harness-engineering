Feature: 주문 취소 및 잔고 복원
  As a 가상자산 거래소 사용자
  I want to 주문을 취소하면 잠긴 잔고가 복원된다

  Background:
    Given 시스템이 초기화되어 있다
    And user 4 의 KRW 잔고가 10000000 이다

  Scenario: 주문 취소 시 잔고 원자적 복원
    Given user 4 이 BTC/KRW BUY LIMIT 주문을 넣는다 price 95000000 qty 0.1
    And 주문이 ACCEPTED 상태로 생성된다
    When 마지막 주문을 취소한다
    Then 마지막 주문의 상태가 CANCELLED 이다
    And user 4 의 KRW available 잔고가 10000000 이다
    And user 4 의 KRW locked 잔고가 0 이다
    And OrderCancelled outbox 이벤트가 존재한다
    And BalanceRestored outbox 이벤트가 존재한다
