<?php
class User {}
class UserFactory {
    public static function create(): User {}
}
class Service {
    public function run(): User {
        return UserFactory::create();
    }
}
