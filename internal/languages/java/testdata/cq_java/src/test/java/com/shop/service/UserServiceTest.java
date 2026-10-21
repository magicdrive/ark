package com.shop.service;

import com.shop.repo.UserRepository;

class UserServiceTest {
    void creates() {
        new UserService(new UserRepository()).create("Ann");
    }
}
