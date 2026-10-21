package com.shop.model;

public interface Named {
    String name();

    default String label() {
        return "[" + name() + "]";
    }
}
