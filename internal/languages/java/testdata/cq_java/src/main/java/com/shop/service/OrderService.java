package com.shop.service;

import com.shop.model.Order;
import com.shop.repo.OrderRepository;

public class OrderService {
    private final OrderRepository repo = new OrderRepository();

    public Order create(String name) {
        Order o = new Order();
        repo.save(o);
        audit(o);
        return o;
    }

    void audit(Order o) {
        o.touch();
    }
}
