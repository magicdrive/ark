package com.acme.service;

import com.acme.model.User;

class UserServiceTest {
    void registers() {
        UserService service = new UserService(null);
        User u = service.register("ann");
        u.label();
    }
}
