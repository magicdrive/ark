<?php
interface UserRepository {
    public function find(int $id): User;
}
class DbUserRepository implements UserRepository {
    public function find(int $id): User {}
}
