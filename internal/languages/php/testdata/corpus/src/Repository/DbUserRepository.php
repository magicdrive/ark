<?php
namespace App\Repository;

use App\Domain\User;
use App\Support\LogsActivity;

final class DbUserRepository implements UserRepository
{
    use LogsActivity;

    public function find(int $id): ?User
    {
        $this->record("find");
        return new User($id, "n");
    }
}
