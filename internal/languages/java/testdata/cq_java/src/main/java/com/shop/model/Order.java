package com.shop.model;

public class Order extends Entity {
    public String name() {
        return "order-" + id;
    }
}
