<?php
class User {
    public static function create(): User {}
}
function f() {
    User::create();
}
