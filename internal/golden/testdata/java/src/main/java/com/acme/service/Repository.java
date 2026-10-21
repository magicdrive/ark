package com.acme.service;

import com.acme.model.Entity;

public interface Repository<T extends Entity> {
    void save(T item);

    T find(long id);
}
