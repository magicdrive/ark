package com.example;

public class App {
    String artifactId() { return "javaapp"; }
    String project() { return artifactId(); }
    void dependency() { new Core().core(); }
}
