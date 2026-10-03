<?php

namespace App\Service;

class UserService
{
    private Repository $repository;

    public const DEFAULT_LIMIT = 100;

    public function __construct(
        private Logger $logger,
    ) {}

    public function find(int $id): User {}
}
