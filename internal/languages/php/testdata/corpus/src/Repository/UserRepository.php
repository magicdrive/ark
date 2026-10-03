<?php
namespace App\Repository;

use App\Domain\User;

interface UserRepository
{
    public function find(int $id): ?User;
}
