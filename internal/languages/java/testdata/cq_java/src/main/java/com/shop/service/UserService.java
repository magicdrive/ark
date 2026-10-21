package com.shop.service;

import static com.shop.util.Texts.normalize;

import com.shop.model.Audited;
import com.shop.model.Money;
import com.shop.model.Named;
import com.shop.model.Order;
import com.shop.model.Status;
import com.shop.model.User;
import com.shop.repo.UserRepository;
import com.shop.shared.Config;
import java.util.function.Function;

public class UserService {
    private final UserRepository repo;

    public UserService(UserRepository repo) {
        this.repo = repo;
    }

    @Audited("create")
    public User create(String name) {
        User u = new User(normalize(name));
        repo.save(u);
        audit(u);
        return u;
    }

    void audit(User u) {
        u.touch();
    }

    void audit(Order o) {
    }

    String describe(Named n) {
        return n.label();
    }

    String nameOf(Named n) {
        return n.name();
    }

    User lookup(long id) {
        return repo.find(id);
    }

    User lookupByName(String name) {
        return repo.find(name);
    }

    void untyped() {
        var r = locate();
        r.save(null);
    }

    UserRepository locate() {
        return repo;
    }

    Money total(Money a, Money b) {
        return a.plus(b);
    }

    boolean active() {
        return Status.ACTIVE.allows();
    }

    Function<String, User> factory() {
        return User::of;
    }

    String setting() {
        return Config.get();
    }

    class Cache {
        User get(long id) {
            return lookup(id);
        }
    }
}
