package com.shop.model;

public enum Status {
    ACTIVE,
    BLOCKED {
        @Override
        public boolean allows() {
            return false;
        }
    };

    public boolean allows() {
        return true;
    }
}
