package com.acme.service;

import static com.acme.service.Strings.upper;

import com.acme.model.*;
import java.util.function.Function;

public class UserService {
    private final Repository<User> users;

    public UserService(Repository<User> users) {
        this.users = users;
    }

    public User register(String name) {
        User u = new User(upper(name));
        users.save(u);
        u.grant(Role.ADMIN);
        u.grant("MEMBER");
        log(1);
        log("registered");
        return u;
    }

    void log(int level) {}

    void log(String message) {}

    public <E extends Entity> void touchAll(E entity) {
        entity.touch();
    }

    Function<String, User> factory() {
        return User::new;
    }

    Runnable later(User u) {
        return new Runnable() {
            @Override
            public void run() {
                u.touch();
                log(2);
            }
        };
    }

    Point corner() {
        class Grid {
            Point at(int x) {
                return Point.origin().shift(x);
            }
        }
        return new Grid().at(3);
    }
}
