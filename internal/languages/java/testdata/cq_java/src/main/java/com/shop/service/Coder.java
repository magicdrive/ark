package com.shop.service;

import com.shop.util.Codes;

class Coder {
    String ofLong(long id) {
        return Codes.code(id);
    }

    String ofInt(int count) {
        return Codes.code(count);
    }

    String ofFlag(boolean flag) {
        return Codes.code(flag);
    }
}
