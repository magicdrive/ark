package com.shop.repo;

import com.shop.model.User;

public class UserRepository {
    public void save(User user) {
        user.touch();
    }

    public User find(long id) {
        return null;
    }

    public User find(String name) {
        return User.of(name);
    }
}
