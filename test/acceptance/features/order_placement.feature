Feature: 주문 생성 Happy Path
  As a 가상자산 거래소 사용자
  I want to BUY 주문을 넣으면 잔고가 차감되고 이벤트가 발행된다

  Background:
    Given 시스템이 초기화되어 있다
    And user 1 의 KRW 잔고가 10000000 이다

  Scenario: 잔고 충분한 BUY LIMIT 주문 성공
    When user 1 이 BTC/KRW BUY LIMIT 주문을 넣는다 price 95000000 qty 0.1
    Then 주문이 ACCEPTED 상태로 생성된다
    And user 1 의 KRW available 잔고가 500000 이다
    And user 1 의 KRW locked 잔고가 9500000 이다
    And OrderPlaced outbox 이벤트가 존재한다
    And BalanceDeducted outbox 이벤트가 존재한다
