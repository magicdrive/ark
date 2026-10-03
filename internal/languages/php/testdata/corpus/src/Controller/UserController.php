<?php
namespace App\Controller;

use App\Service\UserService;

final class UserController
{
    public function __construct(private UserService $service) {}

    public function show(int $id): void
    {
        $this->service->find($id);
    }
}
