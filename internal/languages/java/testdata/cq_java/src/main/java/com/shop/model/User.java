package com.shop.model;

public class User extends Entity implements Named {
    private final String name;
    private Status status = Status.ACTIVE;

    public User(String name) {
        this.name = name;
    }

    public User() {
        this("anonymous");
    }

    public static User of(String name) {
        return new User(name);
    }

    @Override
    public String name() {
        return name;
    }

    public void block() {
        status = Status.BLOCKED;
    }
}
