package com.acme.model;

public interface Named {
    String name();

    default String label() {
        return "[" + name() + "]";
    }
}
