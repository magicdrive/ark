package com.shop.model;

public abstract class Entity {
    protected long id;

    public void touch() {
        id++;
    }
}
