package com.shop.util;

public final class Texts {
    private Texts() {}

    public static String normalize(String s) {
        return normalize(s, true);
    }

    public static String normalize(String s, boolean lower) {
        String t = s.trim();
        return lower ? t.toLowerCase() : t;
    }
}
