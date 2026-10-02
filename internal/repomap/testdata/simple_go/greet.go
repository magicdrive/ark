package main

import "fmt"

// Greet prints a greeting.
func greet() {
	fmt.Println("hello")
}

// GreetUser greets a named user.
func greetUser(name string) {
	fmt.Printf("hello, %s\n", name)
}
