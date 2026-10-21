package com.acme.model;

import java.util.ArrayList;
import java.util.List;

public class User extends Entity {
    private final String name;
    private final List<User> friends = new ArrayList<>();
    private Role role = Role.MEMBER;

    public User(String name) {
        this.name = name;
    }

    public User() {
        this("anonymous");
    }

    @Override
    public String name() {
        return name;
    }

    public void befriend(User other) {
        friends.add(other);
        other.touch();
        touch();
    }

    public void grant(Role r) {
        role = r;
    }

    public void grant(String roleName) {
        grant(Role.valueOf(roleName));
    }

    class Badge extends Key {
        String show() {
            return text() + name();
        }
    }
}
