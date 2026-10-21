package com.acme.model;

public enum Role {
    ADMIN("a"),
    MEMBER("m") {
        @Override
        boolean canWrite() {
            return false;
        }
    };

    private final String code;

    Role(String code) {
        this.code = code;
    }

    boolean canWrite() {
        return true;
    }

    public String code() {
        return code;
    }
}
