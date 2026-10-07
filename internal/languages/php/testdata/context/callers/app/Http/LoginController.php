<?php

namespace App\Http;

use App\Services\LoginScreenPolicy;

// Laravel 6 style untyped property injected by the constructor and never
// written elsewhere: the constructor parameter type proves the receiver, so the
// call resolves exactly (private property, no trait).
class LoginController
{
    private $policy;

    public function __construct(LoginScreenPolicy $policy)
    {
        $this->policy = $policy;
    }

    public function showLoginForm()
    {
        return $this->policy->showsSsoButton();
    }
}
