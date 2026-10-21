package com.shop.gateway;

import org.vendor.User;

public class Gateway {
    public User fetch() {
        User u = new User();
        u.touch();
        return u;
    }
}
