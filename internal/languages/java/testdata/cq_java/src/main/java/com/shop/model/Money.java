package com.shop.model;

public record Money(long cents, String currency) {
    public Money {
        if (cents < 0) {
            throw new IllegalArgumentException("negative");
        }
    }

    public Money plus(Money other) {
        return new Money(cents + other.cents, currency);
    }
}
