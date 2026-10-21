package com.acme.model;

/** The base of every stored object. */
public abstract class Entity implements Named {
    protected long id;

    public long id() {
        return id;
    }

    public void touch() {
        id++;
    }

    public static class Key {
        public String text() {
            return "";
        }
    }
}
