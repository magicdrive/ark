package com.shop.util;

public final class Codes {
    private Codes() {}

    public static String code(long value) {
        return "L" + value;
    }

    public static String code(int value) {
        return "I" + value;
    }

    public static String code(boolean value) {
        return value ? "T" : "F";
    }
}
