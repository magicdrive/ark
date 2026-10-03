<?php
namespace App\Service;

use App\Domain\User;
use App\Repository\UserRepository;

class UserService
{
    public function __construct(
        private readonly UserRepository $repository,
    ) {}

    public function find(int $id): ?User
    {
        return $this->repository->find($id);
    }
}
