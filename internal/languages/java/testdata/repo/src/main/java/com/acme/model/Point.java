package com.acme.model;

public record Point(int x, int y) {
    public static Point origin() {
        return new Point(0, 0);
    }

    public Point shift(int dx) {
        return new Point(x + dx, y);
    }
}
