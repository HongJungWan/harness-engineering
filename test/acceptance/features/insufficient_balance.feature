Feature: 잔고 부족 주문 거부
  As a 거래소 플랫폼
  I need 잔고 부족 시 주문을 거부하고 잔고를 변동시키지 않는다

  Background:
    Given 시스템이 초기화되어 있다
    And user 2 의 KRW 잔고가 100000 이다

  Scenario: 잔고 초과 주문 거부
    When user 2 이 BTC/KRW BUY LIMIT 주문을 넣는다 price 95000000 qty 1.0
    Then insufficient balance 에러가 반환된다
    And user 2 의 KRW available 잔고가 100000 이다
    And outbox 이벤트가 없다
