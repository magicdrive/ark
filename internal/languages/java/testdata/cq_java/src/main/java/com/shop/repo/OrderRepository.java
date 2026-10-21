package com.shop.repo;

import com.shop.model.Order;

public class OrderRepository {
    public void save(Order order) {
        order.touch();
    }

    public Order find(long id) {
        return null;
    }
}
