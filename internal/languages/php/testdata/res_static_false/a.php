<?php
class User {
    public function other() {}
}
class SuperUser {
    public static function create() {}
}
function f() {
    User::create();
}
