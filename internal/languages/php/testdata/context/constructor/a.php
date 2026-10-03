<?php
class UserRepository {}
class Logger {}
class UserService {
    public function __construct(
        private UserRepository $repository,
        private Logger $logger,
    ) {}
}
