<?php

namespace App\Services;

class LoginScreenPolicy
{
    public function showsSsoButton(): bool
    {
        return true;
    }
}
